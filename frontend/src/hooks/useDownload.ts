import { useState } from "react";
import { t, translateMessage } from "@/i18n";
import { downloadExecution, type DownloadExecutionKind } from "@/lib/download-execution";
import { expectedTrackDurationSeconds, type QueueAttemptDisposition } from "@/lib/queue-guards";
import { downloadTrack, fetchSpotifyMetadata } from "@/lib/api";
import { getSettings, parseTemplate, extendAutoOrder, getEffectiveAlbumFilenameTemplate, templateUsesAlbumTrackNumber, getAlbumCategoryLabel, type Settings, type TemplateData } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { joinPath, sanitizePath, getFirstArtist } from "@/lib/utils";
import { logger } from "@/lib/logger";
import { writeAutomaticReplayGainTags } from "@/lib/replaygain";
import type { DownloadResponse, TrackMetadata } from "@/types/api";
import { beginDirectCollectionQueueItem, beginDirectTrackQueueItem, finishDirectQueueItem, getQueueTrackStatusSets, updateQueueTrackResult, type QueueExecutionResult, type QueueItemType, type QueueResumeContext, type QueueTrackStatus } from "@/lib/queue";
type BatchDownloadSource = "playlist" | "album" | "discography" | "collection";
function getCompleteAlbumPaths(tracks: TrackMetadata[], filePaths: Map<string, string>, completedTrackIDs: Set<string>): string[] | null {
    if (tracks.length < 2)
        return null;
    const expectedTrackCount = Math.max(0, ...tracks.map((track) => track.total_tracks || 0));
    if (expectedTrackCount === 0 || tracks.length !== expectedTrackCount)
        return null;
    if (tracks.some((track) => !completedTrackIDs.has(track.spotify_id || "")))
        return null;
    const paths = tracks.map((track) => filePaths.get(track.spotify_id || "") || "");
    if (paths.some((path) => path === "") || new Set(paths).size !== paths.length)
        return null;
    return paths;
}
function getCompletedTrackPaths(tracks: TrackMetadata[], filePaths: Map<string, string>, completedTrackIDs: Set<string>): string[] {
    return tracks
        .filter((track) => completedTrackIDs.has(track.spotify_id || ""))
        .map((track) => filePaths.get(track.spotify_id || "") || "")
        .filter(Boolean);
}
async function applyAutomaticReplayGain(filePaths: string[], writeAlbumTags: boolean): Promise<void> {
    const paths = Array.from(new Set(filePaths.filter(Boolean)));
    if (paths.length === 0)
        return;
    const progressToast = toast.silentInfo(t("translation.replayGain.autoAnalyzing", { value1: paths.length }), {
        duration: Infinity,
    });
    try {
        logger.info(`automatic ReplayGain analysis started for ${paths.length} file(s)`);
        const result = await writeAutomaticReplayGainTags(paths, writeAlbumTags);
        toast.dismiss(progressToast);
        for (const error of result.errors) {
            logger.warning(`automatic ReplayGain: ${error}`);
        }
        const description = result.errors[0];
        if (result.tagged === result.requested && (!result.albumRequested || result.albumApplied)) {
            toast.success(t("translation.replayGain.autoWriteComplete", { value1: result.tagged }));
        }
        else if (result.tagged > 0 && result.tagged < result.requested) {
            toast.warning(t("translation.replayGain.autoWritePartial", { value1: result.tagged, value2: result.requested }), { description });
        }
        else if (result.tagged > 0 && result.albumRequested && !result.albumApplied) {
            toast.warning(t("translation.replayGain.autoAlbumFallback"), { description });
        }
        else {
            toast.warning(t("translation.replayGain.autoWriteFailed"), { description });
        }
    }
    catch (error) {
        toast.dismiss(progressToast);
        logger.warning(`automatic ReplayGain failed: ${error}`);
        toast.warning(t("translation.replayGain.autoWriteFailed"), { description: String(error) });
    }
}
function queueCollectionType(source: BatchDownloadSource, tracks: TrackMetadata[] = []): Exclude<QueueItemType, "track"> {
    if (source === "album")
        return "album";
    if (source === "discography")
        return "artist";
    if (source === "collection") {
        const artistIds = new Set(tracks.map((track) => track.artist_id).filter(Boolean));
        const albumIds = new Set(tracks.map((track) => track.album_id || track.album_name).filter(Boolean));
        if (artistIds.size === 1 && albumIds.size > 1)
            return "artist";
    }
    return "playlist";
}
function buildQueueExecutionResult(tracks: TrackMetadata[], skipped: Set<string>, failed: Map<string, string>, completed: Set<string>, cancelled = false, paused = false, trackFilePaths: Record<string, string> = {}): QueueExecutionResult {
    const trackResults: Record<string, QueueTrackStatus> = {};
    let successCount = 0;
    let skippedCount = 0;
    let failedCount = 0;
    for (const track of tracks) {
        const id = track.spotify_id || "";
        if (!id)
            continue;
        if (failed.has(id)) {
            trackResults[id] = "failed";
            failedCount++;
        }
        else if (skipped.has(id)) {
            trackResults[id] = "skipped";
            skippedCount++;
        }
        else if (completed.has(id)) {
            trackResults[id] = "done";
            successCount++;
        }
    }
    return { trackResults, trackFilePaths, successCount, skippedCount, failedCount, cancelled, paused };
}
function buildFailedQueueExecutionResult(tracks: TrackMetadata[], message: string, cancelled = false): QueueExecutionResult {
    const failures = new Map<string, string>();
    for (const track of tracks) {
        if (track.spotify_id)
            failures.set(track.spotify_id, message);
    }
    return buildQueueExecutionResult(tracks, new Set<string>(), failures, new Set<string>(), cancelled);
}
function isCooldownMessage(message?: string): boolean {
    if (!message)
        return false;
    const lower = message.toLowerCase();
    return lower.includes("short break")
        || lower.includes("scheduled cooldown")
        || lower.includes("temporarily skipped after rate limit")
        || (lower.includes("rate limit") && (lower.includes("skip") || lower.includes("remaining")));
}
function getCooldownFailure(error: unknown): DownloadResponse | null {
    const message = error instanceof Error ? error.message : String(error);
    return isCooldownMessage(message) ? { success: false, message, error: message } : null;
}
function isCancelFailure(error: unknown): boolean {
    const message = error instanceof Error ? error.message : String(error);
    return message.toLowerCase().includes("download cancelled");
}
function cancelledDownloadResponse(error?: unknown): DownloadResponse {
    const message = error instanceof Error ? error.message : (typeof error === "string" ? error : "Download cancelled");
    const text = message || "Download cancelled";
    return { success: false, cancelled: true, error: text, message: text };
}
function formatSourceSuffix(response: {
    source_url?: string;
    source_label?: string;
}): string {
    const url = response.source_url?.trim();
    const label = response.source_label?.trim();
    if (label && url)
        return ` [source: ${label} → ${url}]`;
    if (url)
        return ` [source: ${url}]`;
    if (label)
        return ` [source: ${label}]`;
    return "";
}
interface CheckFileExistenceRequest {
    spotify_id: string;
    track_name: string;
    artist_name: string;
    artists?: string;
    album_name?: string;
    album_artist?: string;
    album_artists?: string;
    release_date?: string;
    isrc?: string;
    track_number?: number;
    disc_number?: number;
    position?: number;
    use_album_track_number?: boolean;
    filename_format?: string;
    include_track_number?: boolean;
    audio_format?: string;
    relative_path?: string;
    duration?: number;
}
interface FileExistenceResult {
    spotify_id: string;
    exists: boolean;
    file_path?: string;
    track_name?: string;
    artist_name?: string;
}
interface DesktopAppBindings {
    CheckFilesExistence: (outputDir: string, rootDir: string, tracks: CheckFileExistenceRequest[]) => Promise<FileExistenceResult[]>;
    SkipDownloadItem: (itemID: string, filePath: string) => Promise<void>;
    CreateM3U8File: (playlistName: string, outputDir: string, filePaths: string[]) => Promise<void>;
    CreateLogFile: (fileName: string, outputDir: string, logs: string[]) => Promise<void>;
    GetTrackISRC: (spotifyId: string) => Promise<string>;
}
interface StreamingLinks {
    tidal_url?: string;
    amazon_url?: string;
}
function desktopAppBindings(): DesktopAppBindings {
    return (window as unknown as {
        go: { main: { App: DesktopAppBindings } };
    }).go.main.App;
}
function readStreamingLinks(payload: string): StreamingLinks {
    const parsed: unknown = JSON.parse(payload);
    if (!parsed || typeof parsed !== "object") return {};
    const record = parsed as { tidal_url?: unknown; amazon_url?: unknown };
    return {
        tidal_url: typeof record.tidal_url === "string" ? record.tidal_url : undefined,
        amazon_url: typeof record.amazon_url === "string" ? record.amazon_url : undefined,
    };
}
const desktopApp = desktopAppBindings();
const CheckFilesExistence = (outputDir: string, rootDir: string, tracks: CheckFileExistenceRequest[]): Promise<FileExistenceResult[]> => desktopApp.CheckFilesExistence(outputDir, rootDir, tracks);
const SkipDownloadItem = (itemID: string, filePath: string): Promise<void> => desktopApp.SkipDownloadItem(itemID, filePath);
const CreateM3U8File = (playlistName: string, outputDir: string, filePaths: string[]): Promise<void> => desktopApp.CreateM3U8File(playlistName, outputDir, filePaths);
const CreateLogFile = (fileName: string, outputDir: string, logs: string[]): Promise<void> => desktopApp.CreateLogFile(fileName, outputDir, logs);
const GetTrackISRC = (spotifyId: string): Promise<string> => desktopApp.GetTrackISRC(spotifyId);
async function resolveTemplateISRC(settings: {
    folderTemplate?: string;
    filenameTemplate?: string;
    existingFileCheckMode?: string;
}, spotifyId?: string): Promise<string> {
    if (!spotifyId) {
        return "";
    }
    const folderTemplate = settings.folderTemplate || "";
    const filenameTemplate = settings.filenameTemplate || "";
    const shouldResolveISRC = settings.existingFileCheckMode === "isrc" ||
        settings.existingFileCheckMode === "hybrid" ||
        folderTemplate.includes("{isrc}") ||
        filenameTemplate.includes("{isrc}");
    if (!shouldResolveISRC) {
        return "";
    }
    try {
        return await GetTrackISRC(spotifyId);
    }
    catch {
        return "";
    }
}
function getTidalAudioFormat(settings: Settings, mode: "single" | "auto"): "LOSSLESS" | "HI_RES_LOSSLESS" | "ATMOS" {
    if (mode === "auto") {
        if (settings.autoQuality === "atmos")
            return "ATMOS";
        return (settings.autoQuality || "24") === "24" ? "HI_RES_LOSSLESS" : "LOSSLESS";
    }
    return settings.tidalQuality || "LOSSLESS";
}
function getExpectedAudioFormat(settings: {
    autoConvertAudio?: boolean;
    autoConvertFormat?: string;
}): string {
    return settings.autoConvertAudio ? settings.autoConvertFormat || "mp3" : "flac";
}
function deduplicateTracksBySpotifyID(tracks: TrackMetadata[]): TrackMetadata[] {
    const seen = new Set<string>();
    return tracks.filter((track) => {
        const spotifyID = track.spotify_id?.trim() || "";
        if (!spotifyID || seen.has(spotifyID))
            return false;
        seen.add(spotifyID);
        return true;
    });
}
function shouldFetchStreamingURLs(order: string[]): boolean {
    return order.includes("amazon") || order.includes("tidal");
}
function getCustomInstanceFields(tidalApi?: string, qobuzApi?: string) {
    return {
        ...(tidalApi ? { tidal_api_url: tidalApi } : {}),
        ...(qobuzApi ? { qobuz_api_url: qobuzApi } : {}),
    };
}
export function useDownload() {
    const [restoredQueueTrackStatuses] = useState(() => getQueueTrackStatusSets());
    const [isDownloading, setIsDownloading] = useState(false);
    const [downloadingTrack, setDownloadingTrack] = useState<string | null>(null);
    const [downloadedTracks, setDownloadedTracks] = useState<Set<string>>(() => new Set(restoredQueueTrackStatuses.downloadedTracks));
    const [failedTracks, setFailedTracks] = useState<Set<string>>(() => new Set(restoredQueueTrackStatuses.failedTracks));
    const [skippedTracks, setSkippedTracks] = useState<Set<string>>(() => new Set(restoredQueueTrackStatuses.skippedTracks));
    const resetBatchDownloadState = () => {
        setDownloadingTrack(null);
        setIsDownloading(false);
        if (downloadExecution.activeKind() === "direct")
            downloadExecution.clearSignals("direct");
    };
    const runDownload = async (request: Parameters<typeof downloadTrack>[0]): Promise<DownloadResponse> => {
        if (downloadExecution.isStopRequested()) {
            return { success: false, cancelled: true, error: "Download cancelled", message: "Download cancelled" };
        }
        const response = await downloadTrack(request);
        if (response.cancelled || downloadExecution.isStopRequested()) {
            return { ...response, success: false, cancelled: true, error: response.error || "Download cancelled" };
        }
        return response;
    };
    const downloadWithAutoFallback = async (id: string, settings: Settings, trackName?: string, artistName?: string, albumName?: string, playlistName?: string, position?: number, spotifyId?: string, durationMs?: number, releaseYear?: string, albumArtist?: string, releaseDate?: string, coverUrl?: string, spotifyTrackNumber?: number, spotifyDiscNumber?: number, spotifyTotalTracks?: number, spotifyTotalDiscs?: number, copyright?: string, publisher?: string): Promise<DownloadResponse> => {
        const service = settings.downloader;
        const os = settings.operatingSystem;
        const customTidalApi = typeof settings.customTidalApi === "string" && settings.customTidalApi.trim().startsWith("https://")
            ? settings.customTidalApi.trim().replace(/\/+$/g, "")
            : undefined;
        const customQobuzApi = typeof settings.customQobuzApi === "string" && settings.customQobuzApi.trim().startsWith("https://")
            ? settings.customQobuzApi.trim().replace(/\/+$/g, "")
            : undefined;
        let outputDir = settings.downloadPath;
        let useAlbumTrackNumber = false;
        const placeholder = "__SLASH_PLACEHOLDER__";
        let finalReleaseDate = releaseDate;
        let finalTrackNumber = spotifyTrackNumber || 0;
        let finalAlbumType = "";
        let finalUPC = "";
        if (spotifyId) {
            try {
                const trackURL = `https://open.spotify.com/track/${spotifyId}`;
                const trackMetadata = await fetchSpotifyMetadata(trackURL, false, 0, 10, settings.separator);
                if ("track" in trackMetadata && trackMetadata.track) {
                    if (trackMetadata.track.artists) {
                        artistName = trackMetadata.track.artists;
                    }
                    if (trackMetadata.track.album_artist) {
                        albumArtist = trackMetadata.track.album_artist;
                    }
                    if (trackMetadata.track.release_date) {
                        finalReleaseDate = trackMetadata.track.release_date;
                    }
                    if (trackMetadata.track.track_number > 0) {
                        finalTrackNumber = trackMetadata.track.track_number;
                    }
                    if (trackMetadata.track.album_type) {
                        finalAlbumType = trackMetadata.track.album_type;
                    }
                    if (trackMetadata.track.upc) {
                        finalUPC = trackMetadata.track.upc;
                    }
                }
            }
            catch (err) {
                logger.debug(`spotify metadata lookup failed: ${err}`);
            }
        }
        const query = trackName && artistName ? `${trackName} ${artistName} ` : undefined;
        const yearValue = releaseYear || finalReleaseDate?.substring(0, 4);
        const hasSubfolder = Boolean(settings.folderTemplate && settings.folderTemplate.trim() !== "" && settings.applyFolderToSingleTrack);
        const trackNumberForTemplate = (hasSubfolder && finalTrackNumber > 0) ? finalTrackNumber : (position || 0);
        if ((hasSubfolder || templateUsesAlbumTrackNumber(settings)) && finalTrackNumber > 0) {
            useAlbumTrackNumber = true;
        }
        const displayArtist = settings.useFirstArtistOnly && artistName
            ? getFirstArtist(artistName)
            : artistName;
        const displayAlbumArtist = settings.useFirstArtistOnly && albumArtist
            ? getFirstArtist(albumArtist)
            : albumArtist;
        const namingArtist = artistName ? getFirstArtist(artistName) : artistName;
        const namingAlbumArtist = albumArtist ? getFirstArtist(albumArtist) : albumArtist;
        const resolvedTemplateISRC = await resolveTemplateISRC(settings, spotifyId || id);
        const templateData: TemplateData = {
            artist: namingArtist?.replace(/\//g, placeholder),
            artists: artistName?.replace(/\//g, placeholder),
            album: albumName?.replace(/\//g, placeholder),
            album_artist: namingAlbumArtist?.replace(/\//g, placeholder) || namingArtist?.replace(/\//g, placeholder),
            title: trackName?.replace(/\//g, placeholder),
            isrc: resolvedTemplateISRC?.replace(/\//g, placeholder),
            track: trackNumberForTemplate,
            total_tracks: spotifyTotalTracks,
            total_discs: spotifyTotalDiscs,
            year: yearValue,
            date: releaseDate,
            playlist: playlistName?.replace(/\//g, placeholder),
        };
        const folderTemplate = settings.folderTemplate || "";
        const useAlbumSubfolder = folderTemplate.includes("{album}") || folderTemplate.includes("{album_artist}") || folderTemplate.includes("{playlist}");
        if (settings.createPlaylistFolder && playlistName && !useAlbumSubfolder) {
            outputDir = joinPath(os, outputDir, sanitizePath(playlistName.replace(/\//g, " "), os));
        }
        if (settings.folderTemplate && settings.applyFolderToSingleTrack) {
            const folderPath = parseTemplate(settings.folderTemplate, templateData);
            if (folderPath) {
                const parts = folderPath.split("/").filter((p: string) => p.trim());
                for (const part of parts) {
                    const sanitizedPart = part.replace(new RegExp(placeholder, "g"), " ");
                    outputDir = joinPath(os, outputDir, sanitizePath(sanitizedPart, os));
                }
            }
        }
        const serviceForCheck = getExpectedAudioFormat(settings);
        let fileExists = false;
        if (trackName && artistName) {
            try {
                const checkRequest: CheckFileExistenceRequest = {
                    spotify_id: spotifyId || id,
                    track_name: trackName,
                    artist_name: displayArtist || "",
                    artists: artistName || "",
                    album_name: albumName,
                    album_artist: displayAlbumArtist,
                    album_artists: albumArtist,
                    release_date: finalReleaseDate || releaseDate,
                    isrc: resolvedTemplateISRC || undefined,
                    track_number: finalTrackNumber || spotifyTrackNumber || 0,
                    disc_number: spotifyDiscNumber || 0,
                    position: trackNumberForTemplate,
                    use_album_track_number: useAlbumTrackNumber,
                    filename_format: settings.filenameTemplate || "",
                    include_track_number: settings.trackNumber || false,
                    audio_format: serviceForCheck,
                    duration: expectedTrackDurationSeconds(durationMs),
                };
                const existenceResults = await CheckFilesExistence(outputDir, settings.downloadPath, [checkRequest]);
                if (existenceResults.length > 0 && existenceResults[0].exists) {
                    fileExists = true;
                    return {
                        success: true,
                        message: t("translation.downloadQueue.fileAlreadyExists"),
                        file: existenceResults[0].file_path || "",
                        already_exists: true,
                    };
                }
            }
            catch (err) {
                console.warn("File existence check failed:", err);
            }
        }
        const { AddToDownloadQueue } = await import("../../wailsjs/go/main/App");
        let itemID: string | undefined;
        if (!fileExists) {
            itemID = await AddToDownloadQueue(id, trackName || "", displayArtist || "", albumName || "");
        }
        if (service === "auto") {
            const order = extendAutoOrder(settings.autoOrder);
            let streamingURLs: StreamingLinks | null = null;
            if (spotifyId && shouldFetchStreamingURLs(order)) {
                try {
                    const { GetStreamingURLs } = await import("../../wailsjs/go/main/App");
                    const urlsJson = await GetStreamingURLs(spotifyId, "");
                    streamingURLs = readStreamingLinks(urlsJson);
                }
                catch (err) {
                    console.error("Failed to get streaming URLs:", err);
                }
            }
            const durationSeconds = durationMs ? Math.round(durationMs / 1000) : undefined;
            let lastResponse: DownloadResponse = { success: false, message: "", error: t("translation.backend.noMatchingServicesFound") };
            const fallbackErrors: string[] = [];
            const tidalQuality = getTidalAudioFormat(settings, "auto");
            const isAtmos = settings.autoQuality === "atmos";
            const is24Bit = (settings.autoQuality || "24") === "24";
            const qobuzQuality = is24Bit ? "27" : "6";
            for (const s of order) {
                if (s === "tidal") {
                    try {
                        logger.debug(`trying Tidal for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "tidal",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            service_url: streamingURLs?.tidal_url,
                            duration: durationSeconds,
                            item_id: itemID,
                            audio_format: tidalQuality,
                            ...getCustomInstanceFields(customTidalApi),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`Tidal: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Tidal] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`Tidal failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`Tidal error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Tidal] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "amazon") {
                    try {
                        logger.debug(`trying amazon for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "amazon",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            service_url: streamingURLs?.amazon_url,
                            item_id: itemID,
                            audio_format: isAtmos ? "atmos" : (is24Bit ? "24" : "16"),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`amazon: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Amazon] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`amazon failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`amazon error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Amazon] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "qobuz") {
                    try {
                        logger.debug(`trying qobuz for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "qobuz",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            item_id: itemID,
                            audio_format: qobuzQuality,
                            ...getCustomInstanceFields(undefined, customQobuzApi),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`qobuz: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Qobuz] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`qobuz failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`qobuz error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Qobuz] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "deezer" || s === "apple" || s === "jiosaavn") {
                    try {
                        logger.debug(`trying ${s} for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: s,
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            item_id: itemID,
                            audio_format: "flac",
                            duration: durationSeconds,
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`${s}: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[${s}] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`${s} failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`${s} error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[${s}] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
            }
            if (itemID) {
                const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
                const finalError = fallbackErrors.length > 0 ? fallbackErrors.join(" | ") : (lastResponse.error || "All services failed");
                await MarkDownloadItemFailed(itemID, finalError);
            }
            return lastResponse;
        }
        const durationSecondsForFallback = durationMs ? Math.round(durationMs / 1000) : undefined;
        let audioFormat: string | undefined;
        if (service === "tidal") {
            audioFormat = getTidalAudioFormat(settings, "single");
        }
        else if (service === "qobuz") {
            audioFormat = settings.qobuzQuality || "6";
        }
        else if (service === "amazon") {
            audioFormat = settings.amazonQuality || "16";
        }
        else if (service === "deezer" || service === "apple" || service === "jiosaavn") {
            audioFormat = "flac";
        }
        logger.debug(`trying ${service} for: ${trackName} - ${artistName}`);
        const singleServiceResponse = await runDownload({
            service: service as "tidal" | "qobuz" | "amazon" | "deezer" | "apple" | "jiosaavn",
            query,
            track_name: trackName,
            artist_name: displayArtist,
            album_name: albumName,
            album_artist: displayAlbumArtist,
            release_date: finalReleaseDate || releaseDate,
            cover_url: coverUrl,
            output_dir: outputDir,
            filename_format: settings.filenameTemplate,
            artists: artistName,
            category: getAlbumCategoryLabel(finalAlbumType),
            upc: finalUPC,
            track_number: settings.trackNumber,
            position: trackNumberForTemplate,
            use_album_track_number: useAlbumTrackNumber,
            spotify_id: spotifyId,
            embed_lyrics: settings.embedLyrics,
            embed_max_quality_cover: settings.embedMaxQualityCover,
            duration: durationSecondsForFallback,
            item_id: itemID,
            audio_format: audioFormat,
            ...getCustomInstanceFields(service === "tidal" ? customTidalApi : undefined, service === "qobuz" ? customQobuzApi : undefined),
            spotify_track_number: spotifyTrackNumber,
            spotify_disc_number: spotifyDiscNumber,
            spotify_total_tracks: spotifyTotalTracks,
            spotify_total_discs: spotifyTotalDiscs,
            isrc: resolvedTemplateISRC || undefined,
            copyright: copyright,
            publisher: publisher,
            use_first_artist_only: settings.useFirstArtistOnly,
            use_single_genre: settings.useSingleGenre,
            embed_genre: settings.embedGenre,
            save_cover: settings.saveCover,
        });
        if (!singleServiceResponse.success && !singleServiceResponse.cancelled && !isCooldownMessage(singleServiceResponse.error) && itemID) {
            const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
            await MarkDownloadItemFailed(itemID, singleServiceResponse.error || "Download failed");
        }
        return singleServiceResponse;
    };
    const downloadWithItemID = async (settings: Settings, itemID: string, trackName?: string, artistName?: string, albumName?: string, folderName?: string, position?: number, spotifyId?: string, durationMs?: number, isAlbum?: boolean, releaseYear?: string, albumArtist?: string, releaseDate?: string, coverUrl?: string, spotifyTrackNumber?: number, spotifyDiscNumber?: number, spotifyTotalTracks?: number, spotifyTotalDiscs?: number, copyright?: string, publisher?: string): Promise<DownloadResponse> => {
        settings = { ...settings, filenameTemplate: getEffectiveAlbumFilenameTemplate(settings) };
        const service = settings.downloader;
        const os = settings.operatingSystem;
        const customTidalApi = typeof settings.customTidalApi === "string" && settings.customTidalApi.trim().startsWith("https://")
            ? settings.customTidalApi.trim().replace(/\/+$/g, "")
            : undefined;
        const customQobuzApi = typeof settings.customQobuzApi === "string" && settings.customQobuzApi.trim().startsWith("https://")
            ? settings.customQobuzApi.trim().replace(/\/+$/g, "")
            : undefined;
        let outputDir = settings.downloadPath;
        let useAlbumTrackNumber = false;
        const placeholder = "__SLASH_PLACEHOLDER__";
        let finalReleaseDate = releaseDate;
        let finalTrackNumber = spotifyTrackNumber || 0;
        let finalAlbumType = "";
        let finalUPC = "";
        if (spotifyId) {
            try {
                const trackURL = `https://open.spotify.com/track/${spotifyId}`;
                const trackMetadata = await fetchSpotifyMetadata(trackURL, false, 0, 10, settings.separator);
                if ("track" in trackMetadata && trackMetadata.track) {
                    if (trackMetadata.track.artists) {
                        artistName = trackMetadata.track.artists;
                    }
                    if (trackMetadata.track.album_artist) {
                        albumArtist = trackMetadata.track.album_artist;
                    }
                    if (trackMetadata.track.release_date) {
                        finalReleaseDate = trackMetadata.track.release_date;
                    }
                    if (trackMetadata.track.track_number > 0) {
                        finalTrackNumber = trackMetadata.track.track_number;
                    }
                    if (trackMetadata.track.album_type) {
                        finalAlbumType = trackMetadata.track.album_type;
                    }
                    if (trackMetadata.track.upc) {
                        finalUPC = trackMetadata.track.upc;
                    }
                }
            }
            catch (err) {
                logger.debug(`spotify metadata lookup failed: ${err}`);
            }
        }
        const query = trackName && artistName ? `${trackName} ${artistName}` : undefined;
        const yearValue = releaseYear || finalReleaseDate?.substring(0, 4);
        const hasSubfolder = Boolean(settings.folderTemplate && settings.folderTemplate.trim() !== "");
        const trackNumberForTemplate = (hasSubfolder && finalTrackNumber > 0) ? finalTrackNumber : (position || 0);
        if ((hasSubfolder || templateUsesAlbumTrackNumber(settings)) && finalTrackNumber > 0) {
            useAlbumTrackNumber = true;
        }
        const displayArtist = settings.useFirstArtistOnly && artistName
            ? getFirstArtist(artistName)
            : artistName;
        const displayAlbumArtist = settings.useFirstArtistOnly && albumArtist
            ? getFirstArtist(albumArtist)
            : albumArtist;
        const namingArtist = artistName ? getFirstArtist(artistName) : artistName;
        const namingAlbumArtist = albumArtist ? getFirstArtist(albumArtist) : albumArtist;
        const resolvedTemplateISRC = await resolveTemplateISRC(settings, spotifyId);
        const templateData: TemplateData = {
            artist: namingArtist?.replace(/\//g, placeholder),
            artists: artistName?.replace(/\//g, placeholder),
            album: albumName?.replace(/\//g, placeholder),
            album_artist: namingAlbumArtist?.replace(/\//g, placeholder) || namingArtist?.replace(/\//g, placeholder),
            title: trackName?.replace(/\//g, placeholder),
            isrc: resolvedTemplateISRC?.replace(/\//g, placeholder),
            track: trackNumberForTemplate,
            total_tracks: spotifyTotalTracks,
            total_discs: spotifyTotalDiscs,
            year: yearValue,
            date: releaseDate,
            playlist: folderName?.replace(/\//g, placeholder),
        };
        const folderTemplate = settings.folderTemplate || "";
        const useAlbumSubfolder = folderTemplate.includes("{album}") || folderTemplate.includes("{album_artist}") || folderTemplate.includes("{playlist}");
        if (settings.createPlaylistFolder && folderName && (!isAlbum || !useAlbumSubfolder)) {
            outputDir = joinPath(os, outputDir, sanitizePath(folderName.replace(/\//g, " "), os));
        }
        if (settings.folderTemplate) {
            const folderPath = parseTemplate(settings.folderTemplate, templateData);
            if (folderPath) {
                const parts = folderPath.split("/").filter(p => p.trim());
                for (const part of parts) {
                    const sanitizedPart = part.replace(new RegExp(placeholder, "g"), " ");
                    outputDir = joinPath(os, outputDir, sanitizePath(sanitizedPart, os));
                }
            }
        }
        if (service === "auto") {
            const order = extendAutoOrder(settings.autoOrder);
            let streamingURLs: StreamingLinks | null = null;
            if (spotifyId && shouldFetchStreamingURLs(order)) {
                try {
                    const { GetStreamingURLs } = await import("../../wailsjs/go/main/App");
                    const urlsJson = await GetStreamingURLs(spotifyId, "");
                    streamingURLs = readStreamingLinks(urlsJson);
                }
                catch (err) {
                    console.error("Failed to get streaming URLs:", err);
                }
            }
            const durationSeconds = durationMs ? Math.round(durationMs / 1000) : undefined;
            let lastResponse: DownloadResponse = { success: false, message: "", error: t("translation.backend.noMatchingServicesFound") };
            const fallbackErrors: string[] = [];
            const tidalQuality = getTidalAudioFormat(settings, "auto");
            const isAtmos = settings.autoQuality === "atmos";
            const is24Bit = (settings.autoQuality || "24") === "24";
            const qobuzQuality = is24Bit ? "27" : "6";
            for (const s of order) {
                if (s === "tidal") {
                    try {
                        logger.debug(`trying Tidal for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "tidal",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            service_url: streamingURLs?.tidal_url,
                            duration: durationSeconds,
                            item_id: itemID,
                            audio_format: tidalQuality,
                            ...getCustomInstanceFields(customTidalApi),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`Tidal: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Tidal] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`Tidal failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`Tidal error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Tidal] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "amazon") {
                    try {
                        logger.debug(`trying amazon for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "amazon",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            service_url: streamingURLs?.amazon_url,
                            item_id: itemID,
                            audio_format: isAtmos ? "atmos" : (is24Bit ? "24" : "16"),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`amazon: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Amazon] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`amazon failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`amazon error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Amazon] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "qobuz") {
                    try {
                        logger.debug(`trying qobuz for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: "qobuz",
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            duration: durationSeconds,
                            item_id: itemID,
                            audio_format: qobuzQuality,
                            ...getCustomInstanceFields(undefined, customQobuzApi),
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`qobuz: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[Qobuz] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`qobuz failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`qobuz error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[Qobuz] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
                else if (s === "deezer" || s === "apple" || s === "jiosaavn") {
                    try {
                        logger.debug(`trying ${s} for: ${trackName} - ${artistName}`);
                        const response = await runDownload({
                            service: s,
                            query,
                            track_name: trackName,
                            artist_name: displayArtist,
                            album_name: albumName,
                            album_artist: displayAlbumArtist,
                            release_date: finalReleaseDate || releaseDate,
                            cover_url: coverUrl,
                            output_dir: outputDir,
                            filename_format: settings.filenameTemplate,
                            artists: artistName,
                            category: getAlbumCategoryLabel(finalAlbumType),
                            upc: finalUPC,
                            track_number: settings.trackNumber,
                            position: trackNumberForTemplate,
                            use_album_track_number: useAlbumTrackNumber,
                            spotify_id: spotifyId,
                            embed_lyrics: settings.embedLyrics,
                            embed_max_quality_cover: settings.embedMaxQualityCover,
                            duration: durationSeconds,
                            item_id: itemID,
                            audio_format: "flac",
                            spotify_track_number: spotifyTrackNumber,
                            spotify_disc_number: spotifyDiscNumber,
                            spotify_total_tracks: spotifyTotalTracks,
                            spotify_total_discs: spotifyTotalDiscs,
                            isrc: resolvedTemplateISRC || undefined,
                            copyright: copyright,
                            publisher: publisher,
                            use_first_artist_only: settings.useFirstArtistOnly,
                            use_single_genre: settings.useSingleGenre,
                            embed_genre: settings.embedGenre,
                            save_cover: settings.saveCover,
                        });
                        if (response.success) {
                            logger.success(`${s}: ${trackName} - ${artistName}${formatSourceSuffix(response)}`);
                            return response;
                        }
                        if (response.cancelled)
                            return response;
                        const errMsg = response.error || response.message || "Failed";
                        if (isCooldownMessage(errMsg))
                            return response;
                        fallbackErrors.push(`[${s}] ${translateMessage(errMsg)}`);
                        lastResponse = response;
                        logger.warning(`${s} failed, trying next...`);
                    }
                    catch (err) {
                        logger.error(`${s} error: ${err}`);
                        if (downloadExecution.isStopRequested() || isCancelFailure(err))
                            return cancelledDownloadResponse(err);
                        const cooldownFailure = getCooldownFailure(err);
                        if (cooldownFailure)
                            return cooldownFailure;
                        fallbackErrors.push(`[${s}] ${String(err)}`);
                        lastResponse = { success: false, message: "", error: String(err) };
                    }
                }
            }
            if (!lastResponse.success && itemID) {
                const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
                const finalError = fallbackErrors.length > 0 ? fallbackErrors.join(" | ") : (lastResponse.error || "All services failed");
                await MarkDownloadItemFailed(itemID, finalError);
            }
            return lastResponse;
        }
        const durationSecondsForFallback = durationMs ? Math.round(durationMs / 1000) : undefined;
        let audioFormat: string | undefined;
        if (service === "tidal") {
            audioFormat = getTidalAudioFormat(settings, "single");
        }
        else if (service === "qobuz") {
            audioFormat = settings.qobuzQuality || "6";
        }
        else if (service === "amazon") {
            audioFormat = settings.amazonQuality || "16";
        }
        else if (service === "deezer" || service === "apple" || service === "jiosaavn") {
            audioFormat = "flac";
        }
        const singleServiceResponse = await runDownload({
            service: service as "tidal" | "qobuz" | "amazon" | "deezer" | "apple" | "jiosaavn",
            query,
            track_name: trackName,
            artist_name: displayArtist,
            album_name: albumName,
            album_artist: displayAlbumArtist,
            release_date: finalReleaseDate || releaseDate,
            cover_url: coverUrl,
            output_dir: outputDir,
            filename_format: settings.filenameTemplate,
            artists: artistName,
            category: getAlbumCategoryLabel(finalAlbumType),
            upc: finalUPC,
            track_number: settings.trackNumber,
            position: trackNumberForTemplate,
            use_album_track_number: useAlbumTrackNumber,
            spotify_id: spotifyId,
            embed_lyrics: settings.embedLyrics,
            embed_max_quality_cover: settings.embedMaxQualityCover,
            duration: durationSecondsForFallback,
            item_id: itemID,
            audio_format: audioFormat,
            ...getCustomInstanceFields(service === "tidal" ? customTidalApi : undefined, service === "qobuz" ? customQobuzApi : undefined),
            spotify_track_number: spotifyTrackNumber,
            spotify_disc_number: spotifyDiscNumber,
            spotify_total_tracks: spotifyTotalTracks,
            spotify_total_discs: spotifyTotalDiscs,
            isrc: resolvedTemplateISRC || undefined,
            copyright: copyright,
            publisher: publisher,
            use_first_artist_only: settings.useFirstArtistOnly,
            use_single_genre: settings.useSingleGenre,
            embed_genre: settings.embedGenre,
            save_cover: settings.saveCover,
        });
        if (!singleServiceResponse.success && !singleServiceResponse.cancelled && !isCooldownMessage(singleServiceResponse.error) && itemID) {
            const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
            await MarkDownloadItemFailed(itemID, singleServiceResponse.error || "Download failed");
        }
        return singleServiceResponse;
    };
    const handleDownloadTrack = async (id: string, trackName?: string, artistName?: string, albumName?: string, spotifyId?: string, playlistName?: string, durationMs?: number, position?: number, albumArtist?: string, releaseDate?: string, coverUrl?: string, spotifyTrackNumber?: number, spotifyDiscNumber?: number, spotifyTotalTracks?: number, spotifyTotalDiscs?: number, copyright?: string, publisher?: string, queueItemId?: string): Promise<QueueAttemptDisposition | undefined> => {
        if (!id) {
            toast.error(t("translation.download.noIdFoundTrack"));
            return;
        }
        const directLease = queueItemId ? null : await downloadExecution.acquireWhenUnblocked("direct");
        try {
        const settings = getSettings();
        const displayArtist = artistName;
        const directQueueItemId = queueItemId || beginDirectTrackQueueItem({
            spotify_id: spotifyId || id, name: trackName || id, artists: artistName || "", album_name: albumName || "",
            album_artist: albumArtist, duration_ms: durationMs || 0, images: coverUrl || "", release_date: releaseDate || "",
            track_number: spotifyTrackNumber || position || 0, disc_number: spotifyDiscNumber,
            total_tracks: spotifyTotalTracks, total_discs: spotifyTotalDiscs, external_urls: "", copyright, publisher,
        }, { folderName: playlistName, startPosition: position });
        const finishDirectTrack = (status: QueueTrackStatus) => {
            if (!queueItemId)
                finishDirectQueueItem(directQueueItemId, {
                    trackResults: { [spotifyId || id]: status }, successCount: status === "done" ? 1 : 0,
                    skippedCount: status === "skipped" ? 1 : 0, failedCount: status === "failed" ? 1 : 0,
                });
            return status;
        };
        logger.info(`starting download: ${trackName} - ${displayArtist}`);
        setDownloadingTrack(id);
        try {
            const releaseYear = releaseDate?.substring(0, 4);
            const response = await downloadWithAutoFallback(id, settings, trackName, artistName, albumName, playlistName, position, spotifyId, durationMs, releaseYear, albumArtist || "", releaseDate, coverUrl, spotifyTrackNumber, spotifyDiscNumber, spotifyTotalTracks, spotifyTotalDiscs, copyright, publisher);
            if (response.success) {
                if (settings.autoReplayGainTags && !response.already_exists && response.file) {
                    await applyAutomaticReplayGain([response.file], false);
                }
                if (response.already_exists) {
                    toast.info(translateMessage(response.message));
                    setSkippedTracks((prev) => new Set(prev).add(id));
                }
                else {
                    toast.success(translateMessage(response.message));
                }
                setDownloadedTracks((prev) => new Set(prev).add(id));
                setFailedTracks((prev) => {
                    const newSet = new Set(prev);
                    newSet.delete(id);
                    return newSet;
                });
                return finishDirectTrack(response.already_exists ? "skipped" : "done");
            }
            else if (response.cancelled) {
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, cancelled: true });
                return "cancelled";
            }
            else if (isCooldownMessage(response.error)) {
                toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                downloadExecution.pauseActive();
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, paused: true });
                return "paused";
            }
            else {
                toast.error(translateMessage(response.error || t("translation.download.downloadFailed")));
                setFailedTracks((prev) => new Set(prev).add(id));
                return finishDirectTrack("failed");
            }
        }
        catch (err) {
            const message = err instanceof Error ? err.message : t("translation.download.downloadFailed");
            if (isCooldownMessage(message)) {
                toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                downloadExecution.pauseActive();
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, { trackResults: {}, successCount: 0, skippedCount: 0, failedCount: 0, paused: true });
                return "paused";
            }
            toast.error(translateMessage(message));
            setFailedTracks((prev) => new Set(prev).add(id));
            return finishDirectTrack("failed");
        }
        finally {
            setDownloadingTrack(null);
            const activeKind = downloadExecution.activeKind();
            if (activeKind)
                downloadExecution.clearStop(activeKind);
        }
        }
        finally {
            if (directLease)
                downloadExecution.release(directLease);
        }
    };
    const handleDownloadSelected = async (selectedTracks: string[], allTracks: TrackMetadata[], folderName?: string, isAlbum?: boolean, batchSource: BatchDownloadSource = "collection", queueItemId?: string) => {
        if (selectedTracks.length === 0) {
            toast.error(t("translation.download.noTracksSelected"));
            return;
        }
        const directLease = queueItemId ? null : await downloadExecution.acquireWhenUnblocked("direct");
        try {
        const settings = getSettings();
        const selectedTrackCandidates = selectedTracks
            .map((id) => allTracks.find((t) => t.spotify_id === id))
            .filter((t): t is TrackMetadata => t !== undefined);
        const selectedTrackObjects = settings.redownloadWithSuffix
            ? selectedTrackCandidates
            : deduplicateTracksBySpotifyID(selectedTrackCandidates);
        const directQueueItemId = queueItemId || beginDirectCollectionQueueItem({
            type: queueCollectionType(isAlbum ? "album" : batchSource, selectedTrackObjects), name: folderName || selectedTrackObjects[0]?.album_name || t("translation.queue.queue"),
            artist: selectedTrackObjects[0]?.album_artist || selectedTrackObjects[0]?.artists || "",
            info: `${selectedTrackObjects.length} ${t("translation.common.tracks")}`, image: selectedTrackObjects[0]?.images || "",
            folderName, isAlbum, tracks: selectedTrackObjects,
        });
        logger.info(`starting batch download: ${selectedTrackObjects.length} selected tracks`);
        downloadExecution.clearStop(queueItemId ? "queue" : "direct");
        setIsDownloading(true);
        try {
            let outputDir = settings.downloadPath;
            const os = settings.operatingSystem;
            const useAlbumTag = settings.folderTemplate?.includes("{album}");
            if (settings.createPlaylistFolder && folderName && (!isAlbum || !useAlbumTag)) {
                outputDir = joinPath(os, outputDir, sanitizePath(folderName.replace(/\//g, " "), os));
            }
            logger.info(`checking existing files in parallel...`);
            const useAlbumTrackNumber = templateUsesAlbumTrackNumber(settings);
            const albumFilenameTemplate = getEffectiveAlbumFilenameTemplate(settings);
            const audioFormat = getExpectedAudioFormat(settings);
            const existenceChecks = selectedTrackObjects.map((track, index) => {
                const displayArtist = track.artists ? getFirstArtist(track.artists) : track.artists;
                const displayAlbumArtist = track.album_artist ? getFirstArtist(track.album_artist) : track.album_artist;
                return {
                    spotify_id: track.spotify_id || "",
                    track_name: track.name || "",
                    artist_name: displayArtist || "",
                    artists: track.artists || "",
                    album_name: track.album_name || "",
                    album_artist: displayAlbumArtist || "",
                    album_artists: track.album_artist || "",
                    category: getAlbumCategoryLabel(track.album_type),
                    upc: track.upc || "",
                    release_date: track.release_date || "",
                    isrc: track.isrc || undefined,
                    track_number: track.track_number || 0,
                    disc_number: track.disc_number || 0,
                    total_tracks: track.total_tracks || 0,
                    total_discs: track.total_discs || 0,
                    position: index + 1,
                    use_album_track_number: useAlbumTrackNumber,
                    filename_format: albumFilenameTemplate || "",
                    include_track_number: settings.trackNumber || false,
                    audio_format: audioFormat,
                    duration: expectedTrackDurationSeconds(track.duration_ms),
                };
            });
            const existenceResults = await CheckFilesExistence(outputDir, settings.downloadPath, existenceChecks);
            const existingSpotifyIDs = new Set<string>();
            const existingFilePaths = new Map<string, string>();
            const finalFilePaths = new Map<string, string>();
            for (const result of existenceResults) {
                if (result.exists) {
                    existingSpotifyIDs.add(result.spotify_id);
                    existingFilePaths.set(result.spotify_id, result.file_path || "");
                    finalFilePaths.set(result.spotify_id, result.file_path || "");
                }
            }
            logger.info(`found ${existingSpotifyIDs.size} existing files`);
            const { AddToDownloadQueue } = await import("../../wailsjs/go/main/App");
            const itemIDs: string[] = [];
            for (const track of selectedTrackObjects) {
                const trackID = track.spotify_id || "";
                const displayArtist = track.artists;
                const itemID = await AddToDownloadQueue(trackID, track.name || "", displayArtist || "", track.album_name || "");
                itemIDs.push(itemID);
                if (existingSpotifyIDs.has(trackID)) {
                    const filePath = existingFilePaths.get(trackID) || "";
                    setTimeout(() => SkipDownloadItem(itemID, filePath), 10);
                    setSkippedTracks((prev) => new Set(prev).add(trackID));
                    setDownloadedTracks((prev) => new Set(prev).add(trackID));
                }
            }
            const tracksToDownload = selectedTrackObjects
                .map((track, originalIndex) => ({ track, originalIndex }))
                .filter(({ track }) => !existingSpotifyIDs.has(track.spotify_id || ""));
            let successCount = 0;
            let errorCount = 0;
            let skippedCount = existingSpotifyIDs.size;
            const failedErrorMessages = new Map<string, string>();
            const completedSpotifyIDs = new Set<string>();
            for (let i = 0; i < tracksToDownload.length; i++) {
                if (downloadExecution.isStopRequested() || downloadExecution.isPauseRequested()) {
                    if (downloadExecution.isStopRequested()) {
                        toast.info(t("translation.download.stopped", { count: successCount, remaining: tracksToDownload.length - i }));
                    }
                    break;
                }
                const { track, originalIndex } = tracksToDownload[i];
                const id = track.spotify_id || "";
                const itemID = itemIDs[originalIndex];
                setDownloadingTrack(id);
                const displayArtist = track.artists;
                try {
                    const releaseYear = track.release_date?.substring(0, 4);
                    const response = await downloadWithItemID(settings, itemID, track.name, track.artists, track.album_name, folderName, originalIndex + 1, track.spotify_id, track.duration_ms, isAlbum, releaseYear, track.album_artist || "", track.release_date, track.images, track.track_number, track.disc_number, track.total_tracks, track.total_discs, track.copyright, track.publisher);
                    if (response.cancelled || downloadExecution.isStopRequested()) {
                        toast.info(t("translation.download.stopped", { count: successCount, remaining: tracksToDownload.length - i }));
                        break;
                    }
                    if (response.success) {
                        if (response.already_exists) {
                            skippedCount++;
                            logger.info(`skipped: ${track.name} - ${displayArtist} (already exists)`);
                            setSkippedTracks((prev) => new Set(prev).add(id));
                        }
                        else {
                            successCount++;
                            completedSpotifyIDs.add(id);
                            logger.success(`downloaded: ${track.name} - ${displayArtist}${formatSourceSuffix(response)}`);
                        }
                        if (response.file) {
                            finalFilePaths.set(id, response.file);
                            finalFilePaths.set(track.spotify_id || id, response.file);
                        }
                        setDownloadedTracks((prev) => new Set(prev).add(id));
                        setFailedTracks((prev) => {
                            const newSet = new Set(prev);
                            newSet.delete(id);
                            return newSet;
                        });
                    }
                    else if (isCooldownMessage(response.error)) {
                        const remaining = tracksToDownload.length - i - 1;
                        toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                        logger.info(`cooldown detected, pausing queue with ${remaining} track(s) remaining`);
                        downloadExecution.pauseActive();
                        break;
                    }
                    else {
                        errorCount++;
                        logger.error(`failed: ${track.name} - ${displayArtist}`);
                        failedErrorMessages.set(id, translateMessage(response.error || t("translation.download.downloadFailed")));
                        setFailedTracks((prev) => new Set(prev).add(id));
                    }
                }
                catch (err) {
                    const message = err instanceof Error ? err.message : String(err);
                    if (isCooldownMessage(message)) {
                        const remaining = tracksToDownload.length - i - 1;
                        toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                        logger.info(`cooldown detected, pausing queue with ${remaining} track(s) remaining`);
                        downloadExecution.pauseActive();
                        break;
                    }
                    errorCount++;
                    logger.error(`error: ${track.name} - ${err}`);
                    failedErrorMessages.set(id, translateMessage(message));
                    setFailedTracks((prev) => new Set(prev).add(id));
                    if (itemID) {
                        const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
                        await MarkDownloadItemFailed(itemID, message);
                    }
                }
            }
            const wasStopped = downloadExecution.isStopRequested();
            const wasPaused = downloadExecution.isPauseRequested() && !wasStopped;
            if (wasStopped) {
                try {
                    const { CancelAllQueuedItems } = await import("../../wailsjs/go/main/App");
                    await CancelAllQueuedItems();
                }
                catch (err) {
                    logger.error(`failed to clear queued download items: ${err}`);
                }
            }
            if (wasPaused) {
                logger.info(`batch paused: ${successCount} downloaded, ${skippedCount} skipped, ${errorCount} failed`);
                const result = buildQueueExecutionResult(selectedTrackObjects, existingSpotifyIDs, failedErrorMessages, completedSpotifyIDs, false, true);
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, result);
                return result;
            }
            if (settings.autoReplayGainTags && completedSpotifyIDs.size > 0) {
                const albumPaths = !wasStopped && errorCount === 0 && isAlbum && settings.autoReplayGainMode === "album"
                    ? getCompleteAlbumPaths(selectedTrackObjects, finalFilePaths, completedSpotifyIDs)
                    : null;
                const trackPaths = getCompletedTrackPaths(selectedTrackObjects, finalFilePaths, completedSpotifyIDs);
                await applyAutomaticReplayGain(albumPaths || trackPaths, albumPaths !== null);
            }
            if (settings.createM3u8File && folderName) {
                const paths = selectedTrackObjects.map((t) => finalFilePaths.get(t.spotify_id || "") || "").filter((p) => p !== "");
                if (paths.length > 0) {
                    try {
                        logger.info(`creating m3u8 playlist: ${folderName}`);
                        await CreateM3U8File(folderName, outputDir, paths);
                        toast.success(t("translation.download.m3u8PlaylistCreated"));
                    }
                    catch (err) {
                        logger.error(`failed to create m3u8 playlist: ${err}`);
                        toast.error(t("translation.migrated.useDownload.failedToCreateM3U8Playlist", { value1: err }));
                    }
                }
            }
            if (settings.exportLogsFile && folderName) {
                const logsToExport: string[] = [];
                logsToExport.push(`Download Report - ${new Date().toLocaleString()}`);
                logsToExport.push("-".repeat(50));
                logsToExport.push("");
                let failedCount = 0;
                selectedTrackObjects.forEach((t) => {
                    const spotifyID = t.spotify_id || "";
                    const errorMessage = failedErrorMessages.get(spotifyID);
                    const isFailed = !!errorMessage;
                    const isSkipped = existingSpotifyIDs.has(spotifyID);
                    const isSuccess = !!finalFilePaths.get(spotifyID);
                    const displayArtist = t.artists;
                    if (isFailed) {
                        failedCount++;
                        logsToExport.push(`${failedCount}. ${t.name} - ${displayArtist}${t.album_name ? ` (${t.album_name})` : ""}`);
                        logsToExport.push(`   Error: ${errorMessage}`);
                        if (spotifyID) {
                            logsToExport.push(`   ID: ${spotifyID}`);
                            logsToExport.push(`   URL: https://open.spotify.com/track/${spotifyID}`);
                        }
                        logsToExport.push("");
                    }
                    else if (!settings.exportLogsOnlyFailed) {
                        if (isSkipped) {
                            logsToExport.push(`[SKIPPED] ${t.name} - ${displayArtist}`);
                        }
                        else if (isSuccess) {
                            logsToExport.push(`[SUCCESS] ${t.name} - ${displayArtist}`);
                        }
                    }
                });
                if (failedCount > 0) {
                    try {
                        logger.info(`creating log file: ${folderName}`);
                        await CreateLogFile(folderName, outputDir, logsToExport);
                        toast.success(t("translation.download.downloadLogCreated"));
                    }
                    catch (err) {
                        logger.error(`failed to create log file: ${err}`);
                    }
                }
            }
            logger.info(`batch complete: ${successCount} downloaded, ${skippedCount} skipped, ${errorCount} failed`);
            if (errorCount === 0 && skippedCount === 0) {
                toast.success(t("translation.download.completed", { count: successCount }));
            }
            else if (errorCount === 0 && successCount === 0) {
                toast.info(t("translation.download.exists", { count: skippedCount }));
            }
            else if (errorCount === 0) {
                toast.info(t("translation.migrated.useDownload.downloadedSkipped", { value1: successCount, value2: skippedCount }));
            }
            else {
                toast.warning(t("translation.download.summary", { downloaded: successCount, skipped: skippedCount, failed: errorCount }));
            }
            const result = buildQueueExecutionResult(selectedTrackObjects, existingSpotifyIDs, failedErrorMessages, completedSpotifyIDs, wasStopped);
            if (!queueItemId)
                finishDirectQueueItem(directQueueItemId, result);
            return result;
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            logger.error(`batch download failed: ${message}`);
            toast.error(translateMessage(message));
            const result = buildFailedQueueExecutionResult(selectedTrackObjects, message, downloadExecution.isStopRequested());
            if (!queueItemId)
                finishDirectQueueItem(directQueueItemId, result);
            return result;
        }
        finally {
            resetBatchDownloadState();
        }
        }
        finally {
            if (directLease)
                downloadExecution.release(directLease);
        }
    };
    const handleDownloadAll = async (tracks: TrackMetadata[], folderName?: string, isAlbum?: boolean, batchSource: BatchDownloadSource = "collection", queueItemId?: string, resumeContext?: QueueResumeContext) => {
        const settings = getSettings();
        const candidateTracksWithID = tracks.filter((track) => track.spotify_id);
        if (candidateTracksWithID.length === 0) {
            toast.error(t("translation.download.noTracksAvailableDownload"));
            return;
        }
        const directLease = queueItemId ? null : await downloadExecution.acquireWhenUnblocked("direct");
        try {
        const tracksWithId = settings.redownloadWithSuffix
            ? candidateTracksWithID
            : deduplicateTracksBySpotifyID(candidateTracksWithID);
        const directQueueItemId = queueItemId || beginDirectCollectionQueueItem({
            type: queueCollectionType(isAlbum ? "album" : batchSource, tracksWithId), name: folderName || tracksWithId[0]?.album_name || t("translation.queue.queue"),
            artist: tracksWithId[0]?.album_artist || tracksWithId[0]?.artists || "",
            info: `${tracksWithId.length} ${t("translation.common.tracks")}`, image: tracksWithId[0]?.images || "",
            folderName, isAlbum, tracks: tracksWithId,
        });
        logger.info(`starting batch download: ${tracksWithId.length} tracks`);
        const collectionTracks = resumeContext?.allTracks?.length ? resumeContext.allTracks : candidateTracksWithID;
        const collectionTrackPositions = new Map<string, number>();
        collectionTracks.forEach((track, index) => {
            const spotifyID = track.spotify_id || "";
            if (spotifyID && !collectionTrackPositions.has(spotifyID))
                collectionTrackPositions.set(spotifyID, index + 1);
        });
        const queueTrackFilePaths = new Map<string, string>(Object.entries(resumeContext?.trackFilePaths || {}));
        downloadExecution.clearSignals(queueItemId ? "queue" : "direct");
        setIsDownloading(true);
        try {
            let outputDir = settings.downloadPath;
            const os = settings.operatingSystem;
            const useAlbumTag = settings.folderTemplate?.includes("{album}");
            if (settings.createPlaylistFolder && folderName && (!isAlbum || !useAlbumTag)) {
                outputDir = joinPath(os, outputDir, sanitizePath(folderName.replace(/\//g, " "), os));
            }
            logger.info(`checking existing files in parallel...`);
            const useAlbumTrackNumber = templateUsesAlbumTrackNumber(settings);
            const albumFilenameTemplate = getEffectiveAlbumFilenameTemplate(settings);
            const audioFormat = getExpectedAudioFormat(settings);
            const existenceChecks = tracksWithId.map((track, index) => {
                const displayArtist = track.artists ? getFirstArtist(track.artists) : track.artists;
                const displayAlbumArtist = track.album_artist ? getFirstArtist(track.album_artist) : track.album_artist;
                return {
                    spotify_id: track.spotify_id || "",
                    track_name: track.name || "",
                    artist_name: displayArtist || "",
                    artists: track.artists || "",
                    album_name: track.album_name || "",
                    album_artist: displayAlbumArtist || "",
                    album_artists: track.album_artist || "",
                    category: getAlbumCategoryLabel(track.album_type),
                    upc: track.upc || "",
                    release_date: track.release_date || "",
                    isrc: track.isrc || undefined,
                    track_number: track.track_number || 0,
                    disc_number: track.disc_number || 0,
                    total_tracks: track.total_tracks || 0,
                    total_discs: track.total_discs || 0,
                    position: collectionTrackPositions.get(track.spotify_id || "") || index + 1,
                    use_album_track_number: useAlbumTrackNumber,
                    filename_format: albumFilenameTemplate || "",
                    include_track_number: settings.trackNumber || false,
                    audio_format: audioFormat,
                    duration: expectedTrackDurationSeconds(track.duration_ms),
                };
            });
            const existenceResults = await CheckFilesExistence(outputDir, settings.downloadPath, existenceChecks);
            const finalFilePaths: string[] = new Array(tracksWithId.length).fill("");
            const existingSpotifyIDs = new Set<string>();
            const existingFilePaths = new Map<string, string>();
            for (let i = 0; i < existenceResults.length; i++) {
                const result = existenceResults[i];
                if (result.exists) {
                    existingSpotifyIDs.add(result.spotify_id);
                    existingFilePaths.set(result.spotify_id, result.file_path || "");
                    finalFilePaths[i] = result.file_path || "";
                    if (result.file_path) {
                        queueTrackFilePaths.set(result.spotify_id, result.file_path);
                    }
                    updateQueueTrackResult(directQueueItemId, result.spotify_id, "skipped", result.file_path || undefined);
                }
            }
            logger.info(`found ${existingSpotifyIDs.size} existing files`);
            const { AddToDownloadQueue } = await import("../../wailsjs/go/main/App");
            const itemIDs: string[] = [];
            for (const track of tracksWithId) {
                const displayArtist = track.artists;
                const itemID = await AddToDownloadQueue(track.spotify_id || "", track.name || "", displayArtist || "", track.album_name || "");
                itemIDs.push(itemID);
                const trackID = track.spotify_id || "";
                if (existingSpotifyIDs.has(trackID)) {
                    const filePath = existingFilePaths.get(trackID) || "";
                    setTimeout(() => SkipDownloadItem(itemID, filePath), 10);
                    setSkippedTracks((prev: Set<string>) => new Set(prev).add(trackID));
                    setDownloadedTracks((prev: Set<string>) => new Set(prev).add(trackID));
                }
            }
            const tracksToDownload = tracksWithId
                .map((track, originalIndex) => ({ track, originalIndex }))
                .filter(({ track }) => !existingSpotifyIDs.has(track.spotify_id || ""));
            let successCount = 0;
            let errorCount = 0;
            let skippedCount = existingSpotifyIDs.size;
            const failedErrorMessages = new Map<string, string>();
            const completedSpotifyIDs = new Set<string>();
            for (let i = 0; i < tracksToDownload.length; i++) {
                if (downloadExecution.isStopRequested() || downloadExecution.isPauseRequested()) {
                    if (downloadExecution.isStopRequested()) {
                        toast.info(t("translation.download.stopped", { count: successCount, remaining: tracksToDownload.length - i }));
                    }
                    break;
                }
                const { track, originalIndex } = tracksToDownload[i];
                const itemID = itemIDs[originalIndex];
                const trackId = track.spotify_id || "";
                setDownloadingTrack(trackId);
                const displayArtist = track.artists;
                try {
                    const releaseYear = track.release_date?.substring(0, 4);
                    const collectionPosition = collectionTrackPositions.get(trackId) || originalIndex + 1;
                    const response = await downloadWithItemID(settings, itemID, track.name, track.artists, track.album_name, folderName, collectionPosition, track.spotify_id, track.duration_ms, isAlbum, releaseYear, track.album_artist || "", track.release_date, track.images, track.track_number, track.disc_number, track.total_tracks, track.total_discs, track.copyright, track.publisher);
                    if (response.cancelled || downloadExecution.isStopRequested()) {
                        toast.info(t("translation.download.stopped", { count: successCount, remaining: tracksToDownload.length - i }));
                        break;
                    }
                    if (response.success) {
                        if (response.already_exists) {
                            skippedCount++;
                            existingSpotifyIDs.add(trackId);
                            logger.info(`skipped: ${track.name} - ${displayArtist} (already exists)`);
                            setSkippedTracks((prev) => new Set(prev).add(trackId));
                        }
                        else {
                            successCount++;
                            completedSpotifyIDs.add(trackId);
                            logger.success(`downloaded: ${track.name} - ${displayArtist}${formatSourceSuffix(response)}`);
                        }
                        setDownloadedTracks((prev) => new Set(prev).add(trackId));
                        setFailedTracks((prev) => {
                            const newSet = new Set(prev);
                            newSet.delete(trackId);
                            return newSet;
                        });
                        if (response.file) {
                            finalFilePaths[originalIndex] = response.file;
                            queueTrackFilePaths.set(trackId, response.file);
                        }
                        updateQueueTrackResult(directQueueItemId, trackId, response.already_exists ? "skipped" : "done", response.file || undefined);
                    }
                    else if (isCooldownMessage(response.error)) {
                        const remaining = tracksToDownload.length - i - 1;
                        toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                        logger.info(`cooldown detected, pausing queue with ${remaining} track(s) remaining`);
                        downloadExecution.pauseActive();
                        break;
                    }
                    else {
                        errorCount++;
                        logger.error(`failed: ${track.name} - ${displayArtist}`);
                        failedErrorMessages.set(trackId, translateMessage(response.error || t("translation.download.downloadFailed")));
                        setFailedTracks((prev) => new Set(prev).add(trackId));
                        updateQueueTrackResult(directQueueItemId, trackId, "failed");
                    }
                }
                catch (err) {
                    const message = err instanceof Error ? err.message : String(err);
                    if (isCooldownMessage(message)) {
                        const remaining = tracksToDownload.length - i - 1;
                        toast.info(t("translation.migrated.useDownload.serversOnAScheduledBreakPausingDownloads"));
                        logger.info(`cooldown detected, pausing queue with ${remaining} track(s) remaining`);
                        downloadExecution.pauseActive();
                        break;
                    }
                    errorCount++;
                    logger.error(`error: ${track.name} - ${err}`);
                    failedErrorMessages.set(trackId, translateMessage(message));
                    setFailedTracks((prev) => new Set(prev).add(trackId));
                    updateQueueTrackResult(directQueueItemId, trackId, "failed");
                    const { MarkDownloadItemFailed } = await import("../../wailsjs/go/main/App");
                    await MarkDownloadItemFailed(itemID, message);
                }
            }
            const wasStopped = downloadExecution.isStopRequested();
            const wasPaused = downloadExecution.isPauseRequested() && !wasStopped;
            if (wasStopped) {
                try {
                    const { CancelAllQueuedItems: CancelQueued } = await import("../../wailsjs/go/main/App");
                    await CancelQueued();
                }
                catch (err) {
                    logger.error(`failed to clear queued download items: ${err}`);
                }
            }
            const resultFilePaths = Object.fromEntries(queueTrackFilePaths);
            if (wasPaused) {
                logger.info(`batch paused: ${successCount} downloaded, ${skippedCount} skipped, ${errorCount} failed`);
                const result = buildQueueExecutionResult(tracksWithId, existingSpotifyIDs, failedErrorMessages, completedSpotifyIDs, false, true, resultFilePaths);
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, result);
                return result;
            }
            if (wasStopped) {
                if (settings.autoReplayGainTags) {
                    const replayGainCompletedIDs = new Set<string>([
                        ...Object.entries(resumeContext?.trackResults || {}).filter(([, status]) => status === "done").map(([trackID]) => trackID),
                        ...completedSpotifyIDs,
                    ]);
                    const trackPaths = getCompletedTrackPaths(collectionTracks, queueTrackFilePaths, replayGainCompletedIDs);
                    await applyAutomaticReplayGain(trackPaths, false);
                }
                const result = buildQueueExecutionResult(tracksWithId, existingSpotifyIDs, failedErrorMessages, completedSpotifyIDs, true, false, resultFilePaths);
                if (!queueItemId)
                    finishDirectQueueItem(directQueueItemId, result);
                return result;
            }
            if (settings.autoReplayGainTags) {
                const replayGainCompletedIDs = new Set<string>([
                    ...Object.entries(resumeContext?.trackResults || {}).filter(([, status]) => status === "done").map(([trackID]) => trackID),
                    ...completedSpotifyIDs,
                ]);
                if (replayGainCompletedIDs.size > 0) {
                    const albumPaths = errorCount === 0 && isAlbum && settings.autoReplayGainMode === "album"
                        ? getCompleteAlbumPaths(collectionTracks, queueTrackFilePaths, replayGainCompletedIDs)
                        : null;
                    const trackPaths = getCompletedTrackPaths(collectionTracks, queueTrackFilePaths, replayGainCompletedIDs);
                    await applyAutomaticReplayGain(albumPaths || trackPaths, albumPaths !== null);
                }
            }
            if (settings.createM3u8File && folderName) {
                try {
                    logger.info(`creating m3u8 playlist: ${folderName}`);
                    const currentTrackIndex = new Map(tracksWithId.map((track, index) => [track.spotify_id || "", index]));
                    const paths = collectionTracks.map((track) => {
                        const trackID = track.spotify_id || "";
                        const index = currentTrackIndex.get(trackID);
                        return index !== undefined ? finalFilePaths[index] || "" : queueTrackFilePaths.get(trackID) || "";
                    }).filter((path) => path !== "");
                    await CreateM3U8File(folderName, outputDir, paths);
                    toast.success(t("translation.download.m3u8PlaylistCreated"));
                }
                catch (err) {
                    logger.error(`failed to create m3u8 playlist: ${err}`);
                    toast.error(t("translation.migrated.useDownload.failedToCreateM3U8Playlist", { value1: err }));
                }
            }
            if (settings.exportLogsFile && folderName) {
                const logsToExport: string[] = [];
                logsToExport.push(`Download Report - ${new Date().toLocaleString()}`);
                logsToExport.push("-".repeat(50));
                logsToExport.push("");
                let failedCount = 0;
                tracksWithId.forEach((t, idx) => {
                    const spotifyID = t.spotify_id || "";
                    const errorMessage = failedErrorMessages.get(spotifyID);
                    const isFailed = !!errorMessage;
                    const isSkipped = existingSpotifyIDs.has(spotifyID);
                    const isSuccess = !!finalFilePaths[idx];
                    const displayArtist = t.artists;
                    if (isFailed) {
                        failedCount++;
                        logsToExport.push(`${failedCount}. ${t.name} - ${displayArtist}${t.album_name ? ` (${t.album_name})` : ""}`);
                        logsToExport.push(`   Error: ${errorMessage}`);
                        if (spotifyID) {
                            logsToExport.push(`   ID: ${spotifyID}`);
                            logsToExport.push(`   URL: https://open.spotify.com/track/${spotifyID}`);
                        }
                        logsToExport.push("");
                    }
                    else if (!settings.exportLogsOnlyFailed) {
                        if (isSkipped) {
                            logsToExport.push(`[SKIPPED] ${t.name} - ${displayArtist}`);
                        }
                        else if (isSuccess) {
                            logsToExport.push(`[SUCCESS] ${t.name} - ${displayArtist}`);
                        }
                    }
                });
                if (failedCount > 0) {
                    try {
                        logger.info(`creating log file: ${folderName}`);
                        await CreateLogFile(folderName, outputDir, logsToExport);
                        toast.success(t("translation.download.downloadLogCreated"));
                    }
                    catch (err) {
                        logger.error(`failed to create log file: ${err}`);
                    }
                }
            }
            logger.info(`batch complete: ${successCount} downloaded, ${skippedCount} skipped, ${errorCount} failed`);
            if (errorCount === 0 && skippedCount === 0) {
                toast.success(t("translation.download.completed", { count: successCount }));
            }
            else if (errorCount === 0 && successCount === 0) {
                toast.info(t("translation.download.exists", { count: skippedCount }));
            }
            else if (errorCount === 0) {
                toast.info(t("translation.migrated.useDownload.downloadedSkipped", { value1: successCount, value2: skippedCount }));
            }
            else {
                toast.warning(t("translation.download.summary", { downloaded: successCount, skipped: skippedCount, failed: errorCount }));
            }
            const result = buildQueueExecutionResult(tracksWithId, existingSpotifyIDs, failedErrorMessages, completedSpotifyIDs, false, false, resultFilePaths);
            if (!queueItemId)
                finishDirectQueueItem(directQueueItemId, result);
            return result;
        }
        catch (err) {
            const message = err instanceof Error ? err.message : String(err);
            logger.error(`batch download failed: ${message}`);
            toast.error(translateMessage(message));
            const result = buildFailedQueueExecutionResult(tracksWithId, message, downloadExecution.isStopRequested());
            if (!queueItemId)
                finishDirectQueueItem(directQueueItemId, result);
            return result;
        }
        finally {
            resetBatchDownloadState();
        }
        }
        finally {
            if (directLease)
                downloadExecution.release(directLease);
        }
    };
    const handlePauseDownload = (owner?: DownloadExecutionKind) => {
        const kind = owner === "queue" ? "queue" : "direct";
        if (!downloadExecution.requestPause(kind))
            return;
        logger.info("pausing batch after active download finishes");
    };
    const handleResumeDownload = (owner?: DownloadExecutionKind) => {
        downloadExecution.clearPause(owner === "queue" ? "queue" : "direct");
    };
    const handleStopDownload = (owner?: DownloadExecutionKind) => {
        const kind = owner === "queue" ? "queue" : "direct";
        if (!downloadExecution.requestStop(kind))
            return;
        logger.info("download stopped by user");
        const stopping = (async () => {
            try {
                const { ForceStopDownloads } = await import("../../wailsjs/go/main/App");
                await ForceStopDownloads();
            }
            catch (err) {
                console.error("Failed to force stop downloads:", err);
            }
        })();
        downloadExecution.blockFor(stopping);
        toast.info(t("translation.migrated.useDownload.stoppingDownload"));
    };
    const resetDownloadedTracks = () => {
        const restoredQueueTrackStatuses = getQueueTrackStatusSets();
        setDownloadedTracks(new Set(restoredQueueTrackStatuses.downloadedTracks));
        setFailedTracks(new Set(restoredQueueTrackStatuses.failedTracks));
        setSkippedTracks(new Set(restoredQueueTrackStatuses.skippedTracks));
    };
    return {
        isDownloading,
        downloadingTrack,
        downloadedTracks,
        failedTracks,
        skippedTracks,
        handleDownloadTrack,
        handleDownloadSelected,
        handleDownloadAll,
        handlePauseDownload,
        handleResumeDownload,
        handleStopDownload,
        resetDownloadedTracks,
    };
}
