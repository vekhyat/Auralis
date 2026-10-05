import { useSyncExternalStore } from "react";
import { CancelTasteSync, SyncTasteNow } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export interface TasteSyncProgress {
    phase: string;
    message: string;
    current: number;
    total: number;
    done: boolean;
    error?: string;
}

let progress: TasteSyncProgress | null = null;
let listening = false;
const listeners = new Set<() => void>();
const snapshot = () => progress;
function publish(next: TasteSyncProgress | null) {
    progress = next;
    listeners.forEach((listener) => listener());
}
function subscribe(listener: () => void) {
    // Keep the runtime subscription for the app's lifetime: sync continues
    // when navigating between Connections and For You.
    if (!listening) {
        EventsOn("taste:sync-progress", (next: TasteSyncProgress) => publish(next));
        listening = true;
    }
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}

export async function startTasteSync() {
    if (progress && !progress.done) return;
    publish({ phase: "starting", message: "", current: 0, total: 0, done: false });
    try {
        await SyncTasteNow();
    } catch (error) {
        publish(null);
        throw error;
    }
}

export async function cancelTasteSync() {
    await CancelTasteSync();
    // The final backend event ends the busy state after cancellation settles.
}

export function useTasteSync() {
    const syncProgress = useSyncExternalStore(subscribe, snapshot);
    return { syncProgress, syncing: !!syncProgress && !syncProgress.done };
}
