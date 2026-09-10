import { useEffect, useMemo, useRef, useState } from "react";
import {
    MAX_RECENT_SEARCHES,
    RECENT_SEARCHES_KEY,
    SEARCH_LIMIT,
    classifySmartInput,
    loadRecentSearches,
    type ResultTab,
    type SmartSearchController,
} from "./smart-search-core";
import { SearchSpotify, SearchSpotifyByType } from "../../wailsjs/go/main/App";
import { backend } from "../../wailsjs/go/models";

interface UseSmartSearchOptions {
    url: string;
    onUrlChange: (value: string) => void;
    onFetch: () => void;
    onFetchUrl: (url: string) => Promise<void>;
}

/**
 * Smart omnibar state machine: link classification, debounced catalog
 * search, per-tab sorting/filtering and recent queries. Resets happen in
 * input events; the effect only schedules the network call.
 */
export function useSmartSearch({ url, onUrlChange, onFetch, onFetchUrl }: UseSmartSearchOptions): SmartSearchController {
    const [searchResults, setSearchResults] = useState<backend.SearchResponse | null>(null);
    const [resultFilter, setResultFilter] = useState("");
    const [sortOrder, setSortOrder] = useState("default");
    const [isLoadingMore, setIsLoadingMore] = useState(false);
    const [lastSearchedQuery, setLastSearchedQuery] = useState("");
    const [settledFor, setSettledFor] = useState("");
    const [activeTab, setActiveTab] = useState<ResultTab>("tracks");
    const [recentSearches, setRecentSearches] = useState<string[]>(loadRecentSearches);
    const [hasMore, setHasMore] = useState<Record<ResultTab, boolean>>({
        tracks: false,
        albums: false,
        artists: false,
        playlists: false,
    });
    const [showInvalidUrlDialog, setShowInvalidUrlDialog] = useState(false);
    const [showNextDialog, setShowNextDialog] = useState(false);
    const [invalidUrl, setInvalidUrl] = useState("");
    const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const searchRequestRef = useRef(0);
    const loadingMoreRef = useRef(false);
    const nextDialogPromptedRef = useRef(false);
    const inputKind = classifySmartInput(url);
    const isSearchInput = inputKind === "search";
    // Derived: a query is "searching" until its results have settled.
    const isSearching = isSearchInput && url.trim() !== "" && settledFor !== url.trim();

    const saveRecentSearch = (query: string) => {
        const trimmed = query.trim();
        if (!trimmed)
            return;
        setRecentSearches((prev) => {
            const filtered = prev.filter((s) => s.toLowerCase() !== trimmed.toLowerCase());
            const updated = [trimmed, ...filtered].slice(0, MAX_RECENT_SEARCHES);
            try {
                localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(updated));
            }
            catch (error) {
                console.error("Failed to save recent searches:", error);
            }
            return updated;
        });
    };
    const removeRecentSearch = (query: string) => {
        setRecentSearches((prev) => {
            const updated = prev.filter((s) => s !== query);
            try {
                localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(updated));
            }
            catch (error) {
                console.error("Failed to save recent searches:", error);
            }
            return updated;
        });
    };

    /**
     * Event-driven entry point for every keystroke into the omnibar.
     * Clears stale results synchronously so the catalog never shows
     * answers to a question that has changed.
     */
    const handleInputChange = (value: string) => {
        const nextKind = classifySmartInput(value);
        searchRequestRef.current += 1;
        if (searchTimeoutRef.current) {
            clearTimeout(searchTimeoutRef.current);
            searchTimeoutRef.current = null;
        }
        setSearchResults(null);
        setLastSearchedQuery("");
        setResultFilter("");
        setSortOrder("default");
        if (nextKind === "next-url") {
            if (!nextDialogPromptedRef.current) {
                setInvalidUrl(value.trim());
                setShowNextDialog(true);
                nextDialogPromptedRef.current = true;
            }
        }
        else {
            nextDialogPromptedRef.current = false;
        }
        onUrlChange(value);
    };

    useEffect(() => {
        if (!isSearchInput || !url.trim()) {
            return;
        }
        const requestId = ++searchRequestRef.current;
        if (searchTimeoutRef.current) {
            clearTimeout(searchTimeoutRef.current);
        }
        searchTimeoutRef.current = setTimeout(async () => {
            try {
                const results = await SearchSpotify({
                    query: url,
                    limit: SEARCH_LIMIT,
                });
                if (requestId !== searchRequestRef.current) {
                    return;
                }
                setSearchResults(results);
                setLastSearchedQuery(url.trim());
                saveRecentSearch(url.trim());
                setHasMore({
                    tracks: results.tracks.length === SEARCH_LIMIT,
                    albums: results.albums.length === SEARCH_LIMIT,
                    artists: results.artists.length === SEARCH_LIMIT,
                    playlists: results.playlists.length === SEARCH_LIMIT,
                });
                if (results.tracks.length > 0)
                    setActiveTab("tracks");
                else if (results.albums.length > 0)
                    setActiveTab("albums");
                else if (results.artists.length > 0)
                    setActiveTab("artists");
                else if (results.playlists.length > 0)
                    setActiveTab("playlists");
            }
            catch (error) {
                if (requestId !== searchRequestRef.current) {
                    return;
                }
                console.error("Search failed:", error);
                setSearchResults(null);
            }
            finally {
                if (requestId === searchRequestRef.current) {
                    setSettledFor(url.trim());
                }
            }
        }, 400);
        return () => {
            if (searchTimeoutRef.current) {
                clearTimeout(searchTimeoutRef.current);
                searchTimeoutRef.current = null;
            }
        };
    }, [url, isSearchInput]);

    const getTabCount = (tab: ResultTab): number => {
        if (!searchResults)
            return 0;
        switch (tab) {
            case "tracks":
                return searchResults.tracks.length;
            case "albums":
                return searchResults.albums.length;
            case "artists":
                return searchResults.artists.length;
            case "playlists":
                return searchResults.playlists.length;
        }
    };
    const loadMore = async () => {
        if (!searchResults || !lastSearchedQuery || isLoadingMore || loadingMoreRef.current)
            return;
        const requestId = searchRequestRef.current;
        const query = lastSearchedQuery;
        const tab = activeTab;
        const currentCount = getTabCount(tab);
        const typeMap: Record<ResultTab, string> = {
            tracks: "track",
            albums: "album",
            artists: "artist",
            playlists: "playlist",
        };
        loadingMoreRef.current = true;
        setIsLoadingMore(true);
        try {
            const moreResults = await SearchSpotifyByType({
                query: query,
                search_type: typeMap[tab],
                limit: SEARCH_LIMIT,
                offset: currentCount,
            });
            if (requestId !== searchRequestRef.current) {
                return;
            }
            if (moreResults.length > 0) {
                setSearchResults((prev) => {
                    if (!prev)
                        return prev;
                    return new backend.SearchResponse({
                        tracks: tab === "tracks"
                            ? [...prev.tracks, ...moreResults]
                            : prev.tracks,
                        albums: tab === "albums"
                            ? [...prev.albums, ...moreResults]
                            : prev.albums,
                        artists: tab === "artists"
                            ? [...prev.artists, ...moreResults]
                            : prev.artists,
                        playlists: tab === "playlists"
                            ? [...prev.playlists, ...moreResults]
                            : prev.playlists,
                    });
                });
            }
            setHasMore((prev) => ({
                ...prev,
                [tab]: moreResults.length === SEARCH_LIMIT,
            }));
        }
        catch (error) {
            console.error("Load more failed:", error);
        }
        finally {
            loadingMoreRef.current = false;
            setIsLoadingMore(false);
        }
    };
    const submit = () => {
        if (inputKind === "next-url") {
            setInvalidUrl(url.trim());
            setShowNextDialog(true);
            return;
        }
        if (inputKind === "invalid-url") {
            setInvalidUrl(url);
            setShowInvalidUrlDialog(true);
            return;
        }
        if (inputKind === "spotify") {
            onFetch();
        }
    };
    const fetchResult = (externalUrl: string) => {
        void onFetchUrl(externalUrl);
    };
    const resetNextDialogPrompt = () => {
        nextDialogPromptedRef.current = false;
    };
    const sortedResults = useMemo(() => buildSortedResults(searchResults, resultFilter, sortOrder, activeTab), [searchResults, resultFilter, sortOrder, activeTab]);
    const hasAnyResults = Boolean(searchResults &&
        (searchResults.tracks.length > 0 ||
            searchResults.albums.length > 0 ||
            searchResults.artists.length > 0 ||
            searchResults.playlists.length > 0));
    return {
        inputKind,
        isSearching,
        isLoadingMore,
        searchResults,
        hasAnyResults,
        activeTab,
        setActiveTab,
        resultFilter,
        setResultFilter,
        sortOrder,
        setSortOrder,
        sortedResults,
        getTabCount,
        tabHasMore: hasMore[activeTab],
        loadMore,
        recentSearches,
        removeRecentSearch,
        submit,
        fetchResult,
        handleInputChange,
        showInvalidUrlDialog,
        setShowInvalidUrlDialog,
        showNextDialog,
        setShowNextDialog,
        invalidUrl,
        resetNextDialogPrompt,
    };
}

function buildSortedResults(
    searchResults: backend.SearchResponse | null,
    resultFilter: string,
    sortOrder: string,
    activeTab: ResultTab,
): {
    tracks: backend.SearchResult[];
    albums: backend.SearchResult[];
    artists: backend.SearchResult[];
    playlists: backend.SearchResult[];
} {
    if (!searchResults)
        return { tracks: [], albums: [], artists: [], playlists: [] };
    const filterStr = resultFilter.toLowerCase();
    let tracks = [...searchResults.tracks];
    if (filterStr) {
        tracks = tracks.filter(track => (track.name || '').toLowerCase().includes(filterStr) || (track.artists || '').toLowerCase().includes(filterStr));
    }
    if (sortOrder !== 'default' && activeTab === "tracks") {
        tracks.sort((a, b) => compareBy(a.name, b.name, a.artists, b.artists, a.duration_ms, b.duration_ms, undefined, undefined, sortOrder));
    }
    let albums = [...searchResults.albums];
    if (filterStr) {
        albums = albums.filter(album => (album.name || '').toLowerCase().includes(filterStr) || (album.artists || '').toLowerCase().includes(filterStr));
    }
    if (sortOrder !== 'default' && activeTab === "albums") {
        albums.sort((a, b) => compareBy(a.name, b.name, a.artists, b.artists, undefined, undefined, a.release_date, b.release_date, sortOrder));
    }
    let artists = [...searchResults.artists];
    if (filterStr) {
        artists = artists.filter(artist => (artist.name || '').toLowerCase().includes(filterStr));
    }
    if (sortOrder !== 'default' && activeTab === "artists") {
        artists.sort((a, b) => compareBy(a.name, b.name, undefined, undefined, undefined, undefined, undefined, undefined, sortOrder));
    }
    let playlists = [...searchResults.playlists];
    if (filterStr) {
        playlists = playlists.filter(playlist => (playlist.name || '').toLowerCase().includes(filterStr) || (playlist.owner || '').toLowerCase().includes(filterStr));
    }
    if (sortOrder !== 'default' && activeTab === "playlists") {
        playlists.sort((a, b) => {
            if (sortOrder === 'title-asc')
                return (a.name || '').localeCompare(b.name || '');
            if (sortOrder === 'title-desc')
                return (b.name || '').localeCompare(a.name || '');
            if (sortOrder === 'owner-asc')
                return (a.owner || '').localeCompare(b.owner || '');
            if (sortOrder === 'owner-desc')
                return (b.owner || '').localeCompare(a.owner || '');
            return 0;
        });
    }
    return { tracks, albums, artists, playlists };
}
function compareBy(nameA: string, nameB: string, artistA?: string, artistB?: string, durationA?: number, durationB?: number, dateA?: string, dateB?: string, order = ""): number {
    switch (order) {
        case 'title-asc':
        case 'name-asc':
            return nameA.localeCompare(nameB);
        case 'title-desc':
        case 'name-desc':
            return nameB.localeCompare(nameA);
        case 'artist-asc':
            return (artistA || '').localeCompare(artistB || '');
        case 'artist-desc':
            return (artistB || '').localeCompare(artistA || '');
        case 'duration-desc':
            return (durationB || 0) - (durationA || 0);
        case 'duration-asc':
            return (durationA || 0) - (durationB || 0);
        case 'year-desc':
            return (dateB || '').localeCompare(dateA || '');
        case 'year-asc':
            return (dateA || '').localeCompare(dateB || '');
        default:
            return 0;
    }
}
