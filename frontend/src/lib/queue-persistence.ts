export const QUEUE_PERSISTENCE_MAX_ATTEMPTS = 4;
export const QUEUE_PERSISTENCE_BASE_DELAY_MS = 250;
export const QUEUE_PERSISTENCE_MAX_DELAY_MS = 4000;

export type QueuePersistencePhase = "idle" | "saving" | "unsaved";

export interface QueuePersistenceState {
    phase: QueuePersistencePhase;
    failedAttempts: number;
    retrying: boolean;
    sealed: boolean;
}

export type StoredQueueLoadPlan = "preserve-corrupt" | "migrate-legacy" | "read-database";

export function persistenceBackoffMs(failedAttempts: number, base = QUEUE_PERSISTENCE_BASE_DELAY_MS, max = QUEUE_PERSISTENCE_MAX_DELAY_MS): number {
    if (failedAttempts <= 0) return base;
    const shifted = base * 2 ** (failedAttempts - 1);
    return Math.min(max, shifted);
}

export function isStoredQueueItem(item: unknown): item is {
    id: string;
    tracks: unknown[];
} {
    if (!item || typeof item !== "object" || Array.isArray(item)) return false;
    const record = item as Record<string, unknown>;
    if (!["id", "key", "name", "artist", "info", "image"].every(key => typeof record[key] === "string")) return false;
    if (typeof record.id !== "string" || record.id.trim() === "") return false;
    if (typeof record.type !== "string" || !["track", "album", "playlist", "artist"].includes(record.type)) return false;
    if (typeof record.status !== "string" || !["pending", "running", "paused", "done", "partial", "skipped", "failed"].includes(record.status)) return false;
    if (typeof record.trackCount !== "number" || !Number.isSafeInteger(record.trackCount) || record.trackCount < 0) return false;
    if (typeof record.addedAt !== "number" || !Number.isFinite(record.addedAt)) return false;
    return Array.isArray(record.tracks) && record.tracks.every(track => {
        if (!track || typeof track !== "object" || Array.isArray(track)) return false;
        return "name" in track && typeof track.name === "string"
            && "artists" in track && typeof track.artists === "string"
            && (!("spotify_id" in track) || typeof track.spotify_id === "string");
    });
}

export function inspectStoredQueue(raw: string): "empty" | "corrupt" | "usable" {
    if (raw.trim() === "") return "empty";
    let parsed: unknown;
    try {
        parsed = JSON.parse(raw);
    }
    catch {
        return "corrupt";
    }
    if (!Array.isArray(parsed)) return "corrupt";
    for (const item of parsed) {
        if (!isStoredQueueItem(item)) return "corrupt";
    }
    return "usable";
}

export function storedQueueLoadPlan(raw: string): StoredQueueLoadPlan {
    const kind = inspectStoredQueue(raw);
    if (kind === "corrupt") return "preserve-corrupt";
    if (kind === "empty") return "migrate-legacy";
    return "read-database";
}

export type PersistentQueueLoadDecision = StoredQueueLoadPlan | "preserve-store";

export function decidePersistentQueueLoad(result: {
    ok: true;
    payload: string;
} | {
    ok: false;
}): PersistentQueueLoadDecision {
    if (!result.ok)
        return "preserve-store";
    return storedQueueLoadPlan(result.payload);
}

export function queueLoadWritesStore(decision: PersistentQueueLoadDecision): boolean {
    return decision === "migrate-legacy" || decision === "read-database";
}

export type QueuePersistenceNotice = "quiet" | "retrying" | "unsaved";

export function queuePersistenceNotice(state: QueuePersistenceState): QueuePersistenceNotice {
    if (state.phase === "unsaved") return "unsaved";
    if (state.retrying) return "retrying";
    return "quiet";
}

export function canRetryQueuePersistence(state: QueuePersistenceState): boolean {
    return state.phase === "unsaved" && !state.sealed;
}

export interface QueuePersistenceController {
    note(): void;
    retry(): void;
    failClosed(): void;
    getState(): QueuePersistenceState;
    subscribe(listener: (state: QueuePersistenceState) => void): () => void;
    whenSettled(): Promise<void>;
}

export function createQueuePersistenceController(options: {
    isDirty: () => boolean;
    persist: () => Promise<void>;
    onFailure?: (error: unknown) => void;
    maxAttempts?: number;
    backoffMs?: (failedAttempts: number) => number;
    sleep?: (ms: number) => Promise<void>;
}): QueuePersistenceController {
    const maxAttempts = options.maxAttempts ?? QUEUE_PERSISTENCE_MAX_ATTEMPTS;
    const backoffMs = options.backoffMs ?? persistenceBackoffMs;
    const sleep = options.sleep ?? ((ms: number) => new Promise<void>((resolve) => {
        setTimeout(resolve, ms);
    }));
    let phase: QueuePersistencePhase = "idle";
    let failedAttempts = 0;
    let retrying = false;
    let sealed = false;
    let running = false;
    let rerun = false;
    let worker: Promise<void> | null = null;
    const listeners = new Set<(state: QueuePersistenceState) => void>();

    function getState(): QueuePersistenceState {
        return { phase, failedAttempts, retrying, sealed };
    }

    function emit(): void {
        const snapshot = getState();
        for (const listener of listeners) listener(snapshot);
    }

    async function runBody(): Promise<void> {
        if (sealed) {
            phase = "unsaved";
            retrying = false;
            emit();
            return;
        }
        phase = "saving";
        emit();
        while (!sealed && options.isDirty()) {
            try {
                retrying = failedAttempts > 0;
                if (retrying) emit();
                await options.persist();
                failedAttempts = 0;
                retrying = false;
            }
            catch (error) {
                failedAttempts += 1;
                try {
                    options.onFailure?.(error);
                }
                catch (failureError) {
                    console.error("Download queue persistence failure handler failed:", failureError);
                }
                if (sealed || failedAttempts >= maxAttempts) {
                    phase = "unsaved";
                    retrying = false;
                    emit();
                    return;
                }
                retrying = true;
                phase = "saving";
                emit();
                await sleep(backoffMs(failedAttempts));
            }
        }
        if (sealed) {
            phase = "unsaved";
            retrying = false;
            emit();
            return;
        }
        phase = "idle";
        retrying = false;
        emit();
    }

    function kick(): void {
        if (running || sealed) return;
        running = true;
        const current: { task: Promise<void> | null } = { task: null };
        current.task = (async () => {
            try {
                await runBody();
            }
            finally {
                running = false;
                if (worker === current.task) worker = null;
            }
            if (sealed) return;
            if (rerun) {
                rerun = false;
                failedAttempts = 0;
                retrying = false;
                kick();
                return;
            }
            if (phase !== "unsaved" && options.isDirty()) kick();
        })();
        worker = current.task;
    }

    function whenSettled(): Promise<void> {
        if (!worker) return Promise.resolve();
        return worker.then(() => whenSettled());
    }

    return {
        note(): void {
            if (sealed) {
                phase = "unsaved";
                retrying = false;
                emit();
                return;
            }
            if (running) {
                rerun = true;
                return;
            }
            failedAttempts = 0;
            retrying = false;
            kick();
        },
        retry(): void {
            if (sealed) {
                phase = "unsaved";
                retrying = false;
                emit();
                return;
            }
            if (phase !== "unsaved") return;
            failedAttempts = 0;
            retrying = false;
            if (running) {
                rerun = true;
                return;
            }
            kick();
        },
        failClosed(): void {
            sealed = true;
            phase = "unsaved";
            retrying = false;
            emit();
        },
        getState,
        subscribe(listener: (state: QueuePersistenceState) => void): () => void {
            listeners.add(listener);
            return () => {
                listeners.delete(listener);
            };
        },
        whenSettled,
    };
}
