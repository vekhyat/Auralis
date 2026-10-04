import { Suspense, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import i18n, { translateMessage } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { X } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { getSettings, getSettingsWithDefaults, loadSettings, saveSettings, applyThemeMode } from "@/lib/settings";
import { openExternal } from "@/lib/utils";
import { fetchSpotifyMetadata } from "@/lib/api";
import { OpenFolder, CheckFFmpegInstalled, DownloadFFmpeg, GetRecentFetches, SaveRecentFetches, ListIPods, ListSyncTargets } from "../wailsjs/go/main/App";
import { EventsOn, EventsOff, Quit } from "../wailsjs/runtime/runtime";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { TitleBar } from "@/components/TitleBar";
import { SourceVerificationPane } from "@/components/SourceVerificationPane";
import { MarkdownLite } from "@/components/MarkdownLite";
import { extractMarkdownSection } from "@/lib/markdown";
import { CatalogPane } from "@/components/CatalogPane";
import { useSmartSearch } from "@/hooks/useSmartSearch";
import { SmartSearchDialogs } from "@/components/SmartSearchDialogs";
import { TrackInfo } from "@/components/TrackInfo";
import { AlbumInfo } from "@/components/AlbumInfo";
import { PlaylistInfo } from "@/components/PlaylistInfo";
import { ArtistInfo } from "@/components/ArtistInfo";
import { DownloadShelf } from "@/components/DownloadShelf";
import { CooldownBanner } from "@/components/CooldownBanner";
import { DebugLoggerPage, DevicesPage, HistoryPage, PageErrorBoundary, PageLoading, QueuePage, SettingsPage, } from "@/lazy-pages";
import { loadDebugLoggerPage, loadDevicesPage, loadHistoryPage, loadQueuePage, loadSettingsPage, } from "@/lib/page-loaders";
import { createLazyPage } from "@/lib/lazy-page";
import { planLegacyHistoryMigration, shouldDiscardLegacyHistory } from "@/lib/fetch-history-migration";
import type { HistoryItem } from "@/components/FetchHistory";
import type { ShellPage } from "@/pages";
import { useDownload } from "@/hooks/useDownload";
import { useQueue } from "@/hooks/useQueue";
import { addCollectionToQueue, addTracksToQueue, type AddResult } from "@/lib/queue";
import type { SpotifyMetadataResponse, TrackMetadata } from "@/types/api";
import { useMetadata } from "@/hooks/useMetadata";
import { useLyrics } from "@/hooks/useLyrics";
import { useCover } from "@/hooks/useCover";
import { useAvailability } from "@/hooks/useAvailability";
import { buildPlaylistFolderName } from "@/lib/playlist";
import { isNewerVersion } from "@/lib/version";
const HISTORY_KEY = "auralis_fetch_history";
const MAX_HISTORY = 5;
function extractSpotifyEntityFromURL(url: string): {
    type: string;
    id: string;
} | null {
    const trimmed = url.trim();
    if (!trimmed) {
        return null;
    }
    const spotifyUriMatch = trimmed.match(/^spotify:(track|album|playlist|artist):([A-Za-z0-9]+)$/i);
    if (spotifyUriMatch) {
        return {
            type: spotifyUriMatch[1].toLowerCase(),
            id: spotifyUriMatch[2],
        };
    }
    let parsed: URL;
    try {
        parsed = new URL(trimmed);
    }
    catch (err) {
        if (!(err instanceof TypeError)) {
            console.error("Failed to parse URL:", err);
        }
        return null;
    }
    const segments = parsed.pathname.split("/").filter(Boolean);
    const supportedTypes = new Set(["track", "album", "playlist", "artist"]);
    for (let i = 0; i < segments.length - 1; i++) {
        const segment = segments[i].toLowerCase();
        if (!supportedTypes.has(segment)) {
            continue;
        }
        const id = segments[i + 1];
        if (id) {
            return { type: segment, id };
        }
    }
    return null;
}
function normalizeHistoryURL(url: string): string {
    const trimmed = url.trim();
    if (!trimmed)
        return trimmed;
    const withoutQuery = trimmed.split("?")[0].replace(/\/+$/, "");
    const spotifyEntity = extractSpotifyEntityFromURL(withoutQuery);
    if (spotifyEntity) {
        return `https://open.spotify.com/${spotifyEntity.type}/${spotifyEntity.id}`;
    }
    return withoutQuery.replace(/(\/artist\/[A-Za-z0-9]+)\/discography\/all$/i, "$1");
}
function getHistoryIdentityKey(type: HistoryItem["type"], url: string): string {
    const normalizedUrl = normalizeHistoryURL(url);
    const spotifyEntity = extractSpotifyEntityFromURL(normalizedUrl);
    if (spotifyEntity) {
        return `${type}:${spotifyEntity.id}`;
    }
    return `${type}:${normalizedUrl}`;
}
function dedupeHistoryItems(items: HistoryItem[]): HistoryItem[] {
    const seen = new Set<string>();
    const deduped: HistoryItem[] = [];
    for (const item of items) {
        const normalizedUrl = normalizeHistoryURL(item.url);
        const key = getHistoryIdentityKey(item.type, normalizedUrl);
        if (seen.has(key))
            continue;
        seen.add(key);
        deduped.push({ ...item, url: normalizedUrl });
    }
    return deduped;
}
function sortHistoryItems(items: HistoryItem[]): HistoryItem[] {
    return [...items].sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
}
function normalizeHistoryItems(items: HistoryItem[]): HistoryItem[] {
    return dedupeHistoryItems(sortHistoryItems(items)).slice(0, MAX_HISTORY);
}
function historyItemsFromPayload(items: unknown[]): HistoryItem[] {
    const parsed: HistoryItem[] = [];
    for (const value of items) {
        if (!value || typeof value !== "object")
            continue;
        const record = value as Record<string, unknown>;
        const type = record.type;
        if (type !== "track" && type !== "album" && type !== "playlist" && type !== "artist")
            continue;
        if (typeof record.url !== "string" || typeof record.name !== "string")
            continue;
        const timestamp = typeof record.timestamp === "number" && Number.isFinite(record.timestamp) ? record.timestamp : 0;
        const item: HistoryItem = {
            id: typeof record.id === "string" && record.id ? record.id : crypto.randomUUID(),
            url: record.url,
            type,
            name: record.name,
            artist: typeof record.artist === "string" ? record.artist : "",
            image: typeof record.image === "string" ? record.image : "",
            timestamp,
        };
        if (typeof record.is_explicit === "boolean")
            item.is_explicit = record.is_explicit;
        parsed.push(item);
    }
    return parsed;
}
function describeFetchedHistory(metadata: SpotifyMetadataResponse | null, url: string): Omit<HistoryItem, "id" | "timestamp"> | null {
    if (!metadata || !url)
        return null;
    if ("track" in metadata) {
        const { track } = metadata;
        return {
            url,
            type: "track",
            name: track.name,
            artist: track.artists,
            image: track.images,
            is_explicit: track.is_explicit,
        };
    }
    if ("album_info" in metadata) {
        const { album_info } = metadata;
        return {
            url,
            type: "album",
            name: album_info.name,
            artist: `${album_info.total_tracks.toLocaleString()} tracks`,
            image: album_info.images,
            is_explicit: album_info.is_explicit,
        };
    }
    if ("playlist_info" in metadata) {
        const { playlist_info } = metadata;
        return {
            url,
            type: "playlist",
            name: playlist_info.name || playlist_info.owner.name,
            artist: `${playlist_info.tracks.total.toLocaleString()} tracks`,
            image: playlist_info.cover || playlist_info.owner.images || "",
        };
    }
    if ("artist_info" in metadata) {
        const { artist_info } = metadata;
        return {
            url,
            type: "artist",
            name: artist_info.name,
            artist: `${artist_info.total_albums.toLocaleString()} albums`,
            image: artist_info.images,
        };
    }
    return null;
}
function rememberFetchedHistory(prev: HistoryItem[], item: Omit<HistoryItem, "id" | "timestamp">): HistoryItem[] {
    const normalizedUrl = normalizeHistoryURL(item.url);
    const identityKey = getHistoryIdentityKey(item.type, normalizedUrl);
    const filtered = prev.filter((entry) => getHistoryIdentityKey(entry.type, entry.url) !== identityKey);
    const newItem: HistoryItem = {
        ...item,
        url: normalizedUrl,
        id: crypto.randomUUID(),
        timestamp: Date.now(),
    };
    return normalizeHistoryItems([newItem, ...filtered]);
}
function App() {
    const { t } = useTranslation();
    const [currentPage, setCurrentPage] = useState<ShellPage>("main");
    const [pageAttempt, setPageAttempt] = useState(0);
    const [ipodConnected, setIpodConnected] = useState(false);
    const [syncTargetsCount, setSyncTargetsCount] = useState(0);
    const [spotifyUrl, setSpotifyUrl] = useState("");
    const [smartSearchInput, setSmartSearchInput] = useState("");
    const [selectedTracks, setSelectedTracks] = useState<string[]>([]);
    const [searchQuery, setSearchQuery] = useState("");
    const [sortBy, setSortBy] = useState<string>("default");
    const [currentListPage, setCurrentListPage] = useState(1);
    const [updateInfo, setUpdateInfo] = useState<{
        version: string;
        changelog: string;
        url: string;
    } | null>(null);
    const [showUpdateDialog, setShowUpdateDialog] = useState(false);
    const [fetchHistory, setFetchHistory] = useState<HistoryItem[]>([]);
    const [hasUnsavedSettings, setHasUnsavedSettings] = useState(false);
    const [pendingPageChange, setPendingPageChange] = useState<ShellPage | null>(null);
    const [showUnsavedChangesDialog, setShowUnsavedChangesDialog] = useState(false);
    const [resetSettingsFn, setResetSettingsFn] = useState<(() => void) | null>(null);
    const ITEMS_PER_PAGE = 50;
    const CURRENT_VERSION = __APP_VERSION__;
    const download = useDownload();
    const queue = useQueue(download);
    const metadata = useMetadata();
    const lyrics = useLyrics();
    const cover = useCover();
    const availability = useAvailability();
    const navigationUrl = metadata.navigationUrl;
    const [seenNavigationUrl, setSeenNavigationUrl] = useState(navigationUrl);
    if (navigationUrl !== seenNavigationUrl) {
        setSeenNavigationUrl(navigationUrl);
        setSpotifyUrl(navigationUrl);
    }
    const catalogMetadata = metadata.metadata;
    const [seenCatalogMetadata, setSeenCatalogMetadata] = useState(catalogMetadata);
    if (catalogMetadata !== seenCatalogMetadata) {
        setSeenCatalogMetadata(catalogMetadata);
        setSelectedTracks([]);
        setSearchQuery("");
        setSortBy("default");
        setCurrentListPage(1);
    }
    const catalogResets = useRef({ download, lyrics, cover, availability });
    useEffect(() => {
        catalogResets.current = { download, lyrics, cover, availability };
    }, [download, lyrics, cover, availability]);
    useEffect(() => {
        const resets = catalogResets.current;
        resets.download.resetDownloadedTracks();
        resets.lyrics.resetLyricsState();
        resets.cover.resetCoverState();
        resets.availability.clearAvailability();
    }, [catalogMetadata]);
    useEffect(() => {
        let first = true;
        let sawEvent = false;
        const seen = new Set<string>();
        const remember = (list: Array<{ id?: string }>) => {
            for (const device of list) {
                if (device.id) seen.add(device.id);
            }
        };
        EventsOn("ipod:devices", (devices: Array<{ id?: string; name?: string }> | null) => {
            sawEvent = true;
            const list = Array.isArray(devices) ? devices : [];
            setIpodConnected(list.length > 0);
            if (first) {
                first = false;
                remember(list);
                return;
            }
            for (const device of list) {
                if (!device.id || seen.has(device.id)) continue;
                toast.success(i18n.t("translation.devices.connected", { name: device.name || "" }));
            }
            seen.clear();
            remember(list);
        });
        void ListIPods().then((devices) => {
            if (sawEvent) return;
            const list = Array.isArray(devices) ? devices : [];
            setIpodConnected(list.length > 0);
            remember(list);
            first = false;
        }).catch(() => {});
        return () => {
            EventsOff("ipod:devices");
        };
    }, []);
    useEffect(() => {
        let mounted = true;
        const fetchSyncTargets = () => {
            void ListSyncTargets().then((targets) => {
                if (mounted) {
                    setSyncTargetsCount((targets || []).length);
                }
            }).catch(() => {});
        };
        fetchSyncTargets();
        EventsOn("devices:changed", fetchSyncTargets);
        return () => {
            mounted = false;
            EventsOff("devices:changed");
        };
    }, []);
    const hasDevices = ipodConnected || syncTargetsCount > 0;
    if (!hasDevices && currentPage === "devices") {
        setCurrentPage("main");
    }
    const [isFFmpegInstalled, setIsFFmpegInstalled] = useState<boolean | null>(null);
    const [isInstallingFFmpeg, setIsInstallingFFmpeg] = useState(false);
    const [ffmpegInstallProgress, setFfmpegInstallProgress] = useState(0);
    const [ffmpegInstallStatus, setFfmpegInstallStatus] = useState("");
    useLayoutEffectInit();
    const checkForUpdates = useCallback(async (): Promise<{
        version: string;
        changelog: string;
        url: string;
    } | null> => {
        try {
            const response = await fetch("https://api.github.com/repos/vekhyat/Auralis/releases/latest");
            const data = await response.json() as {
                tag_name?: string;
                body?: string;
            };
            const rawTag = data.tag_name || "";
            const latestVersion = rawTag.replace(/^v/, "") || "";
            if (!latestVersion || !isNewerVersion(latestVersion, CURRENT_VERSION))
                return null;
            return {
                version: latestVersion,
                changelog: extractMarkdownSection(data.body || "", "Changelog"),
                url: `https://github.com/vekhyat/Auralis/releases/tag/${rawTag}`,
            };
        }
        catch (err) {
            console.error("Failed to check for updates:", err);
            return null;
        }
    }, [CURRENT_VERSION]);
    const persistRecentHistory = useCallback(async (history: HistoryItem[]): Promise<boolean> => {
        try {
            await SaveRecentFetches(JSON.stringify(history));
            return true;
        }
        catch (err) {
            console.error("Failed to save recent fetches:", err);
            return false;
        }
    }, []);
    const loadHistory = useCallback(async (): Promise<HistoryItem[] | null> => {
        let legacyRaw: string | null;
        try {
            legacyRaw = localStorage.getItem(HISTORY_KEY);
        }
        catch (err) {
            console.error("Failed to read legacy fetch history:", err);
            try {
                const persistedRaw = await GetRecentFetches();
                const plan = planLegacyHistoryMigration({
                    legacyRaw: null,
                    persistedRaw,
                    persistedReadOk: true,
                    normalize: (items) => normalizeHistoryItems(historyItemsFromPayload(items)),
                });
                return plan.items;
            }
            catch (readErr) {
                console.error("Failed to load history:", readErr);
            }
            return null;
        }
        let persistedRaw: string | null = null;
        let persistedReadOk = true;
        try {
            persistedRaw = await GetRecentFetches();
        }
        catch (err) {
            console.error("Failed to load history:", err);
            persistedReadOk = false;
        }
        let plan;
        try {
            plan = planLegacyHistoryMigration({
                legacyRaw,
                persistedRaw,
                persistedReadOk,
                normalize: (items) => normalizeHistoryItems(historyItemsFromPayload(items)),
            });
        }
        catch (err) {
            console.error("Failed to normalize fetch history:", err);
            return null;
        }
        if (plan.saveItems != null) {
            const saveConfirmed = await persistRecentHistory(plan.saveItems);
            if (shouldDiscardLegacyHistory({ ...plan, saveConfirmed })) {
                try {
                    localStorage.removeItem(HISTORY_KEY);
                }
                catch (err) {
                    console.error("Failed to remove legacy fetch history:", err);
                }
            }
        }
        return plan.items;
    }, [persistRecentHistory]);
    useEffect(() => {
        const initSettings = async () => {
            const settings = await loadSettings();
            await i18n.changeLanguage(settings.language);
            applyThemeMode(settings.themeMode);
            if (!settings.downloadPath) {
                const settingsWithDefaults = await getSettingsWithDefaults();
                await saveSettings(settingsWithDefaults);
            }
        };
        void initSettings();
        const checkFFmpeg = async () => {
            try {
                const installed = await CheckFFmpegInstalled();
                setIsFFmpegInstalled(installed);
            }
            catch (err) {
                console.error("Failed to check FFmpeg:", err);
                setIsFFmpegInstalled(false);
            }
        };
        void checkFFmpeg();
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            const currentSettings = getSettings();
            if (currentSettings.themeMode === "auto") {
                applyThemeMode("auto");
            }
        };
        mediaQuery.addEventListener("change", handleChange);
        let cancelled = false;
        void checkForUpdates().then((update) => {
            if (cancelled || !update)
                return;
            setUpdateInfo(update);
            if (getSettings().showUpdateNotifications) {
                setShowUpdateDialog(true);
            }
        });
        void loadHistory().then((items) => {
            if (cancelled || !items)
                return;
            setFetchHistory(items);
        });
        return () => {
            cancelled = true;
            mediaQuery.removeEventListener("change", handleChange);
        };
    }, [checkForUpdates, loadHistory]);
    const handleInstallFFmpeg = async () => {
        setIsInstallingFFmpeg(true);
        setFfmpegInstallProgress(0);
        setFfmpegInstallStatus("starting");
        EventsOn("ffmpeg:progress", (progress: number) => {
            setFfmpegInstallProgress(progress);
            if (progress >= 100) {
                setFfmpegInstallStatus("extracting");
            }
            else {
                setFfmpegInstallStatus("downloading");
            }
        });
        EventsOn("ffmpeg:status", (status: string) => {
            setFfmpegInstallStatus(status);
        });
        try {
            const response = await DownloadFFmpeg();
            if (response.success) {
                toast.success(t("translation.migrated.App.ffmpegInstalledSuccessfully"));
                setIsFFmpegInstalled(true);
            }
            else {
                toast.error(t("translation.migrated.App.failedToInstallFFmpeg", { value1: response.error }));
            }
        }
        catch (error) {
            console.error("Error installing FFmpeg:", error);
            toast.error(t("translation.migrated.App.errorDuringFFmpegInstallation", { value1: error }));
        }
        finally {
            EventsOff("ffmpeg:progress");
            EventsOff("ffmpeg:status");
            setIsInstallingFFmpeg(false);
            setFfmpegInstallProgress(0);
            setFfmpegInstallStatus("");
        }
    };
    const historyDraft = describeFetchedHistory(metadata.metadata, spotifyUrl);
    const historyDraftKey = historyDraft
        ? `${historyDraft.type}:${normalizeHistoryURL(historyDraft.url)}:${historyDraft.name}:${historyDraft.artist}`
        : null;
    const [appliedHistoryKey, setAppliedHistoryKey] = useState<string | null>(null);
    // Set when a background download prepends history. The view's draft key
    // stays untouched, so this is what persists that write.
    const downloadedHistoryPending = useRef(false);
    if (historyDraft && historyDraftKey && historyDraftKey !== appliedHistoryKey) {
        setAppliedHistoryKey(historyDraftKey);
        setFetchHistory((prev) => rememberFetchedHistory(prev, historyDraft));
    }
    useEffect(() => {
        if (downloadedHistoryPending.current) {
            downloadedHistoryPending.current = false;
            void persistRecentHistory(fetchHistory);
            return;
        }
        if (!historyDraftKey || historyDraftKey !== appliedHistoryKey)
            return;
        void persistRecentHistory(fetchHistory);
    }, [appliedHistoryKey, fetchHistory, historyDraftKey, persistRecentHistory]);
    const recordRecentFetch = useCallback((url: string, data: SpotifyMetadataResponse) => {
        const draft = describeFetchedHistory(data, url);
        if (!draft)
            return;
        downloadedHistoryPending.current = true;
        setFetchHistory((prev) => rememberFetchedHistory(prev, draft));
    }, []);
    const removeFromHistory = (id: string) => {
        setFetchHistory((prev) => {
            if (!prev.some((h) => h.id === id))
                return prev;
            const updated = prev.filter((h) => h.id !== id);
            void persistRecentHistory(updated);
            return updated;
        });
    };
    const handleHistorySelect = async (item: HistoryItem) => {
        const originUrl = metadata.metadata ? undefined : smartSearchInput;
        setSmartSearchInput("");
        setSpotifyUrl(item.url);
        const result = await metadata.handleFetchMetadata(item.url, originUrl);
        if (result) {
            setSpotifyUrl(result.url);
        }
    };
    const handleFetchMetadata = useCallback(async () => {
        const requestedUrl = smartSearchInput.trim();
        if (!requestedUrl)
            return;
        setSpotifyUrl(requestedUrl);
        setSmartSearchInput("");
        const result = await metadata.handleFetchMetadata(requestedUrl, metadata.metadata ? undefined : requestedUrl);
        if (result)
            setSpotifyUrl(result.url);
    }, [smartSearchInput, metadata]);
    // The omnibar owns link classification and catalog search. Enter on a link
    // opens it; downloads start only from an explicit Download action.
    const omnibar = useSmartSearch({
        url: smartSearchInput,
        onUrlChange: setSmartSearchInput,
        onFetch: () => void handleFetchMetadata(),
        onFetchUrl: async (url) => {
            const originUrl = metadata.metadata ? undefined : smartSearchInput;
            setSmartSearchInput("");
            setSpotifyUrl(url);
            const result = await metadata.handleFetchMetadata(url, originUrl);
            if (result)
                setSpotifyUrl(result.url);
        },
    });
    const isSearchMode = omnibar.inputKind === "search";
    const handleSearchChange = (value: string) => {
        setSearchQuery(value);
        setCurrentListPage(1);
    };
    const toggleTrackSelection = (id: string) => {
        setSelectedTracks((prev) => prev.includes(id) ? prev.filter((prevId) => prevId !== id) : [...prev, id]);
    };
    const toggleSelectAll = (tracks: TrackMetadata[]) => {
        const tracksWithId = tracks.filter((track) => track.spotify_id).map((track) => track.spotify_id || "");
        if (tracksWithId.length === 0)
            return;
        const allSelected = tracksWithId.every(id => selectedTracks.includes(id));
        if (allSelected) {
            setSelectedTracks(prev => prev.filter(id => !tracksWithId.includes(id)));
        }
        else {
            setSelectedTracks(prev => Array.from(new Set([...prev, ...tracksWithId])));
        }
    };
    const selectTrackRange = (ids: string[], select: boolean) => {
        const validIds = ids.filter(Boolean);
        if (validIds.length === 0)
            return;
        if (select) {
            setSelectedTracks((prev) => Array.from(new Set([...prev, ...validIds])));
        }
        else {
            const removeSet = new Set(validIds);
            setSelectedTracks((prev) => prev.filter((id) => !removeSet.has(id)));
        }
    };
    const reportQueueAdd = useCallback((result: AddResult, label: string) => {
        if (result.added === 0) {
            toast.info(t("translation.downloads.requested"));
            return;
        }
        toast.success(t(queue.isSuspended ? "translation.downloads.addedPaused" : "translation.downloads.added", { name: label }), { action: { label: t("translation.downloads.title"), onClick: () => setCurrentPage("queue") } });
    }, [t, queue.isSuspended]);
    const handleQueueTracks = useCallback((tracks: TrackMetadata[], folderName?: string, startPosition?: number) => {
        const queueable = tracks.filter((track) => track.spotify_id);
        if (queueable.length === 0) {
            toast.error(t("translation.download.noTracksAvailableDownload"));
            return;
        }
        const result = addTracksToQueue(queueable, { folderName, startPosition });
        if (result.added === 1 && queueable.length === 1) {
            reportQueueAdd(result, queueable[0].name);
        }
        else if (result.added === 0) {
            toast.info(t("translation.downloads.requested"));
        }
        else {
            reportQueueAdd(result, t("translation.downloads.trackCount", { count: result.added }));
        }
    }, [reportQueueAdd, t]);
    const handleQueueSelectedTracks = useCallback((tracks: TrackMetadata[], folderName?: string) => {
        const selected = tracks.filter((track) => track.spotify_id && selectedTracks.includes(track.spotify_id));
        if (selected.length === 0) {
            toast.error(t("translation.download.noTracksSelected"));
            return;
        }
        handleQueueTracks(selected, folderName);
    }, [handleQueueTracks, selectedTracks, t]);
    const handleQueueCollection = useCallback((input: Parameters<typeof addCollectionToQueue>[0]) => {
        reportQueueAdd(addCollectionToQueue(input), input.name);
    }, [reportQueueAdd]);
    const queueRelease = (data: SpotifyMetadataResponse): boolean => {
        if ("track" in data) {
            handleQueueTracks([data.track]);
            return true;
        }
        if (!("album_info" in data) && !("playlist_info" in data))
            return false;
        const { track_list } = data;
        if (track_list.length === 0) {
            toast.error(t("translation.download.noTracksAvailableDownload"));
            return false;
        }
        const info = t("translation.downloads.trackCount", { count: track_list.length });
        if ("album_info" in data) {
            const { album_info } = data;
            handleQueueCollection({ type: "album", name: album_info.name, artist: album_info.artists, info, image: album_info.images, folderName: album_info.name, isAlbum: true, tracks: track_list });
            return true;
        }
        const { playlist_info } = data;
        const folderName = buildPlaylistFolderName(playlist_info.owner.name, playlist_info.owner.display_name, getSettings().playlistOwnerFolderName);
        handleQueueCollection({ type: "playlist", name: playlist_info.name, artist: playlist_info.owner.display_name || playlist_info.owner.name, info, image: playlist_info.cover || playlist_info.owner.images || "", folderName, tracks: track_list });
        return true;
    };
    // Downloading a search result reads its tracks in the background. The page,
    // the search, and any link being opened are left alone, and a newer fetch
    // in the main view cannot cancel the request.
    const [requestingDownloads, setRequestingDownloads] = useState<ReadonlySet<string>>(() => new Set());
    const requestingDownloadsRef = useRef(new Set<string>());
    const downloadFromUrl = async (url: string) => {
        if (requestingDownloadsRef.current.has(url))
            return;
        requestingDownloadsRef.current.add(url);
        setRequestingDownloads(new Set(requestingDownloadsRef.current));
        try {
            const data = await fetchSpotifyMetadata(url, true, 1.0, 300, undefined, crypto.randomUUID());
            // History only. Opening the release would also move the page and
            // the request clock; a search download must not.
            if (queueRelease(data)) {
                recordRecentFetch(url, data);
                void metadata.saveToHistory(url, data);
            }
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            toast.error(t("translation.downloads.couldntGet"), { description: translateMessage(message) });
        }
        finally {
            requestingDownloadsRef.current.delete(url);
            setRequestingDownloads(new Set(requestingDownloadsRef.current));
        }
    };
    const openFetchedUrl = async (url: string) => {
        setSpotifyUrl(url);
        const result = await metadata.handleFetchMetadata(url);
        if (result)
            setSpotifyUrl(result.url);
    };
    const handleOpenFolder = async () => {
        const settings = getSettings();
        if (!settings.downloadPath) {
            toast.error(t("translation.app.downloadPathNotSet"));
            return;
        }
        try {
            await OpenFolder(settings.downloadPath);
        }
        catch (error) {
            console.error("Error opening folder:", error);
            toast.error(t("translation.migrated.App.errorOpeningFolder", { value1: error }));
        }
    };
    const handleMetadataBack = () => {
        const url = metadata.goBack();
        if (url !== null) {
            setSpotifyUrl(url);
            setSmartSearchInput("");
        }
    };
    const handleMetadataForward = () => {
        const url = metadata.goForward();
        if (url !== null) {
            setSpotifyUrl(url);
            setSmartSearchInput("");
        }
    };
    const handleTitleBarBack = () => {
        if (currentPage === "main") {
            handleMetadataBack();
        }
    };
    const handleTitleBarForward = () => {
        if (currentPage === "main") {
            handleMetadataForward();
        }
    };
    const renderMetadata = () => {
        if (!metadata.metadata)
            return null;
        if ("track" in metadata.metadata) {
            const { track } = metadata.metadata;
            const trackId = track.spotify_id || "";
            return (<TrackInfo track={track} isDownloading={download.isDownloading} downloadingTrack={download.downloadingTrack} isDownloaded={download.downloadedTracks.has(trackId)} isFailed={download.failedTracks.has(trackId)} isSkipped={download.skippedTracks.has(trackId)} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} downloadedLyrics={lyrics.downloadedLyrics.has(track.spotify_id || "")} failedLyrics={lyrics.failedLyrics.has(track.spotify_id || "")} skippedLyrics={lyrics.skippedLyrics.has(track.spotify_id || "")} checkingAvailability={availability.checkingTrackId === track.spotify_id} availability={availability.availabilityMap.get(track.spotify_id || "")} downloadingCover={cover.downloadingCoverTrack === (track.spotify_id || `${track.name}-${track.artists}`)} downloadedCover={cover.downloadedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} failedCover={cover.failedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} skippedCover={cover.skippedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} onQueueTrack={(queuedTrack) => handleQueueTracks([queuedTrack])} onDownloadLyrics={(spotifyId, name, artists, albumName, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, undefined, undefined, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _playlistName, _position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, undefined, undefined, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onOpenFolder={handleOpenFolder} onAlbumClick={metadata.handleAlbumClick} onArtistClick={async (artist) => {
                    const artistUrl = await metadata.handleArtistClick(artist);
                    if (artistUrl) {
                        setSpotifyUrl(artistUrl);
                    }
                }} onPublisherClick={(publisher) => {
                    metadata.clearMetadata(smartSearchInput);
                    setSmartSearchInput(`label:"${publisher.replace(/"/g, '\\"')}"`);
                }} onBack={metadata.resetMetadata}/>);
        }
        if ("album_info" in metadata.metadata) {
            const { album_info, track_list } = metadata.metadata;
            return (<AlbumInfo albumInfo={album_info} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, album_info.name, position, albumArtist, releaseDate, discNumber, true)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, album_info.name, position, trackId, albumArtist, releaseDate, discNumber, true)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, album_info.name, undefined, true)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, album_info.name, true)} onQueueAll={() => handleQueueCollection({ type: "album", name: album_info.name, artist: album_info.artists, info: t("translation.downloads.trackCount", { count: track_list.length }), image: album_info.images, folderName: album_info.name, isAlbum: true, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, album_info.name)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], album_info.name, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onBack={metadata.resetMetadata} onArtistClick={async (artist) => {
                    const pendingArtistUrl = artist.external_urls.replace(/\/$/, "") + "/discography/all";
                    setSpotifyUrl(pendingArtistUrl);
                    const artistUrl = await metadata.handleArtistClick(artist);
                    if (artistUrl) {
                        setSpotifyUrl(artistUrl);
                    }
                }} onTrackClick={async (track) => {
                    if (track.external_urls) {
                        setSpotifyUrl(track.external_urls);
                        await metadata.handleFetchMetadata(track.external_urls);
                    }
                }}/>);
        }
        if ("playlist_info" in metadata.metadata) {
            const { playlist_info, track_list } = metadata.metadata;
            const settings = getSettings();
            const playlistFolderName = buildPlaylistFolderName(playlist_info.owner.name, playlist_info.owner.display_name, settings.playlistOwnerFolderName);
            return (<PlaylistInfo playlistInfo={playlist_info} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, playlistFolderName, position, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, playlistFolderName, position, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, playlistFolderName)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, playlistFolderName)} onQueueAll={() => handleQueueCollection({ type: "playlist", name: playlist_info.owner.name, artist: playlist_info.owner.display_name, info: t("translation.downloads.trackCount", { count: track_list.length }), image: playlist_info.cover || playlist_info.owner.images || "", folderName: playlistFolderName, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, playlistFolderName)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], playlistFolderName, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onBack={metadata.resetMetadata} onAlbumClick={metadata.handleAlbumClick} onArtistClick={async (artist) => {
                    const pendingArtistUrl = artist.external_urls.replace(/\/$/, "") + "/discography/all";
                    setSpotifyUrl(pendingArtistUrl);
                    const artistUrl = await metadata.handleArtistClick(artist);
                    if (artistUrl) {
                        setSpotifyUrl(artistUrl);
                    }
                }} onTrackClick={async (track) => {
                    if (track.external_urls) {
                        setSpotifyUrl(track.external_urls);
                        await metadata.handleFetchMetadata(track.external_urls);
                    }
                }}/>);
        }
        if ("artist_info" in metadata.metadata) {
            const { artist_info, album_list, track_list } = metadata.metadata;
            return (<ArtistInfo artistInfo={artist_info} albumList={album_list} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, artist_info.name, position, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, artist_info.name, position, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, artist_info.name)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, artist_info.name)} onQueueAll={() => handleQueueCollection({ type: "artist", name: artist_info.name, artist: artist_info.name, info: t("translation.downloads.trackCount", { count: track_list.length }), image: artist_info.images, folderName: artist_info.name, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, artist_info.name)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], artist_info.name, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onAlbumClick={metadata.handleAlbumClick} onBack={metadata.resetMetadata} onArtistClick={async (artist) => {
                    const pendingArtistUrl = artist.external_urls.replace(/\/$/, "") + "/discography/all";
                    setSpotifyUrl(pendingArtistUrl);
                    const artistUrl = await metadata.handleArtistClick(artist);
                    if (artistUrl) {
                        setSpotifyUrl(artistUrl);
                    }
                }} onTrackClick={async (track) => {
                    if (track.external_urls) {
                        setSpotifyUrl(track.external_urls);
                        await metadata.handleFetchMetadata(track.external_urls);
                    }
                }}/>);
        }
        return null;
    };
    const commitPageNavigation = (page: ShellPage) => {
        if (page === currentPage) {
            return;
        }
        setCurrentPage(page);
    };
    const handlePageChange = (page: ShellPage) => {
        if (currentPage === "settings" && hasUnsavedSettings && page !== "settings") {
            setPendingPageChange(page);
            setShowUnsavedChangesDialog(true);
            return;
        }
        commitPageNavigation(page);
    };
    const handleDiscardChanges = async () => {
        setShowUnsavedChangesDialog(false);
        if (resetSettingsFn) {
            resetSettingsFn();
        }
        const savedSettings = getSettings();
        await i18n.changeLanguage(savedSettings.language);
        applyThemeMode(savedSettings.themeMode);
        if (pendingPageChange) {
            commitPageNavigation(pendingPageChange);
            setPendingPageChange(null);
        }
    };
    const handleCancelNavigation = () => {
        setShowUnsavedChangesDialog(false);
        setPendingPageChange(null);
    };
    const [secondaryPages, setSecondaryPages] = useState(() => ({
        settings: SettingsPage,
        debug: DebugLoggerPage,
        history: HistoryPage,
        queue: QueuePage,
        devices: DevicesPage,
    }));
    const retryCurrentPage = () => {
        setPageAttempt((attempt) => attempt + 1);
        setSecondaryPages((current) => {
            switch (currentPage) {
                case "settings":
                    return { ...current, settings: createLazyPage(loadSettingsPage) };
                case "debug":
                    return { ...current, debug: createLazyPage(loadDebugLoggerPage) };
                case "history":
                    return { ...current, history: createLazyPage(loadHistoryPage) };
                case "queue":
                    return { ...current, queue: createLazyPage(loadQueuePage) };
                case "devices":
                    return { ...current, devices: createLazyPage(loadDevicesPage) };
                default:
                    return current;
            }
        });
    };
    const renderSecondary = (page: ReactNode) => (<PageErrorBoundary key={`${currentPage}:${pageAttempt}`} resetKey={`${currentPage}:${pageAttempt}`} onRetry={retryCurrentPage}>
      <Suspense fallback={<PageLoading />}>{page}</Suspense>
    </PageErrorBoundary>);
    const renderPage = () => {
        switch (currentPage) {
            case "settings":
                return renderSecondary(<secondaryPages.settings onUnsavedChangesChange={setHasUnsavedSettings} onResetRequest={setResetSettingsFn}/>);
            case "debug":
                return renderSecondary(<secondaryPages.debug />);
            case "devices":
                return renderSecondary(<secondaryPages.devices />);
            case "history":
                return renderSecondary(<secondaryPages.history onHistorySelect={(item) => {
                        setSmartSearchInput("");
                        setSpotifyUrl(item.url);
                        metadata.loadFromCache(item.data, item.url);
                        setCurrentPage("main");
                    }}/>);
            case "queue":
                return renderSecondary(<secondaryPages.queue items={queue.items} isSuspended={queue.isSuspended} onOpenLibrary={() => handlePageChange("main")} onOpenFolder={handleOpenFolder} isProcessing={queue.isProcessing} isPausing={queue.isPausing} processingType={queue.processingType} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} downloadingTracks={download.downloadingTrack ? new Set([download.downloadingTrack]) : new Set()} onStart={queue.start} onPause={queue.pause} onStop={queue.stop} isDirectDownloading={download.isDownloading || download.downloadingTrack !== null} onStopDirect={download.handleStopDownload}/>);
            default:
                return (<>
                    <CatalogPane
                      controller={omnibar}
                      query={smartSearchInput}
                      history={fetchHistory}
                      onHistorySelect={handleHistorySelect}
                      onHistoryRemove={removeFromHistory}
                      onRecentSearchSelect={omnibar.handleInputChange}
                      onDownloadResult={(url) => void downloadFromUrl(url)}
                      requestingDownloads={requestingDownloads}
                      hasMetadata={!!metadata.metadata}
                    />

                    <Dialog open={metadata.showAlbumDialog} onOpenChange={metadata.setShowAlbumDialog}>
                        <DialogContent className="p-6 sm:max-w-106.25 [&>button]:hidden">
                            <div className="absolute top-4 right-4">
                                <Button variant="ghost" size="icon-sm" className="opacity-70 hover:opacity-100" onClick={() => metadata.setShowAlbumDialog(false)}>
                                    <X className="size-4"/>
                                </Button>
                            </div>
                            <DialogTitle className="text-sm font-medium">{t("translation.common.fetchAlbum")}</DialogTitle>
                            <DialogDescription>
                                {t("translation.album.fetchMetadataConfirm")}
                            </DialogDescription>
                            {metadata.selectedAlbum && (<div className="py-2">
                                <p className="border bg-muted/50 px-3 py-2 font-medium">{metadata.selectedAlbum.name}</p>
                            </div>)}
                            <DialogFooter>
                                <Button variant="outline" onClick={() => metadata.setShowAlbumDialog(false)}>
                                    {t("translation.common.cancel")}
                                </Button>
                                <Button onClick={async () => {
                        const pendingAlbumUrl = metadata.selectedAlbum?.external_urls;
                        if (pendingAlbumUrl) {
                            setSpotifyUrl(pendingAlbumUrl);
                        }
                        const albumUrl = await metadata.handleConfirmAlbumFetch(spotifyUrl);
                        if (albumUrl) {
                            setSpotifyUrl(albumUrl);
                        }
                    }}>
                                    {t("translation.common.fetchAlbum")}
                                </Button>
                            </DialogFooter>
                        </DialogContent>
                    </Dialog>

                    {!isSearchMode && metadata.metadata && renderMetadata()}
                </>);
        }
    };
    return (<TooltipProvider>
        <div className="h-full overflow-hidden bg-background text-foreground">
            <TitleBar
              canGoBack={currentPage === "main" && metadata.canGoBack}
              canGoForward={currentPage === "main" && metadata.canGoForward}
              navigationDisabled={currentPage === "main" && metadata.loading}
              onBack={handleTitleBarBack}
              onForward={handleTitleBarForward}
              currentPage={currentPage}
              onPageChange={handlePageChange}
              showDevices={hasDevices}
              queueCount={queue.items.filter((item) => item.status === "pending" || item.status === "running").length}
              omnibar={{
                  value: smartSearchInput,
                  loading: metadata.loading || omnibar.isSearching,
                  onChange: (value) => {
                      omnibar.handleInputChange(value);
                      if (value.trim() && currentPage !== "main")
                          handlePageChange("main");
                  },
                  onSubmit: () => {
                      if (currentPage !== "main")
                          handlePageChange("main");
                      omnibar.submit();
                  },
              }}
            />

            <main
              data-page={currentPage}
              className="app-content fixed right-0 top-16 bottom-[76px] left-[184px] overflow-y-auto overflow-x-hidden"
            >
                <div className="mx-auto max-w-[1600px] px-8 py-8">
                    {renderPage()}
                </div>
            </main>

            <DownloadShelf items={queue.items} isProcessing={queue.isProcessing} isPausing={queue.isPausing} isSuspended={queue.isSuspended}
              onOpen={() => handlePageChange("queue")} onPause={() => queue.pause()} onResume={() => void queue.start()}
              onStop={() => queue.stop()} onFolder={handleOpenFolder} />

            <CooldownBanner />
            <SourceVerificationPane />

            <SmartSearchDialogs controller={omnibar}/>

            <Dialog open={showUpdateDialog} onOpenChange={setShowUpdateDialog}>
              <DialogContent className="sm:max-w-125 [&>button]:hidden">
                <DialogHeader>
                  <DialogTitle>{t("translation.app.updateAvailable")}</DialogTitle>
                  <DialogDescription>
                    {t("translation.app.newVersion")} {updateInfo ? t("translation.migrated.App.v", { value1: updateInfo.version }) : ""} {t("translation.app.availableReV")}{CURRENT_VERSION}
                  </DialogDescription>
                </DialogHeader>
                {updateInfo?.changelog ? (<div className="custom-scrollbar max-h-72 overflow-y-auto border bg-muted/40 p-3">
                    <MarkdownLite content={updateInfo.changelog}/>
                  </div>) : (<p className="text-sm text-muted-foreground">{t("translation.app.noChangelogProvidedRelease")}</p>)}
            <DialogFooter className="gap-2">
                    <Button variant="outline" onClick={() => setShowUpdateDialog(false)}>
                      {t("translation.app.downloadLater")}
                    </Button>
                    <Button onClick={() => {
            if (updateInfo) {
                openExternal(updateInfo.url);
            }
            setShowUpdateDialog(false);
        }}>
                      {t("translation.app.downloadNow")}
                    </Button>
            </DialogFooter>
              </DialogContent>
            </Dialog>

            <Dialog open={showUnsavedChangesDialog} onOpenChange={setShowUnsavedChangesDialog}>
                <DialogContent className="sm:max-w-106.25 [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>{t("translation.app.unsavedChanges")}</DialogTitle>
                        <DialogDescription>
                            {t("translation.app.unsavedChangesDescription")}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={handleCancelNavigation}>
                            {t("translation.sidebar.cancel")}
                        </Button>
                        <Button variant="destructive" onClick={handleDiscardChanges}>
                            {t("translation.app.discardChanges")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={metadata.showVpnAdviceDialog} onOpenChange={metadata.setShowVpnAdviceDialog}>
                <DialogContent className="max-w-md [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>{t("translation.downloads.couldntOpen")}</DialogTitle>
                        <DialogDescription>
                            {t("translation.downloads.couldntOpenHint")}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => metadata.setShowVpnAdviceDialog(false)}>
                            {t("translation.common.close")}
                        </Button>
                        <Button onClick={() => {
                            metadata.setShowVpnAdviceDialog(false);
                            if (spotifyUrl)
                                void openFetchedUrl(spotifyUrl);
                        }}>
                            {t("translation.downloads.tryAgain")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={isFFmpegInstalled === false} onOpenChange={() => { }}>
                <DialogContent className="max-w-112.5 gap-5 p-6 [&>button]:hidden">
                    <DialogHeader className="space-y-2">
                        <DialogTitle className="text-lg font-semibold tracking-tight">
                            {t("translation.downloads.settingUp")}
                        </DialogTitle>
                        <DialogDescription className="text-sm leading-relaxed font-normal text-foreground/70">
                            {t("translation.downloads.ffmpegNeed")}
                        </DialogDescription>
                    </DialogHeader>

                    {isInstallingFFmpeg && (<div className="space-y-4">
                            {ffmpegInstallStatus === "extracting" ? (<div className="flex flex-col items-center justify-center py-2 animate-in fade-in duration-500">
                                    <div className="flex items-center gap-3">
                                        <div className="size-4 animate-spin rounded-full border-2 border-primary border-t-transparent"/>
                                        <span className="text-sm font-bold tracking-tight">{t("translation.app.extracting")}</span>
                                    </div>
                                    <span className="mt-2 font-mono text-[10px] tracking-[0.2em] uppercase text-muted-foreground">{t("translation.app.finalizingSetup")}</span>
                                </div>) : (<div className="space-y-3">
                                    <div className="flex justify-between text-[11px] font-bold">
                                        <span className="tracking-wider uppercase text-muted-foreground">{t("translation.app.downloading")}</span>
                                        <span className="font-mono text-xl tracking-tighter tabular-nums text-primary">{ffmpegInstallProgress}%</span>
                                    </div>
                                    <div className="h-1 w-full overflow-hidden bg-secondary">
                                        <div className="h-full bg-primary transition-all duration-300" style={{ width: `${ffmpegInstallProgress}%` }}/>
                                    </div>
                                </div>)}
                        </div>)}

                    <DialogFooter className="flex-row gap-3 pt-2">
                        {!isInstallingFFmpeg && (<Button variant="outline" className="h-11 flex-1 text-sm" onClick={() => Quit()}>
                                {t("translation.app.exit")}
                            </Button>)}
                        <Button className={`h-11 text-sm ${isInstallingFFmpeg ? 'w-full' : 'flex-1'}`} onClick={handleInstallFFmpeg} disabled={isInstallingFFmpeg}>
                                {isInstallingFFmpeg ? t("translation.migrated.App.installing") : t("translation.migrated.App.installNow")}
                            </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    </TooltipProvider>);
}

function useLayoutEffectInit() {
    // First paint: honor stored theme mode only. Legacy theme/base/font values
    // in ~/.auralis are ignored by design; the locked skins live in CSS.
    useEffect(() => {
        const savedSettings = getSettings();
        if (savedSettings) {
            applyThemeMode(savedSettings.themeMode);
        }
    }, []);
}

export default App;
