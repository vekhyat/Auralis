export type PageType =
    | "main"
    | "for-you"
    | "settings"
    | "debug"
    | "history"
    | "queue"
    | "tools"
    | "audio-analysis"
    | "tempo-key-analyzer"
    | "replaygain"
    | "audio-converter"
    | "audio-resampler"
    | "file-manager"
    | "lyrics-manager"
    | "enrich"
    | "devices"
    | "library-health";

/** Pages the shell can show. Tool pages stay in `PageType` for leftover components, but they are not routes. */
export type ShellPage = Extract<PageType, "main" | "for-you" | "settings" | "debug" | "history" | "queue" | "devices" | "library-health">;

/** Destinations shown as words in the titlebar. Debug stays in the overflow menu. */
export type DestinationPage = Extract<ShellPage, "main" | "for-you" | "queue" | "history" | "library-health" | "devices" | "settings">;

/** For You appears only when enabled. Devices is always shown: it is also where export folders (e.g. for Syncthing) are added. */
export const PRIMARY_DESTINATIONS = ["main", "for-you", "queue", "history", "library-health", "devices", "settings"] as const satisfies readonly DestinationPage[];

const SHELL_PAGES = new Set<PageType>(["main", "for-you", "settings", "debug", "history", "queue", "devices", "library-health"]);

export function isShellPage(page: PageType): page is ShellPage {
    return SHELL_PAGES.has(page);
}
