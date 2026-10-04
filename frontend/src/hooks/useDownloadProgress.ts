import { useSyncExternalStore } from "react";
import { GetDownloadProgress } from "../../wailsjs/go/main/App";
export interface DownloadProgressInfo {
    is_downloading: boolean;
    mb_downloaded: number;
    speed_mbps: number;
    rate_limited?: boolean;
    rate_limit_secs?: number;
    cooldown?: boolean;
    cooldown_secs?: number;
    cooldown_message?: string;
    cooldown_event_id?: number;
}
const emptyProgress: DownloadProgressInfo = {
    is_downloading: false,
    mb_downloaded: 0,
    speed_mbps: 0,
    rate_limited: false,
    rate_limit_secs: 0,
    cooldown: false,
    cooldown_secs: 0,
    cooldown_message: "",
};
let currentProgress = emptyProgress;
const listeners = new Set<() => void>();
let pollTimer: number | null = null;
let polling = false;
async function pollProgress() {
    if (polling) return;
    polling = true;
    try {
        const progressInfo = await GetDownloadProgress();
        currentProgress = progressInfo;
        for (const listener of listeners) {
            listener();
        }
    }
    catch (error) {
        console.error("Failed to get download progress:", error);
    }
    finally {
        polling = false;
    }
}
function startSharedPoll() {
    if (pollTimer !== null) {
        return;
    }
    void pollProgress();
    pollTimer = window.setInterval(() => {
        void pollProgress();
    }, 200);
}
function stopSharedPoll() {
    if (pollTimer === null) {
        return;
    }
    window.clearInterval(pollTimer);
    pollTimer = null;
}
function subscribe(listener: () => void) {
    listeners.add(listener);
    startSharedPoll();
    return () => {
        listeners.delete(listener);
        if (listeners.size === 0) stopSharedPoll();
    };
}
function getSnapshot() {
    return currentProgress;
}
export function useDownloadProgress() {
    return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
