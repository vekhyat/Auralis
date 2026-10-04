export type DownloadExecutionKind = "direct" | "queue";

export interface DownloadExecutionLease {
    readonly id: number;
    readonly kind: DownloadExecutionKind;
}

export interface DownloadExecutionNotice {
    releasedKind: DownloadExecutionKind;
    autoStart: boolean;
}

interface ActiveExecution {
    id: number;
    kind: DownloadExecutionKind;
    pause: boolean;
    stop: boolean;
}

interface ExecutionWaiter {
    kind: DownloadExecutionKind;
    resolve: (lease: DownloadExecutionLease) => void;
}

/**
 * One in-flight transfer at a time. Direct downloads and the queue runner
 * take turns on the same lease, and pause/stop apply only to the holder.
 */
export class DownloadExecutionCoordinator {
    private nextId = 1;
    private active: ActiveExecution | null = null;
    private waiters: ExecutionWaiter[] = [];
    private autoStartArmed = false;
    private followUp = false;
    private blocks = 0;
    private unblock: Promise<void> = Promise.resolve();
    private listeners = new Set<(event: DownloadExecutionNotice) => void>();

    activeKind(): DownloadExecutionKind | null {
        return this.active?.kind ?? null;
    }

    isStopRequested(): boolean {
        return this.active?.stop === true;
    }

    isPauseRequested(): boolean {
        return this.active?.pause === true;
    }

    tryAcquire(kind: DownloadExecutionKind): DownloadExecutionLease | null {
        if (this.active) return null;
        return this.grant(kind);
    }

    acquire(kind: DownloadExecutionKind): Promise<DownloadExecutionLease> {
        if (!this.active) return Promise.resolve(this.grant(kind));
        return new Promise((resolve) => {
            this.waiters.push({ kind, resolve });
        });
    }

    isBlocked(): boolean {
        return this.blocks > 0;
    }

    whenUnblocked(): Promise<void> {
        return this.unblock;
    }

    blockFor(work: Promise<void>): void {
        this.blocks += 1;
        const done = work.then(() => undefined, () => undefined).then(() => {
            this.blocks -= 1;
            if (this.blocks === 0) this.unblock = Promise.resolve();
        });
        this.unblock = this.unblock.then(() => done);
    }

    async acquireWhenUnblocked(kind: DownloadExecutionKind): Promise<DownloadExecutionLease> {
        if (this.blocks > 0) await this.unblock;
        return this.acquire(kind);
    }

    release(lease: DownloadExecutionLease): void {
        if (!this.active || this.active.id !== lease.id) return;
        const releasedKind = this.active.kind;
        const next = this.waiters.shift();
        if (next) {
            const granted = this.grant(next.kind);
            next.resolve(granted);
            this.emit({ releasedKind, autoStart: false });
            return;
        }
        this.active = null;
        const autoStart = releasedKind === "direct" && this.autoStartArmed;
        if (autoStart) this.autoStartArmed = false;
        this.emit({ releasedKind, autoStart });
    }

    requestPause(kind: DownloadExecutionKind): boolean {
        if (!this.active || this.active.kind !== kind) return false;
        this.active.pause = true;
        return true;
    }

    requestStop(kind: DownloadExecutionKind): boolean {
        if (!this.active || this.active.kind !== kind) return false;
        this.active.stop = true;
        this.active.pause = false;
        return true;
    }

    clearPause(kind: DownloadExecutionKind): boolean {
        if (!this.active || this.active.kind !== kind) return false;
        this.active.pause = false;
        return true;
    }

    clearStop(kind: DownloadExecutionKind): boolean {
        if (!this.active || this.active.kind !== kind) return false;
        this.active.stop = false;
        return true;
    }

    clearSignals(kind: DownloadExecutionKind): boolean {
        if (!this.active || this.active.kind !== kind) return false;
        this.active.pause = false;
        this.active.stop = false;
        return true;
    }

    pauseActive(): boolean {
        if (!this.active) return false;
        this.active.pause = true;
        return true;
    }

    armAutoStart(): void {
        this.autoStartArmed = true;
    }

    armFollowUp(): void {
        this.followUp = true;
    }

    consumeFollowUp(paused: boolean, stopped: boolean): boolean {
        const follow = this.followUp && !paused && !stopped;
        this.followUp = false;
        return follow;
    }

    subscribe(listener: (event: DownloadExecutionNotice) => void): () => void {
        this.listeners.add(listener);
        return () => {
            this.listeners.delete(listener);
        };
    }

    private grant(kind: DownloadExecutionKind): DownloadExecutionLease {
        const active: ActiveExecution = { id: this.nextId++, kind, pause: false, stop: false };
        this.active = active;
        return { id: active.id, kind: active.kind };
    }

    private emit(event: DownloadExecutionNotice): void {
        for (const listener of this.listeners) listener(event);
    }
}

export const downloadExecution = new DownloadExecutionCoordinator();
