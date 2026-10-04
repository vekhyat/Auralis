import type { TrackMetadata } from "@/types/api";
import { ApplyPersistentDownloadQueueChanges, LoadPersistentDownloadQueue, ReplacePersistentDownloadQueue } from "../../wailsjs/go/main/App";
import { isProtectedQueueStatus, nextAutomaticQueueItem, nextManualQueueItem, queueItemStatusAfterAttempt, retainProtectedQueueItems } from "./queue-guards";
import { createQueuePersistenceController, decidePersistentQueueLoad, isStoredQueueItem, queueLoadWritesStore, type QueuePersistenceState } from "./queue-persistence";
export type QueueItemType = "track" | "album" | "playlist" | "artist";
export type QueueItemStatus = "pending" | "running" | "paused" | "done" | "partial" | "skipped" | "failed";
export type QueueTrackStatus = "done" | "failed" | "skipped";
export interface QueueItem {
    id: string;
    key: string;
    type: QueueItemType;
    name: string;
    artist: string;
    info: string;
    image: string;
    folderName?: string;
    isAlbum?: boolean;
    position?: number;
    durationMs?: number;
    trackCount: number;
    tracks: TrackMetadata[];
    status: QueueItemStatus;
    error?: string;
    trackResults?: Record<string, QueueTrackStatus>;
    trackFilePaths?: Record<string, string>;
    addedAt: number;
}
export interface AddCollectionInput {
    type: Exclude<QueueItemType, "track">;
    name: string;
    artist: string;
    info: string;
    image: string;
    folderName?: string;
    isAlbum?: boolean;
    tracks: TrackMetadata[];
}
export interface AddTracksOptions {
    folderName?: string;
    startPosition?: number;
}
export interface AddResult {
    added: number;
    skipped: number;
}
export interface QueueExecutionResult {
    trackResults: Record<string, QueueTrackStatus>;
    trackFilePaths?: Record<string, string>;
    successCount: number;
    skippedCount: number;
    failedCount: number;
    cancelled?: boolean;
    paused?: boolean;
}
export interface QueueResumeContext {
    allTracks: TrackMetadata[];
    trackFilePaths: Record<string, string>;
    trackResults: Record<string, QueueTrackStatus>;
}
export interface QueueTrackStatusSets {
    downloadedTracks: Set<string>;
    failedTracks: Set<string>;
    skippedTracks: Set<string>;
}
const LEGACY_STORAGE_KEY = "auralis_download_queue";
const listeners = new Set<(items: QueueItem[]) => void>();
const autoStartListeners = new Set<() => void>();
let cache: QueueItem[] = [];
let persistedCache: QueueItem[] = [];
let initializationPromise: Promise<void> | null = null;
let needsFullReplace = false;
let legacyMigrationPending = false;
let preserveCorruptStore = false;
let hasUnpersistedEdits = false;
function parseQueue(raw: string | null, source: string, rejectInvalidItems = false): QueueItem[] | null {
    if (raw === null || raw.trim() === "")
        return null;
    try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed))
            return null;
        const items = parsed.filter((item): item is QueueItem => isStoredQueueItem(item));
        if (items.length !== parsed.length) {
            console.error(`Download queue from ${source} contains invalid items.`);
            if (rejectInvalidItems) {
                return null;
            }
        }
        return items;
    }
    catch (err) {
        console.error(`Failed to parse download queue from ${source}:`, err);
        return null;
    }
}
function readLegacyQueue(): QueueItem[] | null {
    try {
        const raw = localStorage.getItem(LEGACY_STORAGE_KEY);
        if (raw === null || raw.trim() === "") return [];
        return parseQueue(raw, "legacy storage", true);
    }
    catch (err) {
        console.error("Failed to read the legacy download queue:", err);
        return null;
    }
}
function removeLegacyQueue(): void {
    try {
        localStorage.removeItem(LEGACY_STORAGE_KEY);
        legacyMigrationPending = false;
    }
    catch (err) {
        console.error("Failed to remove the migrated legacy download queue:", err);
    }
}
function restoreInterruptedItems(items: QueueItem[]): {
    items: QueueItem[];
    changed: boolean;
} {
    let changed = false;
    const restored = items.map((item) => {
        if (item.status !== "running")
            return item;
        changed = true;
        return { ...item, status: "paused" as QueueItemStatus };
    });
    return { items: restored, changed };
}
async function replacePersistentQueue(items: QueueItem[]): Promise<void> {
    await ReplacePersistentDownloadQueue(JSON.stringify(items));
    needsFullReplace = false;
    if (legacyMigrationPending) {
        removeLegacyQueue();
    }
}
function hasSameOrder(previous: QueueItem[], next: QueueItem[]): boolean {
    return previous.length === next.length && previous.every((item, index) => item.id === next[index]?.id);
}
async function persistChanges(previous: QueueItem[], next: QueueItem[]): Promise<void> {
    const previousByID = new Map(previous.map((item) => [item.id, item]));
    const nextIDs = new Set(next.map((item) => item.id));
    const upserts = next.filter((item) => previousByID.get(item.id) !== item);
    const removedIDs = previous.filter((item) => !nextIDs.has(item.id)).map((item) => item.id);
    const orderJSON = hasSameOrder(previous, next) ? "" : JSON.stringify(next.map((item) => item.id));
    if (upserts.length === 0 && removedIDs.length === 0 && orderJSON === "") {
        return;
    }
    await ApplyPersistentDownloadQueueChanges(JSON.stringify(upserts), removedIDs, orderJSON);
}
const queuePersistence = createQueuePersistenceController({
    isDirty: () => !preserveCorruptStore && (needsFullReplace || persistedCache !== cache),
    persist: async () => {
        const attemptedCache = cache;
        if (needsFullReplace) {
            await replacePersistentQueue(attemptedCache);
        }
        else {
            await persistChanges(persistedCache, attemptedCache);
        }
        persistedCache = attemptedCache;
    },
    onFailure: (err) => {
        needsFullReplace = true;
        console.error("Failed to persist download queue to the database:", err);
    },
});
function schedulePersistence(): void {
    if (preserveCorruptStore) {
        queuePersistence.failClosed();
        return;
    }
    queuePersistence.note();
}
export function getQueuePersistenceState(): QueuePersistenceState {
    return queuePersistence.getState();
}
export function subscribeQueuePersistence(listener: (state: QueuePersistenceState) => void): () => void {
    return queuePersistence.subscribe(listener);
}
export function retryQueuePersistence(): void {
    if (preserveCorruptStore) {
        queuePersistence.failClosed();
        return;
    }
    queuePersistence.retry();
}
export async function flushQueuePersistence(): Promise<boolean> {
    if (preserveCorruptStore) return !hasUnpersistedEdits;
    if (!needsFullReplace && persistedCache === cache) return true;
    if (queuePersistence.getState().phase === "unsaved") queuePersistence.retry();
    else schedulePersistence();
    await queuePersistence.whenSettled();
    const saved = !needsFullReplace && persistedCache === cache;
    if (saved) hasUnpersistedEdits = false;
    return saved;
}
export type { QueuePersistenceState };
export function initializeQueuePersistence(): Promise<void> {
    if (initializationPromise) {
        return initializationPromise;
    }
    initializationPromise = (async () => {
        let loaded: { ok: true; payload: string } | { ok: false };
        try {
            loaded = { ok: true, payload: await LoadPersistentDownloadQueue() };
        }
        catch (err) {
            console.error("Failed to load download queue database:", err);
            loaded = { ok: false };
        }
        const loadDecision = decidePersistentQueueLoad(loaded);
        if (!queueLoadWritesStore(loadDecision)) {
            if (loadDecision === "preserve-store") {
                console.error("Download queue database could not be read; leaving the stored queue untouched.");
            }
            else {
                console.error("Download queue database payload is corrupt; leaving the stored queue untouched.");
            }
            preserveCorruptStore = true;
            cache = [];
            queuePersistence.failClosed();
            return;
        }
        const databasePayload = loaded.ok ? loaded.payload : "";
        const loadPlan = loadDecision;
        if (loadPlan === "migrate-legacy") {
            const legacyQueue = readLegacyQueue();
            if (legacyQueue === null) {
                preserveCorruptStore = true;
                cache = [];
                queuePersistence.failClosed();
                return;
            }
            const restored = restoreInterruptedItems(legacyQueue);
            cache = restored.items;
            legacyMigrationPending = true;
            try {
                await replacePersistentQueue(cache);
                persistedCache = cache;
            }
            catch (err) {
                needsFullReplace = true;
                console.error("Failed to migrate download queue to the database:", err);
                schedulePersistence();
            }
            return;
        }
        const persisted = parseQueue(databasePayload, "database", true);
        if (!persisted) {
            console.error("Download queue database payload is corrupt; leaving the stored queue untouched.");
            preserveCorruptStore = true;
            cache = [];
            queuePersistence.failClosed();
            return;
        }
        const restored = restoreInterruptedItems(persisted);
        cache = restored.items;
        let rawCount = persisted.length;
        try {
            const rawParsed = JSON.parse(databasePayload);
            if (Array.isArray(rawParsed)) {
                rawCount = rawParsed.length;
            }
        }
        catch {
            rawCount = persisted.length;
        }
        if (restored.changed || cache.length !== rawCount) {
            try {
                await replacePersistentQueue(cache);
                persistedCache = cache;
            }
            catch (err) {
                needsFullReplace = true;
                console.error("Failed to persist the restored download queue:", err);
                schedulePersistence();
            }
        }
        else {
            persistedCache = cache;
        }
        removeLegacyQueue();
    })();
    return initializationPromise;
}
function read(): QueueItem[] {
    return cache;
}
function write(items: QueueItem[]): void {
    hasUnpersistedEdits = true;
    cache = items;
    schedulePersistence();
    for (const listener of listeners) {
        listener(items);
    }
}
export function getQueue(): QueueItem[] {
    return read();
}
export function subscribeQueue(listener: (items: QueueItem[]) => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}
export function subscribeQueueAutoStart(listener: () => void): () => void {
    autoStartListeners.add(listener);
    return () => {
        autoStartListeners.delete(listener);
    };
}
function notifyQueueAdded(): void {
    for (const listener of autoStartListeners) {
        listener();
    }
}
function getTrackKey(track: TrackMetadata): string {
    return `track:${track.spotify_id || track.external_urls || `${track.name}-${track.artists}-${track.album_name}`}`;
}
function getCollectionKey(input: AddCollectionInput): string {
    return `${input.type}:${input.folderName || input.name}`;
}
function createId(): string {
    return crypto.randomUUID();
}
function mergeByKey(items: QueueItem[], incoming: QueueItem): {
    items: QueueItem[];
    added: boolean;
} {
    const existingIndex = items.findIndex((item) => item.key === incoming.key);
    if (existingIndex === -1) {
        return { items: [...items, incoming], added: true };
    }
    const existing = items[existingIndex];
    if (existing.status === "pending" || existing.status === "running" || existing.status === "paused") {
        return { items, added: false };
    }
    const next = [...items];
    next[existingIndex] = { ...incoming, id: existing.id };
    return { items: next, added: true };
}
function beginRunningItem(incoming: QueueItem): string {
    const items = read();
    const existingIndex = items.findIndex((item) => item.key === incoming.key);
    if (existingIndex === -1) {
        write([...items, incoming]);
        return incoming.id;
    }
    const next = [...items];
    const id = next[existingIndex].id;
    next[existingIndex] = { ...incoming, id };
    write(next);
    return id;
}
export function beginDirectTrackQueueItem(track: TrackMetadata, options: AddTracksOptions = {}): string {
    return beginRunningItem({
        id: createId(), key: getTrackKey(track), type: "track", name: track.name,
        artist: track.artists, info: track.album_name || "", image: track.images || "",
        folderName: options.folderName, position: options.startPosition || track.track_number,
        durationMs: track.duration_ms, trackCount: 1, tracks: [track], status: "running", addedAt: Date.now(),
    });
}
export function beginDirectCollectionQueueItem(input: AddCollectionInput): string {
    const tracks = input.tracks.filter((track) => track.spotify_id);
    return beginRunningItem({
        id: createId(), key: getCollectionKey(input), type: input.type, name: input.name,
        artist: input.artist, info: input.info, image: input.image, folderName: input.folderName,
        isAlbum: input.isAlbum, durationMs: tracks.reduce((sum, track) => sum + (track.duration_ms || 0), 0),
        trackCount: tracks.length, tracks, status: "running", addedAt: Date.now(),
    });
}
export function mergeQueueTrackResults(previous?: Record<string, QueueTrackStatus>, incoming?: Record<string, QueueTrackStatus>): Record<string, QueueTrackStatus> {
    return { ...(previous || {}), ...(incoming || {}) };
}
export function mergeQueueTrackFilePaths(previous?: Record<string, string>, incoming?: Record<string, string>): Record<string, string> {
    return { ...(previous || {}), ...(incoming || {}) };
}
export function getQueueTrackStatusSets(items: QueueItem[] = read()): QueueTrackStatusSets {
    const downloadedTracks = new Set<string>();
    const failedTracks = new Set<string>();
    const skippedTracks = new Set<string>();
    for (const item of items) {
        for (const [trackId, status] of Object.entries(item.trackResults || {})) {
            if (!trackId)
                continue;
            if (status === "done") {
                downloadedTracks.add(trackId);
            }
            else if (status === "skipped") {
                skippedTracks.add(trackId);
                downloadedTracks.add(trackId);
            }
            else if (status === "failed") {
                failedTracks.add(trackId);
            }
        }
    }
    return { downloadedTracks, failedTracks, skippedTracks };
}
export function getRemainingQueueTracks(item: Pick<QueueItem, "tracks" | "trackResults">): TrackMetadata[] {
    const results = item.trackResults || {};
    return item.tracks.filter((track) => {
        const id = track.spotify_id || "";
        return Boolean(id) && !results[id];
    });
}
export function summarizeQueueTrackResults(tracks: TrackMetadata[], trackResults: Record<string, QueueTrackStatus>, flags: Pick<QueueExecutionResult, "cancelled" | "paused"> = {}): QueueExecutionResult {
    let successCount = 0;
    let skippedCount = 0;
    let failedCount = 0;
    for (const track of tracks) {
        const id = track.spotify_id || "";
        const status = id ? trackResults[id] : undefined;
        if (status === "done")
            successCount += 1;
        else if (status === "skipped")
            skippedCount += 1;
        else if (status === "failed")
            failedCount += 1;
    }
    return { trackResults, successCount, skippedCount, failedCount, ...flags };
}
export function getQueueItemStatus(result: QueueExecutionResult, hasRemaining: boolean): QueueItemStatus {
    return queueItemStatusAfterAttempt(result, hasRemaining);
}
export function finishDirectQueueItem(id: string, result: QueueExecutionResult): void {
    const item = read().find((candidate) => candidate.id === id);
    const trackResults = mergeQueueTrackResults(item?.trackResults, result.trackResults);
    const trackFilePaths = mergeQueueTrackFilePaths(item?.trackFilePaths, result.trackFilePaths);
    const hasRemaining = item ? getRemainingQueueTracks({ tracks: item.tracks, trackResults }).length > 0 : result.cancelled === true || result.paused === true;
    const summary = item
        ? summarizeQueueTrackResults(item.tracks, trackResults, { cancelled: result.cancelled, paused: result.paused })
        : { ...result, trackResults };
    updateQueueItem(id, { status: getQueueItemStatus(summary, hasRemaining), trackResults, trackFilePaths });
}
export function addTracksToQueue(tracks: TrackMetadata[], options: AddTracksOptions = {}): AddResult {
    const queueable = tracks.filter((track) => track.spotify_id);
    if (queueable.length === 0) {
        return { added: 0, skipped: 0 };
    }
    let items = read();
    let added = 0;
    queueable.forEach((track, index) => {
        const item: QueueItem = {
            id: createId(),
            key: getTrackKey(track),
            type: "track",
            name: track.name,
            artist: track.artists,
            info: track.album_name || "",
            image: track.images || "",
            folderName: options.folderName,
            position: options.startPosition ? options.startPosition + index : track.track_number,
            durationMs: track.duration_ms,
            trackCount: 1,
            tracks: [track],
            status: "pending",
            addedAt: Date.now(),
        };
        const result = mergeByKey(items, item);
        items = result.items;
        if (result.added) {
            added += 1;
        }
    });
    write(items);
    if (added > 0) {
        notifyQueueAdded();
    }
    return { added, skipped: queueable.length - added };
}
export function addCollectionToQueue(input: AddCollectionInput): AddResult {
    const queueable = input.tracks.filter((track) => track.spotify_id);
    if (queueable.length === 0) {
        return { added: 0, skipped: 0 };
    }
    const item: QueueItem = {
        id: createId(),
        key: getCollectionKey(input),
        type: input.type,
        name: input.name,
        artist: input.artist,
        info: input.info,
        image: input.image,
        folderName: input.folderName,
        isAlbum: input.isAlbum,
        durationMs: queueable.reduce((sum, track) => sum + (track.duration_ms || 0), 0),
        trackCount: queueable.length,
        tracks: queueable,
        status: "pending",
        addedAt: Date.now(),
    };
    const result = mergeByKey(read(), item);
    if (result.added) {
        write(result.items);
        notifyQueueAdded();
        return { added: 1, skipped: 0 };
    }
    return { added: 0, skipped: 1 };
}
export function updateQueueItem(id: string, patch: Partial<Pick<QueueItem, "status" | "error" | "trackResults" | "trackFilePaths">>): void {
    const items = read();
    const index = items.findIndex((item) => item.id === id);
    if (index === -1)
        return;
    const next = [...items];
    next[index] = { ...next[index], ...patch };
    write(next);
}
export function updateQueueTrackResult(itemId: string, trackId: string, status: QueueTrackStatus, filePath?: string): void {
    if (!trackId)
        return;
    const items = read();
    const index = items.findIndex((item) => item.id === itemId);
    if (index === -1)
        return;
    const item = items[index];
    const trackResults = mergeQueueTrackResults(item.trackResults, { [trackId]: status });
    const trackFilePaths = filePath
        ? mergeQueueTrackFilePaths(item.trackFilePaths, { [trackId]: filePath })
        : item.trackFilePaths;
    const next = [...items];
    next[index] = { ...item, trackResults, trackFilePaths };
    write(next);
}
export function removeQueueItem(id: string): void {
    const items = read();
    const target = items.find((item) => item.id === id);
    if (!target || isProtectedQueueStatus(target.status))
        return;
    write(items.filter((item) => item.id !== id));
}
export function removeTrackFromQueueItem(itemId: string, trackIndex: number): void {
    const items = read();
    const index = items.findIndex((item) => item.id === itemId);
    if (index === -1)
        return;
    const item = items[index];
    if (isProtectedQueueStatus(item.status))
        return;
    if (trackIndex < 0 || trackIndex >= item.tracks.length)
        return;
    const remaining = item.tracks.filter((_, position) => position !== trackIndex);
    if (remaining.length === 0) {
        write(items.filter((candidate) => candidate.id !== itemId));
        return;
    }
    const next = [...items];
    next[index] = {
        ...item,
        tracks: remaining,
        trackCount: remaining.length,
        durationMs: remaining.reduce((sum, track) => sum + (track.duration_ms || 0), 0),
    };
    write(next);
}
export function retryQueueItem(id: string): void {
    const item = read().find((candidate) => candidate.id === id);
    if (!item || item.status === "running")
        return;
    const preservedResults = Object.fromEntries(Object.entries(item.trackResults || {}).filter(([, status]) => status === "done" || status === "skipped"));
    const preservedPaths = Object.fromEntries(Object.entries(item.trackFilePaths || {}).filter(([trackId]) => Boolean(preservedResults[trackId])));
    updateQueueItem(id, { status: "pending", error: "", trackResults: preservedResults, trackFilePaths: preservedPaths });
    notifyQueueAdded();
}
export function clearQueue(type?: QueueItemType): void {
    const items = read();
    const next = retainProtectedQueueItems(items, type);
    if (next.length === items.length)
        return;
    write(next);
}
export function clearFinishedQueueItems(type?: QueueItemType): void {
    const items = read();
    const next = items.filter((item) => {
        const isFinished = item.status === "done" || item.status === "partial" || item.status === "skipped" || item.status === "failed";
        const matchesType = !type || item.type === type;
        return !(isFinished && matchesType);
    });
    if (next.length === items.length)
        return;
    write(next);
}
export function getNextRunnableQueueItem(type?: QueueItemType): QueueItem | undefined {
    return nextManualQueueItem(read(), type);
}
export function getNextPendingQueueItem(): QueueItem | undefined {
    return nextAutomaticQueueItem(read());
}
export function countRunnableQueueItems(type?: QueueItemType): number {
    return read().filter((item) => (item.status === "paused" || item.status === "pending") && (!type || item.type === type)).length;
}
