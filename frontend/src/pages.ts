export type PageType =
    | "main"
    | "settings"
    | "debug"
    | "tools"
    | "audio-analysis"
    | "tempo-key-analyzer"
    | "replaygain"
    | "audio-converter"
    | "audio-resampler"
    | "file-manager"
    | "lyrics-manager"
    | "enrich"
    | "history"
    | "queue"
    | "devices";

/** Destinations shown as words in the titlebar. */
export type DestinationPage = Extract<PageType, "main" | "queue" | "history" | "devices" | "tools" | "settings">;

export const TOOL_PAGES: PageType[] = [
    "tools",
    "audio-analysis",
    "tempo-key-analyzer",
    "replaygain",
    "audio-converter",
    "audio-resampler",
    "file-manager",
    "lyrics-manager",
    "enrich",
];

export function isToolPage(page: PageType): boolean {
    return TOOL_PAGES.includes(page);
}
