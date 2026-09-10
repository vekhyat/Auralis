import type { backend } from "../../wailsjs/go/models";

export type ResultTab = "tracks" | "albums" | "artists" | "playlists";
export type SmartInputKind = "empty" | "spotify" | "search" | "next-url" | "invalid-url";
export type NextProvider = "Tidal" | "Deezer" | "Amazon Music" | "Qobuz";

export const RECENT_SEARCHES_KEY = "auralis_recent_searches";
export const MAX_RECENT_SEARCHES = 8;
export const SEARCH_LIMIT = 50;

export function getNextProvider(hostname: string): NextProvider | null {
    if (hostname === "tidal.com" || hostname.endsWith(".tidal.com"))
        return "Tidal";
    if (hostname === "deezer.com" || hostname.endsWith(".deezer.com"))
        return "Deezer";
    if (hostname.startsWith("music.amazon."))
        return "Amazon Music";
    if (hostname === "qobuz.com" || hostname.endsWith(".qobuz.com"))
        return "Qobuz";
    return null;
}
function parseSmartUrl(value: string): URL | null {
    try {
        return new URL(/^www\./i.test(value) ? `https://${value}` : value);
    }
    catch {
        return null;
    }
}
export function classifySmartInput(value: string): SmartInputKind {
    const trimmed = value.trim();
    if (!trimmed) {
        return "empty";
    }
    if (/^spotify:/i.test(trimmed)) {
        return "spotify";
    }
    const looksLikeUrl = /^(https?:\/\/|www\.)/i.test(trimmed);
    if (!looksLikeUrl) {
        return "search";
    }
    const parsedUrl = parseSmartUrl(trimmed);
    if (!parsedUrl) {
        return "invalid-url";
    }
    const hostname = parsedUrl.hostname.toLowerCase();
    if (hostname === "spotify.com" || hostname.endsWith(".spotify.com") || hostname === "spotify.link" || hostname.endsWith(".spotify.link"))
        return "spotify";
    return getNextProvider(hostname) ? "next-url" : "invalid-url";
}
export function loadRecentSearches(): string[] {
    try {
        const saved = localStorage.getItem(RECENT_SEARCHES_KEY);
        return saved ? JSON.parse(saved) : [];
    }
    catch (error) {
        console.error("Failed to load recent searches:", error);
        return [];
    }
}
export interface SmartSearchController {
    inputKind: SmartInputKind;
    isSearching: boolean;
    isLoadingMore: boolean;
    searchResults: backend.SearchResponse | null;
    hasAnyResults: boolean;
    activeTab: ResultTab;
    setActiveTab: (tab: ResultTab) => void;
    resultFilter: string;
    setResultFilter: (value: string) => void;
    sortOrder: string;
    setSortOrder: (value: string) => void;
    sortedResults: {
        tracks: backend.SearchResult[];
        albums: backend.SearchResult[];
        artists: backend.SearchResult[];
        playlists: backend.SearchResult[];
    };
    getTabCount: (tab: ResultTab) => number;
    tabHasMore: boolean;
    loadMore: () => Promise<void>;
    recentSearches: string[];
    removeRecentSearch: (query: string) => void;
    submit: () => void;
    fetchResult: (externalUrl: string) => void;
    /** Event-driven omnibar change: resets stale results, then updates the URL. */
    handleInputChange: (value: string) => void;
    showInvalidUrlDialog: boolean;
    setShowInvalidUrlDialog: (open: boolean) => void;
    showNextDialog: boolean;
    setShowNextDialog: (open: boolean) => void;
    invalidUrl: string;
    resetNextDialogPrompt: () => void;
}
