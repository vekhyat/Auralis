export type QueueExecutionKind = "direct" | "queue";

export interface QueueEntryPlanInput {
    pendingOnly: boolean;
    queueBusy: boolean;
    queuePaused: boolean;
    queueStopped: boolean;
    activeKind: QueueExecutionKind | null;
}

export type QueueEntryPlan = "start" | "defer-auto-start" | "follow-up" | "ignore";

export function planQueueEntry(input: QueueEntryPlanInput): QueueEntryPlan {
    // An explicit pause/cancel remains in effect even after the worker releases
    // its lease. Adding or retrying music must not silently resume transfers.
    if (input.pendingOnly && (input.queuePaused || input.queueStopped)) return "ignore";
    if (input.activeKind === "direct") {
        return input.pendingOnly ? "defer-auto-start" : "ignore";
    }
    if (input.queueBusy) {
        if (input.pendingOnly && !input.queuePaused && !input.queueStopped) return "follow-up";
        return "ignore";
    }
    return "start";
}

export function queueControlApplies(processingType: string | null, requestedType: string | undefined): boolean {
    if (requestedType === undefined) return true;
    return processingType === requestedType;
}

export function isMixedQueueRun(isProcessing: boolean, processingType: string | null): boolean {
    return isProcessing && processingType === null;
}

export function showsTabQueueControls(isProcessing: boolean, processingType: string | null, activeTab: string): boolean {
    return isProcessing && processingType !== null && processingType === activeTab;
}

export function isProtectedQueueStatus(status: string): boolean {
    return status === "running";
}

export function retainProtectedQueueItems<T extends {
    status: string;
    type: string;
}>(items: T[], type?: string): T[] {
    return items.filter((item) => isProtectedQueueStatus(item.status) || (type !== undefined && item.type !== type));
}

export type QueueAttemptDisposition = "done" | "skipped" | "failed" | "cancelled" | "paused";

export interface QueueAttemptOutcome {
    trackResults: Record<string, "done" | "skipped" | "failed">;
    successCount: number;
    skippedCount: number;
    failedCount: number;
    cancelled?: boolean;
    paused?: boolean;
}

export function singleTrackQueueOutcome(trackId: string, disposition: QueueAttemptDisposition): QueueAttemptOutcome {
    if (disposition === "paused") {
        return { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, paused: true };
    }
    if (disposition === "cancelled") {
        return { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, cancelled: true };
    }
    return {
        trackResults: trackId ? { [trackId]: disposition } : {},
        successCount: disposition === "done" ? 1 : 0,
        skippedCount: disposition === "skipped" ? 1 : 0,
        failedCount: disposition === "failed" ? 1 : 0,
    };
}

export function queueItemStatusAfterAttempt(result: {
    paused?: boolean;
    cancelled?: boolean;
    failedCount: number;
    successCount: number;
    skippedCount: number;
}, hasRemaining: boolean): "paused" | "partial" | "failed" | "skipped" | "done" {
    if ((result.paused || result.cancelled) && hasRemaining)
        return "paused";
    if (result.failedCount > 0 && result.successCount + result.skippedCount > 0)
        return "partial";
    if (result.failedCount > 0)
        return "failed";
    if (result.skippedCount > 0 && result.successCount === 0)
        return "skipped";
    return "done";
}

export function queueRunHaltsBeforeNextItem(status: string): boolean {
    return status === "paused";
}

export function isAutomaticallyRunnableQueueStatus(status: string): boolean {
    return status === "pending";
}

export function isManuallyRunnableQueueStatus(status: string): boolean {
    return status === "pending" || status === "paused";
}

export function nextAutomaticQueueItem<T extends {
    status: string;
}>(items: T[]): T | undefined {
    return items.find((item) => isAutomaticallyRunnableQueueStatus(item.status));
}

export function nextManualQueueItem<T extends {
    status: string;
    type: string;
}>(items: T[], type?: string): T | undefined {
    return items.find((item) => isManuallyRunnableQueueStatus(item.status) && (!type || item.type === type));
}

/** Expected length in seconds for an existence check. Non-positive values are omitted. */
export function expectedTrackDurationSeconds(durationMs?: number): number | undefined {
    if (durationMs == null || !Number.isFinite(durationMs) || durationMs <= 0)
        return undefined;
    return Math.round(durationMs / 1000);
}
