export function loadSettingsPage() {
    return import("@/components/SettingsPage").then((module) => ({ default: module.SettingsPage }));
}

export function loadDebugLoggerPage() {
    return import("@/components/DebugLoggerPage").then((module) => ({ default: module.DebugLoggerPage }));
}

export function loadHistoryPage() {
    return import("@/components/HistoryPage").then((module) => ({ default: module.HistoryPage }));
}

export function loadQueuePage() {
    return import("@/components/QueuePage").then((module) => ({ default: module.QueuePage }));
}

export function loadDevicesPage() {
    return import("@/components/DevicesPage").then((module) => ({ default: module.DevicesPage }));
}

export function loadLibraryHealthPage() {
    return import("@/components/LibraryHealthPage").then((module) => ({ default: module.LibraryHealthPage }));
}
