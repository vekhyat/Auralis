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
    | "devices";

/** Pages the shell can show. Tool pages stay in `PageType` for leftover components, but they are not routes. */
export type ShellPage = Extract<PageType, "main" | "for-you" | "settings" | "debug" | "history" | "queue" | "devices">;

/** Destinations shown as words in the titlebar. Debug stays in the overflow menu. */
export type DestinationPage = Extract<ShellPage, "main" | "for-you" | "queue" | "history" | "devices" | "settings">;

/** Devices only appears while an iPod is connected. For You appears when enabled. */
export const PRIMARY_DESTINATIONS = ["main", "for-you", "queue", "history", "devices", "settings"] as const satisfies readonly DestinationPage[];

const SHELL_PAGES = new Set<PageType>(["main", "for-you", "settings", "debug", "history", "queue", "devices"]);

export function isShellPage(page: PageType): page is ShellPage {
    return SHELL_PAGES.has(page);
}
