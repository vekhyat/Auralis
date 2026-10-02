import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { X } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { getSettings, getSettingsWithDefaults, loadSettings, saveSettings, applyThemeMode } from "@/lib/settings";
import { openExternal } from "@/lib/utils";
import { OpenFolder, CheckFFmpegInstalled, DownloadFFmpeg, GetRecentFetches, SaveRecentFetches } from "../wailsjs/go/main/App";
import { EventsOn, EventsOff, Quit } from "../wailsjs/runtime/runtime";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { TitleBar } from "@/components/TitleBar";
import { MarkdownLite, extractMarkdownSection } from "@/components/MarkdownLite";
import { CatalogPane } from "@/components/CatalogPane";
import { useSmartSearch } from "@/hooks/useSmartSearch";
import { SmartSearchDialogs } from "@/components/SmartSearchDialogs";
import { TrackInfo } from "@/components/TrackInfo";
import { AlbumInfo } from "@/components/AlbumInfo";
import { PlaylistInfo } from "@/components/PlaylistInfo";
import { ArtistInfo } from "@/components/ArtistInfo";
import { DownloadProgressToast } from "@/components/DownloadProgressToast";
import { CooldownBanner } from "@/components/CooldownBanner";
import { AudioAnalysisPage } from "@/components/AudioAnalysisPage";
import { TempoKeyAnalyzerPage } from "@/components/TempoKeyAnalyzerPage";
import { ReplayGainPage } from "@/components/ReplayGainPage";
import { AudioConverterPage } from "@/components/AudioConverterPage";
import { AudioResamplerPage } from "@/components/AudioResamplerPage";
import { FileManagerPage } from "@/components/FileManagerPage";
import { LyricsManagerPage } from "@/components/LyricsManagerPage";
import { EnrichPage } from "@/components/EnrichPage";
import { ToolsPage, type ToolGroup } from "@/components/ToolsPage";
import { SettingsPage } from "@/components/SettingsPage";
import { DebugLoggerPage } from "@/components/DebugLoggerPage";
import { HistoryPage } from "@/components/HistoryPage";
import { DevicesPage } from "@/components/DevicesPage";
import { QueuePage } from "@/components/QueuePage";
import type { HistoryItem } from "@/components/FetchHistory";
import type { PageType } from "@/pages";
import { useDownload } from "@/hooks/useDownload";
import { useQueue } from "@/hooks/useQueue";
import { addCollectionToQueue, addTracksToQueue, type AddResult } from "@/lib/queue";
import type { TrackMetadata } from "@/types/api";
import { useMetadata } from "@/hooks/useMetadata";
import { useLyrics } from "@/hooks/useLyrics";
import { useCover } from "@/hooks/useCover";
import { useAvailability } from "@/hooks/useAvailability";
import { ensureApiStatusCheckStarted } from "@/lib/api-status";
import { useDownloadProgress } from "@/hooks/useDownloadProgress";
import { buildPlaylistFolderName } from "@/lib/playlist";
import { isNewerVersion } from "@/lib/version";
const HISTORY_KEY = "auralis_fetch_history";
const MAX_HISTORY = 5;
const TOOL_NAVIGATION_PAGES = new Set<PageType>(["tools", "audio-analysis", "tempo-key-analyzer", "replaygain", "audio-converter", "audio-resampler", "file-manager", "lyrics-manager", "enrich"]);
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
    try {
        const parsed = new URL(trimmed);
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
    }
    catch {
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
function parseStoredHistory(value: string | null): HistoryItem[] {
    if (!value) {
        return [];
    }
    try {
        const parsed = JSON.parse(value);
        return Array.isArray(parsed) ? parsed : [];
    }
    catch (err) {
        console.error("Failed to parse stored history:", err);
        return [];
    }
}
function App() {
    const { t } = useTranslation();
    const [currentPage, setCurrentPage] = useState<PageType>("main");
    const [toolNavigation, setToolNavigation] = useState<{
        history: PageType[];
        index: number;
    }>({ history: ["tools"], index: 0 });
    const [activeToolGroup, setActiveToolGroup] = useState<ToolGroup>("analysis");
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
    const [pendingPageChange, setPendingPageChange] = useState<PageType | null>(null);
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
    const downloadProgress = useDownloadProgress();
    useEffect(() => {
        setSpotifyUrl(metadata.navigationUrl);
        setSmartSearchInput(metadata.navigationUrl);
    }, [metadata.navigationUrl]);
    useEffect(() => {
        let first = true;
        const seen = new Set<string>();
        EventsOn("ipod:devices", (devices: Array<{ id?: string; name?: string }> | null) => {
            const list = Array.isArray(devices) ? devices : [];
            if (first) {
                first = false;
                for (const device of list) {
                    if (device.id) seen.add(device.id);
                }
                return;
            }
            for (const device of list) {
                if (!device.id || seen.has(device.id)) continue;
                toast.success(i18n.t("translation.devices.connected", { name: device.name || "" }));
            }
            seen.clear();
            for (const device of list) {
                if (device.id) seen.add(device.id);
            }
        });
        return () => {
            EventsOff("ipod:devices");
        };
    }, []);
    const [isFFmpegInstalled, setIsFFmpegInstalled] = useState<boolean | null>(null);
    const [isInstallingFFmpeg, setIsInstallingFFmpeg] = useState(false);
    const [ffmpegInstallProgress, setFfmpegInstallProgress] = useState(0);
    const [ffmpegInstallStatus, setFfmpegInstallStatus] = useState("");
    useLayoutEffectInit();
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
        initSettings();
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
        checkFFmpeg();
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            const currentSettings = getSettings();
            if (currentSettings.themeMode === "auto") {
                applyThemeMode("auto");
            }
        };
        mediaQuery.addEventListener("change", handleChange);
        checkForUpdates();
        ensureApiStatusCheckStarted();
        void loadHistory();
        return () => {
            mediaQuery.removeEventListener("change", handleChange);
        };
    }, []);
    useEffect(() => {
        setSelectedTracks([]);
        setSearchQuery("");
        download.resetDownloadedTracks();
        lyrics.resetLyricsState();
        cover.resetCoverState();
        availability.clearAvailability();
        setSortBy("default");
        setCurrentListPage(1);
    }, [metadata.metadata]);
    const checkForUpdates = async () => {
        try {
            const response = await fetch("https://api.github.com/repos/vekhyat/Auralis/releases/latest");
            const data = await response.json();
            const rawTag = data.tag_name || "";
            const latestVersion = rawTag.replace(/^v/, "") || "";
            if (latestVersion && isNewerVersion(latestVersion, CURRENT_VERSION)) {
                setUpdateInfo({
                    version: latestVersion,
                    changelog: extractMarkdownSection(data.body || "", "Changelog"),
                    url: `https://github.com/vekhyat/Auralis/releases/tag/${rawTag}`,
                });
                if (getSettings().showUpdateNotifications) {
                    setShowUpdateDialog(true);
                }
            }
        }
        catch (err) {
            console.error("Failed to check for updates:", err);
        }
    };
    const persistRecentHistory = useCallback(async (history: HistoryItem[]) => {
        try {
            await SaveRecentFetches(JSON.stringify(history));
        }
        catch (err) {
            console.error("Failed to save recent fetches:", err);
        }
    }, []);
    const loadHistory = useCallback(async () => {
        try {
            const saved = parseStoredHistory(localStorage.getItem(HISTORY_KEY));
            const persisted = parseStoredHistory(await GetRecentFetches());
            const normalized = normalizeHistoryItems([...persisted, ...saved]);
            setFetchHistory(normalized);
            await persistRecentHistory(normalized);
        }
        catch (err) {
            console.error("Failed to load history:", err);
        }
        finally {
            localStorage.removeItem(HISTORY_KEY);
        }
    }, [persistRecentHistory]);
    const handleInstallFFmpeg = async () => {
        setIsInstallingFFmpeg(true);
        setFfmpegInstallProgress(0);
        setFfmpegInstallStatus("starting");
        try {
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
            const response = await DownloadFFmpeg();
            EventsOff("ffmpeg:progress");
            EventsOff("ffmpeg:status");
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
            setIsInstallingFFmpeg(false);
            setFfmpegInstallProgress(0);
            setFfmpegInstallStatus("");
        }
    };
    const addToHistory = (item: Omit<HistoryItem, "id" | "timestamp">) => {
        setFetchHistory((prev) => {
            const normalizedUrl = normalizeHistoryURL(item.url);
            const identityKey = getHistoryIdentityKey(item.type, normalizedUrl);
            const filtered = prev.filter((h) => getHistoryIdentityKey(h.type, h.url) !== identityKey);
            const newItem: HistoryItem = {
                ...item,
                url: normalizedUrl,
                id: crypto.randomUUID(),
                timestamp: Date.now(),
            };
            const updated = normalizeHistoryItems([newItem, ...filtered]);
            void persistRecentHistory(updated);
            return updated;
        });
    };
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
        setSmartSearchInput(item.url);
        setSpotifyUrl(item.url);
        const updatedUrl = await metadata.handleFetchMetadata(item.url, originUrl);
        if (updatedUrl) {
            setSpotifyUrl(updatedUrl);
        }
    };
    const handleFetchMetadata = useCallback(async () => {
        const requestedUrl = smartSearchInput.trim();
        setSpotifyUrl(requestedUrl);
        const updatedUrl = await metadata.handleFetchMetadata(requestedUrl, metadata.metadata ? undefined : requestedUrl);
        if (updatedUrl) {
            setSpotifyUrl(updatedUrl);
            setSmartSearchInput(updatedUrl);
        }
    }, [smartSearchInput, metadata]);
    // The omnibar owns link classification and catalog search; App keeps the
    // fetch pipeline so queue/download behavior never changes.
    const omnibar = useSmartSearch({
        url: smartSearchInput,
        onUrlChange: setSmartSearchInput,
        onFetch: () => void handleFetchMetadata(),
        onFetchUrl: async (url) => {
            const originUrl = metadata.metadata ? undefined : smartSearchInput;
            setSmartSearchInput(url);
            setSpotifyUrl(url);
            const updatedUrl = await metadata.handleFetchMetadata(url, originUrl);
            if (updatedUrl) {
                setSpotifyUrl(updatedUrl);
                setSmartSearchInput(updatedUrl);
            }
        },
    });
    const isSearchMode = omnibar.inputKind === "search";
    useEffect(() => {
        if (!metadata.metadata || !spotifyUrl)
            return;
        let historyItem: Omit<HistoryItem, "id" | "timestamp"> | null = null;
        if ("track" in metadata.metadata) {
            const { track } = metadata.metadata;
            historyItem = {
                url: spotifyUrl,
                type: "track",
                name: track.name,
                artist: track.artists,
                image: track.images,
                is_explicit: track.is_explicit,
            };
        }
        else if ("album_info" in metadata.metadata) {
            const { album_info } = metadata.metadata;
            historyItem = {
                url: spotifyUrl,
                type: "album",
                name: album_info.name,
                artist: `${album_info.total_tracks.toLocaleString()} tracks`,
                image: album_info.images,
                is_explicit: album_info.is_explicit,
            };
        }
        else if ("playlist_info" in metadata.metadata) {
            const { playlist_info } = metadata.metadata;
            historyItem = {
                url: spotifyUrl,
                type: "playlist",
                name: playlist_info.owner.name,
                artist: `${playlist_info.tracks.total.toLocaleString()} tracks`,
                image: playlist_info.cover || playlist_info.owner.images || "",
            };
        }
        else if ("artist_info" in metadata.metadata) {
            const { artist_info } = metadata.metadata;
            historyItem = {
                url: spotifyUrl,
                type: "artist",
                name: artist_info.name,
                artist: `${artist_info.total_albums.toLocaleString()} albums`,
                image: artist_info.images,
            };
        }
        if (historyItem) {
            addToHistory(historyItem);
        }
    }, [metadata.metadata]);
    const handleSearchChange = (value: string) => {
        setSearchQuery(value);
        setCurrentListPage(1);
    };
    const toggleTrackSelection = (id: string) => {
        setSelectedTracks((prev) => prev.includes(id) ? prev.filter((prevId) => prevId !== id) : [...prev, id]);
    };
    const toggleSelectAll = (tracks: any[]) => {
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
            toast.info(t("translation.queue.alreadyInQueue"));
            return;
        }
        toast.success(t("translation.queue.addedValue1Queue", { value1: label }));
    }, []);
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
            toast.info(t("translation.queue.alreadyInQueue"));
        }
        else {
            toast.success(t("translation.queue.addedValue1TracksQueue", { value1: result.added.toLocaleString() }));
        }
    }, [reportQueueAdd]);
    const handleQueueSelectedTracks = useCallback((tracks: TrackMetadata[], folderName?: string) => {
        const selected = tracks.filter((track) => track.spotify_id && selectedTracks.includes(track.spotify_id));
        if (selected.length === 0) {
            toast.error(t("translation.download.noTracksSelected"));
            return;
        }
        handleQueueTracks(selected, folderName);
    }, [handleQueueTracks, selectedTracks]);
    const handleQueueCollection = useCallback((input: Parameters<typeof addCollectionToQueue>[0]) => {
        reportQueueAdd(addCollectionToQueue(input), input.name);
    }, [reportQueueAdd]);
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
            setSmartSearchInput(url);
        }
    };
    const handleMetadataForward = () => {
        const url = metadata.goForward();
        if (url !== null) {
            setSpotifyUrl(url);
            setSmartSearchInput(url);
        }
    };
    const moveToolNavigation = (offset: -1 | 1) => {
        const nextIndex = toolNavigation.index + offset;
        const nextPage = toolNavigation.history[nextIndex];
        if (!nextPage || nextIndex < 0 || nextIndex >= toolNavigation.history.length) {
            return;
        }
        setToolNavigation((previous) => ({ ...previous, index: nextIndex }));
        setCurrentPage(nextPage);
    };
    const handleTitleBarBack = () => {
        if (TOOL_NAVIGATION_PAGES.has(currentPage)) {
            moveToolNavigation(-1);
            return;
        }
        if (currentPage === "main") {
            handleMetadataBack();
        }
    };
    const handleTitleBarForward = () => {
        if (TOOL_NAVIGATION_PAGES.has(currentPage)) {
            moveToolNavigation(1);
            return;
        }
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
            return (<TrackInfo track={track} isDownloading={download.isDownloading} downloadingTrack={download.downloadingTrack} isDownloaded={download.downloadedTracks.has(trackId)} isFailed={download.failedTracks.has(trackId)} isSkipped={download.skippedTracks.has(trackId)} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} downloadedLyrics={lyrics.downloadedLyrics.has(track.spotify_id || "")} failedLyrics={lyrics.failedLyrics.has(track.spotify_id || "")} skippedLyrics={lyrics.skippedLyrics.has(track.spotify_id || "")} checkingAvailability={availability.checkingTrackId === track.spotify_id} availability={availability.availabilityMap.get(track.spotify_id || "")} downloadingCover={cover.downloadingCoverTrack === (track.spotify_id || `${track.name}-${track.artists}`)} downloadedCover={cover.downloadedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} failedCover={cover.failedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} skippedCover={cover.skippedCovers.has(track.spotify_id || `${track.name}-${track.artists}`)} onDownload={download.handleDownloadTrack} onQueueTrack={(queuedTrack) => handleQueueTracks([queuedTrack])} onDownloadLyrics={(spotifyId, name, artists, albumName, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, undefined, undefined, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _playlistName, _position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, undefined, undefined, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onOpenFolder={handleOpenFolder} onAlbumClick={metadata.handleAlbumClick} onArtistClick={async (artist) => {
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
            return (<AlbumInfo albumInfo={album_info} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, album_info.name, position, albumArtist, releaseDate, discNumber, true)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, album_info.name, position, trackId, albumArtist, releaseDate, discNumber, true)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, album_info.name, undefined, true)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, album_info.name, true)} onQueueAll={() => handleQueueCollection({ type: "album", name: album_info.name, artist: album_info.artists, info: `${track_list.length.toLocaleString()} tracks`, image: album_info.images, folderName: album_info.name, isAlbum: true, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, album_info.name)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], album_info.name, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onBack={metadata.resetMetadata} onArtistClick={async (artist) => {
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
            return (<PlaylistInfo playlistInfo={playlist_info} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, playlistFolderName, position, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, playlistFolderName, position, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, playlistFolderName)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, playlistFolderName)} onQueueAll={() => handleQueueCollection({ type: "playlist", name: playlist_info.owner.name, artist: playlist_info.owner.display_name, info: `${track_list.length.toLocaleString()} tracks`, image: playlist_info.cover || playlist_info.owner.images || "", folderName: playlistFolderName, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, playlistFolderName)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], playlistFolderName, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onBack={metadata.resetMetadata} onAlbumClick={metadata.handleAlbumClick} onArtistClick={async (artist) => {
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
            return (<ArtistInfo artistInfo={artist_info} albumList={album_list} trackList={track_list} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} currentPage={currentListPage} itemsPerPage={ITEMS_PER_PAGE} downloadedLyrics={lyrics.downloadedLyrics} failedLyrics={lyrics.failedLyrics} skippedLyrics={lyrics.skippedLyrics} downloadingLyricsTrack={lyrics.downloadingLyricsTrack} checkingAvailabilityTrack={availability.checkingTrackId} availabilityMap={availability.availabilityMap} downloadedCovers={cover.downloadedCovers} failedCovers={cover.failedCovers} skippedCovers={cover.skippedCovers} downloadingCoverTrack={cover.downloadingCoverTrack} isBulkDownloadingCovers={cover.isBulkDownloadingCovers} isBulkDownloadingLyrics={lyrics.isBulkDownloadingLyrics} isMetadataLoading={metadata.loading} onSearchChange={handleSearchChange} onSortChange={setSortBy} onToggleTrack={toggleTrackSelection} onToggleSelectAll={toggleSelectAll} onSelectTrackRange={selectTrackRange} onDownloadLyrics={(spotifyId, name, artists, albumName, _folderName, _isArtistDiscography, position, albumArtist, releaseDate, discNumber) => lyrics.handleDownloadLyrics(spotifyId, name, artists, albumName, artist_info.name, position, albumArtist, releaseDate, discNumber)} onDownloadCover={(coverUrl, trackName, artistName, albumName, _folderName, _isArtistDiscography, position, trackId, albumArtist, releaseDate, discNumber) => cover.handleDownloadCover(coverUrl, trackName, artistName, albumName, artist_info.name, position, trackId, albumArtist, releaseDate, discNumber)} onCheckAvailability={availability.checkAvailability} onDownloadAllLyrics={() => lyrics.handleDownloadAllLyrics(track_list, artist_info.name)} onDownloadAllCovers={() => cover.handleDownloadAllCovers(track_list, artist_info.name)} onQueueAll={() => handleQueueCollection({ type: "artist", name: artist_info.name, artist: artist_info.name, info: `${track_list.length.toLocaleString()} tracks`, image: artist_info.images, folderName: artist_info.name, tracks: track_list })} onQueueSelected={() => handleQueueSelectedTracks(track_list, artist_info.name)} onQueueTrack={(queuedTrack, position) => handleQueueTracks([queuedTrack], artist_info.name, position)} onOpenFolder={handleOpenFolder} onPageChange={setCurrentListPage} onAlbumClick={metadata.handleAlbumClick} onBack={metadata.resetMetadata} onArtistClick={async (artist) => {
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
    const commitPageNavigation = (page: PageType) => {
        if (page === currentPage) {
            return;
        }
        if (TOOL_NAVIGATION_PAGES.has(page)) {
            setToolNavigation((previous) => {
                if (!TOOL_NAVIGATION_PAGES.has(currentPage)) {
                    const history: PageType[] = page === "tools" ? ["tools"] : ["tools", page];
                    return { history, index: history.length - 1 };
                }
                const currentEntry = previous.history[previous.index];
                if (currentEntry === page) {
                    return previous;
                }
                const history = [...previous.history.slice(0, previous.index + 1), page];
                return { history, index: history.length - 1 };
            });
        }
        setCurrentPage(page);
    };
    const handlePageChange = (page: PageType) => {
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
    const renderPage = () => {
        switch (currentPage) {
            case "settings":
                return <SettingsPage onUnsavedChangesChange={setHasUnsavedSettings} onResetRequest={setResetSettingsFn}/>;
            case "debug":
                return <DebugLoggerPage />;
            case "devices":
                return <DevicesPage />;
            case "history":
                return <HistoryPage onHistorySelect={(item) => {
                        setSmartSearchInput(item.url);
                        setSpotifyUrl(item.url);
                        metadata.loadFromCache(item.data, item.url);
                        setCurrentPage("main");
                    }}/>;
            case "queue":
                return <QueuePage items={queue.items} isProcessing={queue.isProcessing} isPausing={queue.isPausing} processingType={queue.processingType} downloadedTracks={download.downloadedTracks} failedTracks={download.failedTracks} skippedTracks={download.skippedTracks} downloadingTracks={download.downloadingTrack ? new Set([download.downloadingTrack]) : new Set()} onStart={queue.start} onPause={queue.pause} onStop={queue.stop} isDirectDownloading={download.isDownloading || download.downloadingTrack !== null} onStopDirect={download.handleStopDownload}/>;
            case "tools":
                return <ToolsPage activeGroup={activeToolGroup} onActiveGroupChange={setActiveToolGroup} onPageChange={handlePageChange}/>;
            case "audio-analysis":
                return <AudioAnalysisPage />;
            case "tempo-key-analyzer":
                return <TempoKeyAnalyzerPage />;
            case "replaygain":
                return <ReplayGainPage />;
            case "audio-converter":
                return <AudioConverterPage />;
            case "audio-resampler":
                return <AudioResamplerPage />;
            case "file-manager":
                return <FileManagerPage />;
            case "lyrics-manager":
                return <LyricsManagerPage />;
            case "enrich":
                return <EnrichPage />;
            default:
                return (<>
                    <CatalogPane
                      controller={omnibar}
                      query={smartSearchInput}
                      history={fetchHistory}
                      onHistorySelect={handleHistorySelect}
                      onHistoryRemove={removeFromHistory}
                      onRecentSearchSelect={omnibar.handleInputChange}
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
              canGoBack={TOOL_NAVIGATION_PAGES.has(currentPage) ? toolNavigation.index > 0 : currentPage === "main" && metadata.canGoBack}
              canGoForward={TOOL_NAVIGATION_PAGES.has(currentPage) ? toolNavigation.index < toolNavigation.history.length - 1 : currentPage === "main" && metadata.canGoForward}
              navigationDisabled={currentPage === "main" && metadata.loading}
              onBack={handleTitleBarBack}
              onForward={handleTitleBarForward}
              currentPage={currentPage}
              onPageChange={handlePageChange}
              queueCount={queue.items.filter((item) => item.status === "pending" || item.status === "running").length}
              omnibar={{
                  value: smartSearchInput,
                  loading: metadata.loading || omnibar.isSearching,
                  onChange: omnibar.handleInputChange,
                  onSubmit: omnibar.submit,
              }}
            />

            <main
              data-page={currentPage}
              className="fixed inset-x-0 top-11 bottom-0 overflow-y-auto overflow-x-hidden"
            >
                <div className="px-6 py-5">
                    {renderPage()}
                </div>
            </main>

            <DownloadProgressToast isPreparing={queue.isProcessing} onOpenQueue={() => handlePageChange("queue")}/>

            <CooldownBanner />

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
                        <DialogTitle>{t("translation.app.fetchFailed")}</DialogTitle>
                        <DialogDescription className="space-y-3">
                            <span className="block">
                                {t("translation.migrated.App.metadataFetchFailedTryUsingAHigh")}
                            </span>
                            <span className="block">
                                {t("translation.migrated.App.chooseALocationThatIsNotBlocked")}
                            </span>
                            <span className="block">
                                {t("translation.migrated.App.ifYouAreAlreadyUsingAVPN")}
                            </span>
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button onClick={() => metadata.setShowVpnAdviceDialog(false)}>
                            {t("translation.common.close")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={isFFmpegInstalled === false} onOpenChange={() => { }}>
                <DialogContent className="max-w-112.5 gap-5 p-6 [&>button]:hidden">
                    <DialogHeader className="space-y-2">
                        <DialogTitle className="text-lg font-bold tracking-tight">
                            {t("translation.migrated.App.ffmpegRequired")}
                        </DialogTitle>
                        <DialogDescription className="text-sm leading-relaxed font-normal text-foreground/70">
                            {t("translation.migrated.App.spotiflacChecksYourSystemForFFmpegAnd")} <span className="font-semibold text-foreground">30-40MB</span> {t("translation.migrated.App.ofData")}
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
                                        <div className="flex flex-col gap-0.5">
                                            <span className="tracking-wider uppercase text-muted-foreground">{t("translation.app.downloading")}</span>
                                            {downloadProgress.is_downloading && downloadProgress.mb_downloaded > 0 && (<span className="font-mono tabular-nums text-primary">
                                                    {downloadProgress.mb_downloaded.toFixed(1)}{t("literal.common.mb")}
                                                    {downloadProgress.speed_mbps > 0 && <> @ {downloadProgress.speed_mbps.toFixed(1)}{t("literal.downloadProgressToast.mbS")}</>}
                                                </span>)}
                                        </div>
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
