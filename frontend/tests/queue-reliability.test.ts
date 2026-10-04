import assert from "node:assert/strict";
import { test } from "node:test";
import { DownloadExecutionCoordinator } from "../src/lib/download-execution.ts";

test("adding downloads cannot resume a suspended worker after its lease is released", () => {
    for (const activeKind of [null, "direct", "queue"] as const) {
        assert.equal(planQueueEntry({ pendingOnly: true, queueBusy: false, queuePaused: true, queueStopped: false, activeKind }), "ignore");
        assert.equal(planQueueEntry({ pendingOnly: true, queueBusy: false, queuePaused: false, queueStopped: true, activeKind }), "ignore");
    }
    assert.equal(planQueueEntry({ pendingOnly: false, queueBusy: false, queuePaused: true, queueStopped: false, activeKind: null }), "start");
});
import { expectedTrackDurationSeconds, isAutomaticallyRunnableQueueStatus, isManuallyRunnableQueueStatus, isMixedQueueRun, isProtectedQueueStatus, nextAutomaticQueueItem, nextManualQueueItem, planQueueEntry, queueControlApplies, queueItemStatusAfterAttempt, queueRunHaltsBeforeNextItem, retainProtectedQueueItems, showsTabQueueControls, singleTrackQueueOutcome, } from "../src/lib/queue-guards.ts";
import { canRetryQueuePersistence, createQueuePersistenceController, decidePersistentQueueLoad, inspectStoredQueue, persistenceBackoffMs, queueLoadWritesStore, queuePersistenceNotice, storedQueueLoadPlan, } from "../src/lib/queue-persistence.ts";
const QUEUE_ATTEMPTS = 4;

test("the next lease waits until an in-flight stop has finished", async () => {
    const execution = new DownloadExecutionCoordinator();
    let finishStop: () => void = () => {};
    const stopping = new Promise<void>((resolve) => {
        finishStop = resolve;
    });
    const direct = execution.tryAcquire("direct");
    assert.ok(direct);
    assert.equal(execution.requestStop("direct"), true);
    execution.blockFor(stopping);
    execution.release(direct);
    const pending = execution.acquireWhenUnblocked("queue");
    await Promise.resolve();
    assert.equal(execution.activeKind(), null);
    assert.equal(execution.isBlocked(), true);
    finishStop();
    const queue = await pending;
    assert.equal(queue.kind, "queue");
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.isPauseRequested(), false);
    assert.equal(execution.isBlocked(), false);
});

test("acquire grants the only token before its promise yields", async () => {
    const execution = new DownloadExecutionCoordinator();
    const pending = execution.acquire("direct");
    assert.equal(execution.activeKind(), "direct");
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.isPauseRequested(), false);
    const lease = await pending;
    assert.equal(lease.kind, "direct");
    assert.equal(lease.id, 1);
    execution.release(lease);
    assert.equal(execution.activeKind(), null);
    const next = execution.acquireWhenUnblocked("direct");
    assert.equal(execution.activeKind(), "direct");
    assert.equal(execution.isStopRequested(), false);
    execution.release(await next);
});

test("a waiting direct download is granted before queued auto-start, and the queue lease does not inherit stop", async () => {
    const execution = new DownloadExecutionCoordinator();
    const first = execution.tryAcquire("direct");
    assert.ok(first);
    execution.armAutoStart();
    execution.requestStop("direct");
    const notices: Array<{ releasedKind: string; autoStart: boolean }> = [];
    execution.subscribe((event) => {
        notices.push({ releasedKind: event.releasedKind, autoStart: event.autoStart });
        if (!event.autoStart) return;
        assert.equal(planQueueEntry({
            pendingOnly: true,
            queueBusy: false,
            queuePaused: false,
            queueStopped: false,
            activeKind: execution.activeKind(),
        }), "start");
        const queue = execution.tryAcquire("queue");
        assert.ok(queue);
        assert.equal(execution.isStopRequested(), false);
        assert.equal(execution.isPauseRequested(), false);
    });
    let secondGranted = false;
    const second = execution.acquire("direct").then((lease) => {
        secondGranted = true;
        return lease;
    });
    execution.release(first);
    assert.equal(secondGranted, false);
    assert.equal(execution.activeKind(), "direct");
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.isPauseRequested(), false);
    assert.deepEqual(notices, [{ releasedKind: "direct", autoStart: false }]);
    const secondLease = await second;
    assert.equal(secondGranted, true);
    execution.release(secondLease);
    assert.deepEqual(notices, [
        { releasedKind: "direct", autoStart: false },
        { releasedKind: "direct", autoStart: true },
    ]);
    assert.equal(execution.activeKind(), "queue");
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.isPauseRequested(), false);
});

test("pause and stop do not cross between a direct download and the queue", () => {
    const execution = new DownloadExecutionCoordinator();
    const direct = execution.tryAcquire("direct");
    assert.ok(direct);
    assert.equal(execution.requestPause("queue"), false);
    assert.equal(execution.requestStop("queue"), false);
    assert.equal(execution.isPauseRequested(), false);
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.requestPause("direct"), true);
    assert.equal(execution.requestStop("direct"), true);
    assert.equal(execution.isStopRequested(), true);
    assert.equal(execution.isPauseRequested(), false);
    execution.release(direct);
    const queue = execution.tryAcquire("queue");
    assert.ok(queue);
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.isPauseRequested(), false);
    assert.equal(execution.requestStop("direct"), false);
    assert.equal(execution.requestPause("direct"), false);
    assert.equal(execution.isStopRequested(), false);
    assert.equal(execution.requestPause("queue"), true);
    assert.equal(execution.pauseActive(), true);
    execution.release({ id: queue.id + 1, kind: "queue" });
    assert.equal(execution.activeKind(), "queue");
    assert.equal(execution.isPauseRequested(), true);
    execution.release(queue);
    assert.equal(execution.activeKind(), null);
});

test("queued auto-start does not fire when the released lease is the queue itself", () => {
    const execution = new DownloadExecutionCoordinator();
    execution.armAutoStart();
    const queue = execution.tryAcquire("queue");
    assert.ok(queue);
    const notices: boolean[] = [];
    execution.subscribe((event) => notices.push(event.autoStart));
    execution.release(queue);
    assert.deepEqual(notices, [false]);
    assert.equal(execution.activeKind(), null);
    const direct = execution.tryAcquire("direct");
    assert.ok(direct);
    execution.release(direct);
    assert.deepEqual(notices, [false, true]);
    execution.release(direct);
    assert.deepEqual(notices, [false, true]);
});

test("follow-up is consumed only when that queue run was not paused or stopped", () => {
    const execution = new DownloadExecutionCoordinator();
    const running = execution.tryAcquire("queue");
    assert.ok(running);
    execution.armFollowUp();
    const follow = execution.consumeFollowUp(false, false);
    execution.release(running);
    assert.equal(follow, true);
    assert.equal(execution.activeKind(), null);
    execution.armFollowUp();
    assert.equal(execution.consumeFollowUp(true, false), false);
    execution.armFollowUp();
    assert.equal(execution.consumeFollowUp(false, true), false);
    assert.equal(execution.consumeFollowUp(false, false), false);
});

test("queue entry waits for a direct download, follows a running queue, and ignores a manual start", () => {
    assert.equal(planQueueEntry({
        pendingOnly: true,
        queueBusy: false,
        queuePaused: false,
        queueStopped: false,
        activeKind: "direct",
    }), "defer-auto-start");
    assert.equal(planQueueEntry({
        pendingOnly: false,
        queueBusy: false,
        queuePaused: false,
        queueStopped: false,
        activeKind: "direct",
    }), "ignore");
    assert.equal(planQueueEntry({
        pendingOnly: true,
        queueBusy: true,
        queuePaused: false,
        queueStopped: false,
        activeKind: "queue",
    }), "follow-up");
    assert.equal(planQueueEntry({
        pendingOnly: true,
        queueBusy: true,
        queuePaused: true,
        queueStopped: false,
        activeKind: "queue",
    }), "ignore");
    assert.equal(planQueueEntry({
        pendingOnly: true,
        queueBusy: true,
        queuePaused: false,
        queueStopped: true,
        activeKind: "queue",
    }), "ignore");
    assert.equal(planQueueEntry({
        pendingOnly: false,
        queueBusy: true,
        queuePaused: false,
        queueStopped: false,
        activeKind: "queue",
    }), "ignore");
    assert.equal(planQueueEntry({
        pendingOnly: true,
        queueBusy: false,
        queuePaused: false,
        queueStopped: false,
        activeKind: null,
    }), "start");
});

test("a mixed run takes global pause and stop, not a tab control", () => {
    assert.equal(queueControlApplies(null, undefined), true);
    assert.equal(queueControlApplies("album", undefined), true);
    assert.equal(queueControlApplies(null, "album"), false);
    assert.equal(queueControlApplies("album", "album"), true);
    assert.equal(queueControlApplies("album", "track"), false);
    assert.equal(isMixedQueueRun(true, null), true);
    assert.equal(isMixedQueueRun(true, "album"), false);
    assert.equal(isMixedQueueRun(false, null), false);
    assert.equal(showsTabQueueControls(true, null, "album"), false);
    assert.equal(showsTabQueueControls(true, "album", "album"), true);
    assert.equal(showsTabQueueControls(true, "album", "track"), false);
    assert.equal(showsTabQueueControls(false, "album", "album"), false);
});

test("running items stay in the queue when a tab or the whole queue is cleared", () => {
    const items = [
        { id: "run-album", status: "running", type: "album" },
        { id: "pend-album", status: "pending", type: "album" },
        { id: "pause-track", status: "paused", type: "track" },
        { id: "done-album", status: "done", type: "album" },
    ];
    assert.equal(isProtectedQueueStatus("running"), true);
    assert.equal(isProtectedQueueStatus("paused"), false);
    assert.equal(isProtectedQueueStatus("pending"), false);
    assert.deepEqual(retainProtectedQueueItems(items).map((item) => item.id), ["run-album"]);
    assert.deepEqual(retainProtectedQueueItems(items, "album").map((item) => item.id), ["run-album", "pause-track"]);
    assert.deepEqual(retainProtectedQueueItems(items, "track").map((item) => item.id), ["run-album", "pend-album", "done-album"]);
});

test("corrupt stored queues are left untouched and an empty store can migrate", () => {
    const item = { id: "a", key: "track:a", type: "track", name: "Track", artist: "Artist", info: "", image: "", trackCount: 0, tracks: [], status: "pending", addedAt: 1 };
    assert.equal(storedQueueLoadPlan(""), "migrate-legacy");
    assert.equal(storedQueueLoadPlan("   "), "migrate-legacy");
    assert.equal(storedQueueLoadPlan("not-json"), "preserve-corrupt");
    assert.equal(storedQueueLoadPlan("{}"), "preserve-corrupt");
    assert.equal(storedQueueLoadPlan("[{}]"), "preserve-corrupt");
    assert.equal(storedQueueLoadPlan("[{\"id\":1,\"tracks\":[]}]"), "preserve-corrupt");
    assert.equal(storedQueueLoadPlan("[{\"id\":\"a\"}]"), "preserve-corrupt");
    assert.equal(storedQueueLoadPlan(JSON.stringify([item])), "read-database");
    assert.equal(inspectStoredQueue(JSON.stringify([{ ...item, tracks: [{ name: "t", artists: "Artist" }] }])), "usable");
    assert.equal(inspectStoredQueue(JSON.stringify([{ ...item, name: null }])), "corrupt");
    assert.equal(inspectStoredQueue(JSON.stringify([{ ...item, tracks: [null] }])), "corrupt");
    assert.equal(inspectStoredQueue(JSON.stringify([{ ...item, tracks: ["invalid"] }])), "corrupt");
    assert.equal(inspectStoredQueue(JSON.stringify([{ ...item, tracks: [{ name: "t", artists: "Artist", spotify_id: 1 }] }])), "corrupt");
});

test("persistence backoff doubles from the failed attempt and then caps", () => {
    assert.equal(persistenceBackoffMs(0), 250);
    assert.equal(persistenceBackoffMs(1), 250);
    assert.equal(persistenceBackoffMs(2), 500);
    assert.equal(persistenceBackoffMs(3), 1000);
    assert.equal(persistenceBackoffMs(4), 2000);
    assert.equal(persistenceBackoffMs(5), 4000);
    assert.equal(persistenceBackoffMs(8), 4000);
});

test("the same failed snapshot is retried with backoff and then reported unsaved", async () => {
    const snapshot = { id: "same" };
    const seen: unknown[] = [];
    const delays: number[] = [];
    const notices: string[] = [];
    const persisted = null;
    const controller = createQueuePersistenceController({
        isDirty: () => persisted !== snapshot,
        persist: async () => {
            seen.push(snapshot);
            throw new Error("disk");
        },
        sleep: async (ms: number) => {
            delays.push(ms);
            notices.push(queuePersistenceNotice(controller.getState()));
        },
    });
    controller.note();
    assert.equal(queuePersistenceNotice(controller.getState()), "quiet");
    await controller.whenSettled();
    assert.equal(seen.length, QUEUE_ATTEMPTS);
    assert.equal(seen.every((item) => item === snapshot), true);
    assert.equal(persisted, null);
    assert.deepEqual(delays, [250, 500, 1000]);
    assert.deepEqual(notices, ["retrying", "retrying", "retrying"]);
    assert.deepEqual(controller.getState(), {
        phase: "unsaved",
        failedAttempts: QUEUE_ATTEMPTS,
        retrying: false,
        sealed: false,
    });
    assert.equal(queuePersistenceNotice(controller.getState()), "unsaved");
    assert.equal(canRetryQueuePersistence(controller.getState()), true);
});

test("a later success stores that same snapshot and the first attempt stays quiet", async () => {
    const snapshot = [{ id: "a" }];
    let persisted: typeof snapshot | null = null;
    let needsFullReplace = false;
    const seen: Array<typeof snapshot> = [];
    const controller = createQueuePersistenceController({
        isDirty: () => needsFullReplace || persisted !== snapshot,
        persist: async () => {
            const attempted = snapshot;
            seen.push(attempted);
            if (seen.length < QUEUE_ATTEMPTS) throw new Error("disk");
            persisted = attempted;
            needsFullReplace = false;
        },
        onFailure: () => {
            needsFullReplace = true;
        },
        sleep: async () => {},
    });
    controller.note();
    assert.equal(controller.getState().phase, "saving");
    assert.equal(controller.getState().retrying, false);
    assert.equal(queuePersistenceNotice(controller.getState()), "quiet");
    await controller.whenSettled();
    assert.equal(seen.length, QUEUE_ATTEMPTS);
    assert.equal(seen.every((item) => item === snapshot), true);
    assert.equal(persisted, snapshot);
    assert.equal(needsFullReplace, false);
    assert.deepEqual(controller.getState(), {
        phase: "idle",
        failedAttempts: 0,
        retrying: false,
        sealed: false,
    });
});

test("retry and a new mutation each restart the attempt budget", async () => {
    let calls = 0;
    let fail = true;
    let persisted = false;
    const controller = createQueuePersistenceController({
        isDirty: () => !persisted,
        persist: async () => {
            calls += 1;
            if (fail) throw new Error("disk");
            persisted = true;
        },
        sleep: async () => {},
    });
    controller.retry();
    await controller.whenSettled();
    assert.equal(calls, 0);
    controller.note();
    await controller.whenSettled();
    assert.equal(calls, QUEUE_ATTEMPTS);
    assert.equal(controller.getState().phase, "unsaved");
    controller.retry();
    await controller.whenSettled();
    assert.equal(calls, QUEUE_ATTEMPTS * 2);
    assert.equal(controller.getState().failedAttempts, QUEUE_ATTEMPTS);
    fail = false;
    controller.note();
    await controller.whenSettled();
    assert.equal(calls, QUEUE_ATTEMPTS * 2 + 1);
    assert.equal(persisted, true);
    assert.equal(controller.getState().phase, "idle");
    assert.equal(controller.getState().failedAttempts, 0);
    assert.equal(canRetryQueuePersistence(controller.getState()), false);
});

test("a mutation during the retry pass gets a fresh budget after that pass", async () => {
    let calls = 0;
    let noted = false;
    const controller = createQueuePersistenceController({
        isDirty: () => true,
        persist: async () => {
            calls += 1;
            throw new Error("disk");
        },
        sleep: async () => {
            if (noted) return;
            noted = true;
            controller.note();
        },
    });
    controller.note();
    await controller.whenSettled();
    assert.equal(calls, QUEUE_ATTEMPTS * 2);
    assert.equal(controller.getState().phase, "unsaved");
    assert.equal(controller.getState().failedAttempts, QUEUE_ATTEMPTS);
    assert.equal(controller.getState().sealed, false);
});

test("cooldown pauses the current item and a stopped item is not auto-started", () => {
    const cooldown = singleTrackQueueOutcome("track-1", "paused");
    assert.equal(cooldown.paused, true);
    assert.deepEqual(cooldown.trackResults, {});
    const cooldownStatus = queueItemStatusAfterAttempt(cooldown, true);
    assert.equal(cooldownStatus, "paused");
    assert.equal(queueRunHaltsBeforeNextItem(cooldownStatus), true);
    assert.equal(isAutomaticallyRunnableQueueStatus(cooldownStatus), false);
    const stopped = singleTrackQueueOutcome("track-1", "cancelled");
    assert.equal(stopped.cancelled, true);
    const stoppedStatus = queueItemStatusAfterAttempt(stopped, true);
    assert.equal(stoppedStatus, "paused");
    assert.equal(isManuallyRunnableQueueStatus(stoppedStatus), true);
    assert.equal(queueRunHaltsBeforeNextItem("done"), false);
    assert.equal(queueRunHaltsBeforeNextItem("failed"), false);
    const items = [
        { id: "stopped-album", status: stoppedStatus, type: "album" },
        { id: "new-track", status: "pending", type: "track" },
    ];
    assert.equal(nextAutomaticQueueItem(items)?.id, "new-track");
    assert.equal(nextManualQueueItem(items)?.id, "stopped-album");
    assert.equal(queueItemStatusAfterAttempt({ paused: true, failedCount: 0, successCount: 1, skippedCount: 0 }, false), "done");
});

test("a rejected queue load leaves the store untouched", () => {
    const rejected = decidePersistentQueueLoad({ ok: false });
    assert.equal(rejected, "preserve-store");
    assert.equal(queueLoadWritesStore(rejected), false);
    const corrupt = decidePersistentQueueLoad({ ok: true, payload: "not-json" });
    assert.equal(corrupt, "preserve-corrupt");
    assert.equal(queueLoadWritesStore(corrupt), false);
    assert.equal(queueLoadWritesStore(decidePersistentQueueLoad({ ok: true, payload: "" })), true);
    assert.equal(decidePersistentQueueLoad({ ok: true, payload: "[]" }), "read-database");
    assert.equal(queueLoadWritesStore("read-database"), true);
});

test("existence checks send rounded duration seconds and omit empty lengths", () => {
    assert.equal(expectedTrackDurationSeconds(180000), 180);
    assert.equal(expectedTrackDurationSeconds(30000), 30);
    assert.notEqual(expectedTrackDurationSeconds(30000), expectedTrackDurationSeconds(180000));
    assert.equal(expectedTrackDurationSeconds(30500), 31);
    assert.equal(expectedTrackDurationSeconds(0), undefined);
    assert.equal(expectedTrackDurationSeconds(undefined), undefined);
});

test("a sealed store does not persist for note, retry, or an in-flight failure", async () => {
    let calls = 0;
    let rejectPersist: (error: Error) => void = () => {};
    const controller = createQueuePersistenceController({
        isDirty: () => true,
        persist: () => {
            calls += 1;
            return new Promise<void>((_resolve, reject) => {
                rejectPersist = reject;
            });
        },
    });
    controller.failClosed();
    controller.note();
    controller.retry();
    await controller.whenSettled();
    assert.equal(calls, 0);
    assert.equal(canRetryQueuePersistence(controller.getState()), false);
    assert.equal(queuePersistenceNotice(controller.getState()), "unsaved");
    const running = createQueuePersistenceController({
        isDirty: () => true,
        persist: () => {
            calls += 1;
            return new Promise<void>((_resolve, reject) => {
                rejectPersist = reject;
            });
        },
    });
    running.note();
    assert.equal(calls, 1);
    assert.equal(queuePersistenceNotice(running.getState()), "quiet");
    running.failClosed();
    running.note();
    rejectPersist(new Error("disk"));
    await running.whenSettled();
    assert.equal(calls, 1);
    assert.equal(running.getState().sealed, true);
    assert.equal(running.getState().phase, "unsaved");
    assert.equal(canRetryQueuePersistence(running.getState()), false);
});
