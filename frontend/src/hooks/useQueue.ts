import { t, translateMessage } from "@/i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import { downloadExecution, type DownloadExecutionKind } from "@/lib/download-execution";
import { planQueueEntry, queueControlApplies, queueRunHaltsBeforeNextItem, singleTrackQueueOutcome, type QueueAttemptDisposition } from "@/lib/queue-guards";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { getNextPendingQueueItem, getNextRunnableQueueItem, getQueue, getQueueItemStatus, getRemainingQueueTracks, mergeQueueTrackFilePaths, mergeQueueTrackResults, subscribeQueue, subscribeQueueAutoStart, summarizeQueueTrackResults, updateQueueItem, type QueueExecutionResult, type QueueItem, type QueueItemStatus, type QueueItemType, type QueueResumeContext } from "@/lib/queue";
import type { TrackMetadata } from "@/types/api";
import { removeQueueItem } from "@/lib/queue";
interface QueueDownloadHandlers {
    handleDownloadTrack: (id: string, trackName?: string, artistName?: string, albumName?: string, spotifyId?: string, playlistName?: string, durationMs?: number, position?: number, albumArtist?: string, releaseDate?: string, coverUrl?: string, spotifyTrackNumber?: number, spotifyDiscNumber?: number, spotifyTotalTracks?: number, spotifyTotalDiscs?: number, copyright?: string, publisher?: string, queueItemId?: string) => Promise<QueueAttemptDisposition | undefined>;
    handleDownloadAll: (tracks: TrackMetadata[], folderName?: string, isAlbum?: boolean, batchSource?: "playlist" | "album" | "discography" | "collection", queueItemId?: string, resumeContext?: QueueResumeContext) => Promise<QueueExecutionResult | undefined>;
    handlePauseDownload: (owner?: DownloadExecutionKind) => void;
    handleResumeDownload: (owner?: DownloadExecutionKind) => void;
    handleStopDownload: (owner?: DownloadExecutionKind) => void;
}
function batchSourceFor(item: QueueItem): "playlist" | "album" | "discography" | "collection" {
    if (item.type === "album")
        return "album";
    if (item.type === "playlist")
        return "playlist";
    if (item.type === "artist")
        return "discography";
    return "collection";
}
export function useQueue(download: QueueDownloadHandlers) {
    const [items, setItems] = useState<QueueItem[]>(() => getQueue());
    const [isProcessing, setIsProcessing] = useState(false);
    const [isPausing, setIsPausing] = useState(false);
    const [isSuspended, setIsSuspended] = useState(() => getQueue().some((item) => item.status === "paused"));
    const suspendedRef = useRef(isSuspended);
    const [processingType, setProcessingType] = useState<QueueItemType | null>(null);
    const shouldStopRef = useRef(false);
    const shouldPauseRef = useRef(false);
    const isProcessingRef = useRef(false);
    const processingTypeRef = useRef<QueueItemType | null>(null);
    const activeItemRef = useRef<QueueItem | null>(null);
    useEffect(() => subscribeQueue(setItems), []);
    const runItem = useCallback(async (item: QueueItem): Promise<QueueItemStatus> => {
        const remainingTracks = getRemainingQueueTracks(item);
        let result: QueueExecutionResult;
        if (remainingTracks.length === 0) {
            result = summarizeQueueTrackResults(item.tracks, item.trackResults || {});
        }
        else if (item.type === "track") {
            const track = remainingTracks[0];
            if (!track?.spotify_id) {
                throw new Error(t("translation.download.noIdFoundTrack"));
            }
            const disposition = await download.handleDownloadTrack(track.spotify_id, track.name, track.artists, track.album_name, track.spotify_id, item.folderName, track.duration_ms, item.position, track.album_artist, track.release_date, track.images, track.track_number, track.disc_number, track.total_tracks, track.total_discs, track.copyright, track.publisher, item.id);
            result = singleTrackQueueOutcome(track.spotify_id, disposition ?? "cancelled");
        }
        else {
            result = await download.handleDownloadAll(remainingTracks, item.folderName, item.isAlbum, batchSourceFor(item), item.id, {
                allTracks: item.tracks,
                trackFilePaths: item.trackFilePaths || {},
                trackResults: item.trackResults || {},
            }) || { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, cancelled: true };
        }
        const trackResults = mergeQueueTrackResults(item.trackResults, result.trackResults);
        const trackFilePaths = mergeQueueTrackFilePaths(item.trackFilePaths, result.trackFilePaths);
        const summary = summarizeQueueTrackResults(item.tracks, trackResults, {
            cancelled: result.cancelled,
            paused: result.paused,
        });
        const hasRemaining = getRemainingQueueTracks({ tracks: item.tracks, trackResults }).length > 0;
        const status = getQueueItemStatus(summary, hasRemaining);
        updateQueueItem(item.id, { trackResults, trackFilePaths });
        if (status === "failed") {
            throw new Error(item.type === "track"
                ? t("translation.download.downloadFailed")
                : t("translation.queue.value1TracksFailed", { value1: summary.failedCount.toLocaleString() }));
        }
        return status;
    }, [download]);
    const runQueue = useCallback(async (type: QueueItemType | undefined, itemId: string | undefined, pendingOnly: boolean) => {
        while (downloadExecution.isBlocked()) {
            await downloadExecution.whenUnblocked();
        }
        const plan = planQueueEntry({
            pendingOnly,
            queueBusy: isProcessingRef.current,
            queuePaused: shouldPauseRef.current || suspendedRef.current,
            queueStopped: shouldStopRef.current,
            activeKind: downloadExecution.activeKind(),
        });
        if (plan === "defer-auto-start") {
            downloadExecution.armAutoStart();
            return;
        }
        if (plan === "follow-up") {
            downloadExecution.armFollowUp();
            return;
        }
        if (plan === "ignore")
            return;
        const lease = downloadExecution.tryAcquire("queue");
        if (!lease) {
            if (pendingOnly)
                downloadExecution.armAutoStart();
            return;
        }
        const getNextItem = () => {
            if (itemId) {
                return getQueue().find((item) => item.id === itemId && (item.status === "paused" || item.status === "pending"));
            }
            return pendingOnly ? getNextPendingQueueItem() : getNextRunnableQueueItem(type);
        };
        isProcessingRef.current = true;
        try {
            const initialItem = getNextItem();
            if (!initialItem) {
                if (!pendingOnly) {
                    toast.info(t("translation.queue.nothingQueued"));
                }
                return;
            }
            const effectiveType = type ?? (itemId ? initialItem.type : null);
            shouldStopRef.current = false;
            shouldPauseRef.current = false;
            suspendedRef.current = false;
            setIsSuspended(false);
            download.handleResumeDownload("queue");
            processingTypeRef.current = effectiveType;
            setProcessingType(effectiveType);
            setIsPausing(false);
            setIsProcessing(true);
            for (let item: QueueItem | undefined = initialItem; item; item = itemId ? undefined : getNextItem()) {
                if (shouldStopRef.current || shouldPauseRef.current) {
                    break;
                }
                activeItemRef.current = item;
                updateQueueItem(item.id, { status: "running", error: "" });
                try {
                    const status = await runItem(item);
                    updateQueueItem(item.id, { status });
                    if (queueRunHaltsBeforeNextItem(status) || downloadExecution.isPauseRequested()) {
                        shouldPauseRef.current = true;
                        suspendedRef.current = true;
                        setIsSuspended(true);
                        setIsPausing(true);
                    }
                }
                catch (err) {
                    const message = translateMessage(err instanceof Error ? err.message : String(err));
                    updateQueueItem(item.id, { status: "failed", error: message });
                }
                finally {
                    activeItemRef.current = null;
                }
                if (shouldStopRef.current) {
                    // Remove only after the transfer has settled and released its
                    // running status. Keep later requests for explicit resume.
                    updateQueueItem(item.id, { status: "paused" });
                    removeQueueItem(item.id);
                }
                if (shouldStopRef.current || shouldPauseRef.current) {
                    break;
                }
            }
        }
        finally {
            const paused = shouldPauseRef.current;
            const stopped = shouldStopRef.current;
            if (paused || stopped) {
                // Persist the hold on the next request so a restart still waits
                // for an explicit Resume after a pause or a cancel.
                const nextItem = getNextItem();
                if (nextItem) {
                    if (nextItem.status === "pending") {
                        updateQueueItem(nextItem.id, { status: "paused" });
                    }
                    if (!stopped)
                        toast.info(t("translation.queue.pauseCompleted"));
                }
            }
            const followAdded = downloadExecution.consumeFollowUp(paused, stopped);
            isProcessingRef.current = false;
            processingTypeRef.current = null;
            activeItemRef.current = null;
            setIsProcessing(false);
            setIsPausing(false);
            setProcessingType(null);
            shouldStopRef.current = false;
            shouldPauseRef.current = false;
            downloadExecution.release(lease);
            if (followAdded) {
                queueMicrotask(() => {
                    void runQueue(undefined, undefined, true);
                });
            }
        }
    }, [download, runItem]);
    const start = useCallback((type?: QueueItemType, itemId?: string) => runQueue(type, itemId, false), [runQueue]);
    useEffect(() => subscribeQueueAutoStart(() => {
        void runQueue(undefined, undefined, true);
    }), [runQueue]);
    useEffect(() => downloadExecution.subscribe((event) => {
        if (event.autoStart)
            void runQueue(undefined, undefined, true);
    }), [runQueue]);
    const startItem = useCallback((itemId: string) => start(undefined, itemId), [start]);
    const pause = useCallback((type?: QueueItemType) => {
        if (!isProcessingRef.current || shouldPauseRef.current)
            return;
        if (!queueControlApplies(processingTypeRef.current, type))
            return;
        if (downloadExecution.activeKind() !== "queue")
            return;
        shouldPauseRef.current = true;
        suspendedRef.current = true;
        setIsSuspended(true);
        setIsPausing(true);
        if (activeItemRef.current?.type !== "track") {
            download.handlePauseDownload("queue");
        }
    }, [download]);
    const stop = useCallback((type?: QueueItemType) => {
        if (!isProcessingRef.current)
            return;
        if (!queueControlApplies(processingTypeRef.current, type))
            return;
        if (downloadExecution.activeKind() !== "queue")
            return;
        shouldStopRef.current = true;
        suspendedRef.current = true;
        setIsSuspended(true);
        shouldPauseRef.current = false;
        setIsPausing(false);
        download.handleStopDownload("queue");
    }, [download]);
    return { items, isProcessing, isPausing, isSuspended, processingType, start, startItem, pause, stop };
}
