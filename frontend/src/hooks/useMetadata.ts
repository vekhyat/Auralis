import { useEffect, useRef, useState } from "react";
import { t, translateMessage } from "@/i18n";
import { fetchSpotifyMetadata } from "@/lib/api";
import { adoptCorrelatedStreamBegin, allowsRequestOutcome, armRequestStream, bindRequestId, claimRequest, createRequestClock, releaseRequest, retireRequest, streamChunkBelongs, type RequestOutcome, } from "@/lib/request-generation";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { logger } from "@/lib/logger";
import { AddFetchHistory, SearchSpotifyByType } from "../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import type { SpotifyMetadataResponse, TrackMetadata } from "@/types/api";
function isRecord(value: unknown): value is Record<string, unknown> {
    return !!value && typeof value === "object" && !Array.isArray(value);
}
function nestedName(value: unknown): string {
    return isRecord(value) && typeof value.name === "string" ? value.name : "";
}
function streamBaseName(payload: Record<string, unknown>): string {
    const artistName = nestedName(payload.artist_info);
    if (artistName)
        return artistName;
    const albumName = nestedName(payload.album_info);
    if (albumName)
        return albumName;
    if (!isRecord(payload.playlist_info))
        return "";
    return nestedName(payload.playlist_info) || nestedName(payload.playlist_info.owner);
}
function asMetadataBase(payload: Record<string, unknown>): SpotifyMetadataResponse {
    if ("track_list" in payload) {
        return payload as unknown as SpotifyMetadataResponse;
    }
    return { ...payload, track_list: [] } as unknown as SpotifyMetadataResponse;
}
export function useMetadata() {
    const [loading, setLoading] = useState(false);
    const [metadata, setMetadata] = useState<SpotifyMetadataResponse | null>(null);
    const navigationHistory = useRef<Array<{
        url: string;
        metadata: SpotifyMetadataResponse | null;
    }>>([{ url: "", metadata: null }]);
    const navigationIndex = useRef(0);
    const [navigationState, setNavigationState] = useState({ canGoBack: false, canGoForward: false, currentUrl: "" });
    const [showVpnAdviceDialog, setShowVpnAdviceDialog] = useState(false);
    const [fetchFailureReason, setFetchFailureReason] = useState("");
    const loadingToastId = useRef<string | number | null>(null);
    const fetchedCount = useRef(0);
    const currentName = useRef("");
    const clock = useRef(createRequestClock());
    const allows = (generation: number, outcome: RequestOutcome, streamId?: number) => allowsRequestOutcome(clock.current, generation, outcome, streamId);
    const beginRequest = () => {
        const claimed = claimRequest(clock.current);
        clock.current = bindRequestId(claimed.clock, claimed.generation, crypto.randomUUID());
        setLoading(true);
        return claimed.generation;
    };
    const retireInFlight = () => {
        clock.current = retireRequest(clock.current);
        setLoading(false);
    };
    const finishOwnedRequest = (generation: number) => {
        if (!allows(generation, "finally")) {
            return;
        }
        clock.current = releaseRequest(clock.current, generation);
        setLoading(false);
    };
    const updateNavigationState = () => {
        setNavigationState({
            canGoBack: navigationIndex.current > 0,
            canGoForward: navigationIndex.current < navigationHistory.current.length - 1,
            currentUrl: navigationHistory.current[navigationIndex.current]?.url || "",
        });
    };
    const rememberOrigin = (url?: string) => {
        if (url !== undefined) {
            navigationHistory.current[navigationIndex.current] = { ...navigationHistory.current[navigationIndex.current], url };
        }
    };
    const commitNavigation = (url: string, data: SpotifyMetadataResponse) => {
        navigationHistory.current = [...navigationHistory.current.slice(0, navigationIndex.current + 1), { url, metadata: data }];
        navigationIndex.current += 1;
        setMetadata(data);
        updateNavigationState();
    };
    const moveNavigation = (offset: -1 | 1) => {
        const nextIndex = navigationIndex.current + offset;
        if (nextIndex < 0 || nextIndex >= navigationHistory.current.length)
            return null;
        retireInFlight();
        navigationIndex.current = nextIndex;
        const entry = navigationHistory.current[nextIndex];
        setMetadata(entry.metadata);
        updateNavigationState();
        return entry.url;
    };
    const [showAlbumDialog, setShowAlbumDialog] = useState(false);
    const [selectedAlbum, setSelectedAlbum] = useState<{
        id: string;
        name: string;
        external_urls: string;
    } | null>(null);
    const [pendingArtistName, setPendingArtistName] = useState<string | null>(null);
    const showFetchFailureAdvice = (errorMsg: string) => {
        setFetchFailureReason(errorMsg);
        setShowVpnAdviceDialog(true);
    };
    const resolveArtistUrlBySearch = async (artistName: string): Promise<string | null> => {
        const query = artistName.trim();
        if (!query) {
            return null;
        }
        const results = await SearchSpotifyByType({
            query,
            search_type: "artist",
            limit: 10,
            offset: 0,
        });
        const normalizedQuery = query.toLocaleLowerCase();
        const exactMatches = results.filter((result) => result.name.trim().toLocaleLowerCase() === normalizedQuery);
        return exactMatches.length === 1 ? exactMatches[0]?.external_urls || null : null;
    };
    useEffect(() => {
        if (loading) {
            fetchedCount.current = 0;
            currentName.current = "";
            loadingToastId.current = toast.silentInfo(t("translation.download.fetchingMetadata"), {
                duration: Infinity,
                description: t("translation.download.pleaseWaitWhileWeRetrieve")
            });
            return;
        }
        if (loadingToastId.current) {
            toast.dismiss(loadingToastId.current);
            loadingToastId.current = null;
        }
    }, [loading]);
    useEffect(() => {
        const beginHandler = (event: unknown) => {
            clock.current = adoptCorrelatedStreamBegin(clock.current, event);
        };
        EventsOn("metadata-stream-begin", beginHandler);
        const handler = (event: unknown) => {
            const chunk = streamChunkBelongs(clock.current, clock.current.current, event);
            if (!chunk) {
                return;
            }
            const payload = chunk.payload;
            if (Array.isArray(payload)) {
                const tracks = payload as TrackMetadata[];
                fetchedCount.current += tracks.length;
                if (loadingToastId.current && currentName.current) {
                    toast.silentInfo(t("translation.migrated.useMetadata.fetchingTracksFor", { value1: currentName.current.toLowerCase() }), {
                        id: loadingToastId.current,
                        description: t("translation.metadata.fetched", { count: fetchedCount.current, formattedCount: fetchedCount.current.toLocaleString() })
                    });
                }
                setMetadata(prev => {
                    if (!prev || !("track_list" in prev)) {
                        return prev;
                    }
                    return {
                        ...prev,
                        track_list: [...prev.track_list, ...tracks]
                    };
                });
                return;
            }
            if (!isRecord(payload)) {
                return;
            }
            const name = streamBaseName(payload);
            if (name) {
                currentName.current = name;
                if (loadingToastId.current) {
                    toast.silentInfo(t("translation.migrated.useMetadata.fetchingTracksFor", { value1: name.toLowerCase() }), {
                        id: loadingToastId.current,
                        description: t("translation.metadata.fetched", { count: fetchedCount.current, formattedCount: fetchedCount.current.toLocaleString() })
                    });
                }
            }
            setMetadata(prev => {
                if (prev && "track_list" in prev && prev.track_list.length > 0) {
                    return prev;
                }
                return asMetadataBase(payload);
            });
        };
        EventsOn("metadata-stream", handler);
        return () => {
            EventsOff("metadata-stream");
            EventsOff("metadata-stream-begin");
        };
    }, []);
    const getUrlType = (url: string): string => {
        if (url.includes("/track/"))
            return "track";
        if (url.includes("/album/"))
            return "album";
        if (url.includes("/playlist/"))
            return "playlist";
        if (url.includes("/artist/"))
            return "artist";
        return "unknown";
    };
    const saveToHistory = async (url: string, data: SpotifyMetadataResponse) => {
        try {
            let name = "";
            let info = "";
            let image = "";
            let type = "unknown";
            if ("track" in data) {
                type = "track";
                name = data.track.name;
                info = data.track.artists;
                image = (data.track.images && data.track.images.length > 0) ? data.track.images : "";
            }
            else if ("album_info" in data) {
                type = "album";
                name = data.album_info.name;
                info = `${data.track_list.length} tracks`;
                image = data.album_info.images;
            }
            else if ("playlist_info" in data) {
                type = "playlist";
                if (data.playlist_info.name) {
                    name = data.playlist_info.name;
                }
                else if (data.playlist_info.owner.name) {
                    name = data.playlist_info.owner.name;
                }
                info = `${data.playlist_info.tracks.total} tracks`;
                image = data.playlist_info.cover || "";
            }
            else if ("artist_info" in data) {
                type = "artist";
                name = data.artist_info.name;
                info = `${data.artist_info.total_albums || data.album_list.length} albums`;
                image = data.artist_info.images;
            }
            const jsonStr = JSON.stringify(data);
            await AddFetchHistory({
                id: crypto.randomUUID(),
                url: url,
                type: type,
                name: name,
                info: info,
                image: image,
                data: jsonStr,
                is_explicit: ("track" in data && Boolean(data.track.is_explicit)) || ("album_info" in data && Boolean(data.album_info.is_explicit)),
                timestamp: Math.floor(Date.now() / 1000)
            });
        }
        catch (err) {
            console.error("Failed to save fetch history:", err);
        }
    };
    const fetchMetadataDirectly = async (url: string, originUrl?: string, ownedGeneration?: number) => {
        const generation = ownedGeneration ?? beginRequest();
        if (!allows(generation, "finally")) {
            return;
        }
        // Artist search claims before the URL exists, so a late begin from the
        // previous read can attach. Drop it when this read actually starts.
        clock.current = armRequestStream(clock.current, generation);
        rememberOrigin(originUrl);
        const urlType = getUrlType(url);
        logger.info(`fetching ${urlType} metadata...`);
        logger.debug(`url: ${url}`);
        setLoading(true);
        setMetadata(null);
        try {
            const startTime = Date.now();
            const timeout = urlType === "artist" ? 60 : 300;
            const data = await fetchSpotifyMetadata(url, true, 1.0, timeout, undefined, clock.current.requestId || undefined);
            if (!allows(generation, "success")) {
                return;
            }
            const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
            if ("playlist_info" in data) {
                const playlistInfo = data.playlist_info;
                if (!playlistInfo.owner.name && playlistInfo.tracks.total === 0 && data.track_list.length === 0) {
                    logger.warning("playlist appears to be empty or private");
                    toast.error(t("translation.download.playlistNotFoundMayBe"));
                    setMetadata(null);
                    return;
                }
            }
            else if ("album_info" in data) {
                const albumInfo = data.album_info;
                if (!albumInfo.name && albumInfo.total_tracks === 0 && data.track_list.length === 0) {
                    logger.warning("album appears to be empty or not found");
                    toast.error(t("translation.download.albumNotFoundMayBe"));
                    setMetadata(null);
                    return;
                }
            }
            commitNavigation(url, data);
            saveToHistory(url, data);
            if ("track" in data) {
                logger.success(`fetched track: ${data.track.name} - ${data.track.artists}`);
                logger.debug(`duration: ${data.track.duration_ms}ms`);
            }
            else if ("album_info" in data) {
                logger.success(`fetched album: ${data.album_info.name}`);
                logger.debug(`${data.track_list.length} tracks, released: ${data.album_info.release_date}`);
            }
            else if ("playlist_info" in data) {
                logger.success(`fetched playlist: ${data.track_list.length} tracks`);
                logger.debug(`by ${data.playlist_info.owner.display_name || data.playlist_info.owner.name}`);
            }
            else if ("artist_info" in data) {
                logger.success(`fetched artist: ${data.artist_info.name}`);
                logger.debug(`${data.album_list.length} albums, ${data.track_list.length} tracks`);
            }
            logger.info(`fetch completed in ${elapsed}s`);
            toast.success(t("translation.download.metadataFetchedSuccessfully"));
        }
        catch (err) {
            if (!allows(generation, "error")) {
                return;
            }
            const rawError = err instanceof Error ? err.message : t("translation.app.fetchFailed");
            const errorMsg = translateMessage(rawError);
            logger.error(`fetch failed: ${errorMsg}`);
            toast.error(errorMsg);
            showFetchFailureAdvice(rawError);
            const current = navigationHistory.current[navigationIndex.current];
            setMetadata(current?.metadata ?? null);
        }
        finally {
            finishOwnedRequest(generation);
        }
    };
    const loadFromCache = (cachedData: string, url = "", originUrl?: string) => {
        try {
            const data = JSON.parse(cachedData);
            retireInFlight();
            rememberOrigin(originUrl);
            commitNavigation(url, data);
            toast.success(t("translation.download.loadedCache"));
        }
        catch (err) {
            console.error("Failed to load from cache:", err);
            toast.error(t("translation.download.failedLoadCache"));
        }
    };
    const handleFetchMetadata = async (url: string, originUrl?: string) => {
        if (!url.trim()) {
            logger.warning("empty url provided");
            toast.error(t("translation.download.pleaseEnterSpotifyUrl"));
            return;
        }
        let urlToFetch = url.trim();
        const isArtistUrl = urlToFetch.includes("/artist/");
        if (isArtistUrl && !urlToFetch.includes("/discography")) {
            urlToFetch = urlToFetch.replace(/\/$/, "") + "/discography/all";
            logger.debug("converted to discography url");
        }
        if (isArtistUrl) {
            logger.info("artist url detected");
            setPendingArtistName(null);
            await fetchMetadataDirectly(urlToFetch, originUrl);
        }
        else {
            await fetchMetadataDirectly(urlToFetch, originUrl);
        }
        return urlToFetch;
    };
    const handleAlbumClick = (album: {
        id: string;
        name: string;
        external_urls: string;
    }) => {
        logger.debug(`album clicked: ${album.name}`);
        setSelectedAlbum(album);
        setShowAlbumDialog(true);
    };
    const handleArtistClick = async (artist: {
        id: string;
        name: string;
        external_urls: string;
    }, originUrl?: string) => {
        logger.debug(`artist clicked: ${artist.name}`);
        const generation = beginRequest();
        const artistID = artist.id.trim();
        const artistUrlFromID = /^[a-zA-Z0-9]{22}$/.test(artistID)
            ? `https://open.spotify.com/artist/${artistID}`
            : "";
        let resolvedArtistUrl = artist.external_urls.trim() || artistUrlFromID;
        try {
            if (!resolvedArtistUrl) {
                resolvedArtistUrl = (await resolveArtistUrlBySearch(artist.name)) || "";
            }
        }
        catch (err) {
            if (!allows(generation, "error")) {
                return "";
            }
            const rawError = err instanceof Error ? err.message : t("translation.app.fetchFailed");
            logger.error(`artist search failed: ${rawError}`);
            toast.error(translateMessage(rawError));
            finishOwnedRequest(generation);
            return "";
        }
        if (!allows(generation, "finally")) {
            return "";
        }
        if (!resolvedArtistUrl) {
            toast.error(t("translation.migrated.useMetadata.artistNotFound", { value1: artist.name }));
            finishOwnedRequest(generation);
            return "";
        }
        const artistUrl = resolvedArtistUrl.includes("/discography")
            ? resolvedArtistUrl
            : resolvedArtistUrl.replace(/\/$/, "") + "/discography/all";
        setPendingArtistName(artist.name);
        await fetchMetadataDirectly(artistUrl, originUrl, generation);
        if (!allows(generation, "success")) {
            return "";
        }
        return resolvedArtistUrl;
    };
    const handleConfirmAlbumFetch = async (originUrl?: string) => {
        if (!selectedAlbum)
            return;
        const albumUrl = selectedAlbum.external_urls;
        const albumName = selectedAlbum.name;
        logger.info(`fetching album: ${albumName}...`);
        logger.debug(`url: ${albumUrl}`);
        const generation = beginRequest();
        setShowAlbumDialog(false);
        setMetadata(null);
        try {
            const startTime = Date.now();
            const data = await fetchSpotifyMetadata(albumUrl, true, 1.0, 300, undefined, clock.current.requestId || undefined);
            if (!allows(generation, "success")) {
                return;
            }
            const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
            if ("album_info" in data) {
                const albumInfo = data.album_info;
                if (!albumInfo.name && albumInfo.total_tracks === 0 && data.track_list.length === 0) {
                    logger.warning("album appears to be empty or not found");
                    toast.error(t("translation.download.albumNotFoundMayBe"));
                    setMetadata(null);
                    setSelectedAlbum(null);
                    return albumUrl;
                }
            }
            rememberOrigin(originUrl);
            commitNavigation(albumUrl, data);
            saveToHistory(albumUrl, data);
            if ("album_info" in data) {
                logger.success(`fetched album: ${data.album_info.name}`);
                logger.debug(`${data.track_list.length} tracks, released: ${data.album_info.release_date}`);
            }
            logger.info(`fetch completed in ${elapsed}s`);
            toast.success(t("translation.download.albumMetadataFetchedSuccessfully"));
            return albumUrl;
        }
        catch (err) {
            if (!allows(generation, "error")) {
                return;
            }
            const rawError = err instanceof Error ? err.message : t("translation.app.fetchFailed");
            const errorMsg = translateMessage(rawError);
            logger.error(`fetch failed: ${errorMsg}`);
            toast.error(errorMsg);
            showFetchFailureAdvice(errorMsg);
        }
        finally {
            if (allows(generation, "finally")) {
                finishOwnedRequest(generation);
                setSelectedAlbum(null);
            }
        }
    };
    return {
        loading,
        metadata,
        showVpnAdviceDialog,
        setShowVpnAdviceDialog,
        fetchFailureReason,
        showAlbumDialog,
        setShowAlbumDialog,
        selectedAlbum,
        pendingArtistName,
        canGoBack: navigationState.canGoBack,
        canGoForward: navigationState.canGoForward,
        navigationUrl: navigationState.currentUrl,
        goBack: () => moveNavigation(-1),
        goForward: () => moveNavigation(1),
        handleFetchMetadata,
        handleAlbumClick,
        handleConfirmAlbumFetch,
        handleArtistClick,
        loadFromCache,
        resetMetadata: () => moveNavigation(-1),
        clearMetadata: (url = "") => {
            retireInFlight();
            rememberOrigin(url);
            navigationHistory.current = [...navigationHistory.current.slice(0, navigationIndex.current + 1), { url: "", metadata: null }];
            navigationIndex.current += 1;
            setMetadata(null);
            updateNavigationState();
        },
    };
}
