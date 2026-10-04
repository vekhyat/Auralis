import { translateMessage } from "@/i18n";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Ellipsis, FolderOpen, ImageDown, FileText, Download, CircleCheck } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { TrackList } from "./TrackList";
import { InspectorPane } from "./InspectorPane";
import { getSettings } from "@/lib/settings";
import { downloadCover } from "@/lib/api";
import { useState } from "react";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { joinPath, sanitizePath } from "@/lib/utils";
import { parseTemplate, type TemplateData } from "@/lib/settings";
import { buildClickableArtists, splitArtistNames, getClickableArtistKey } from "@/lib/artist-links";
import type { TrackMetadata, TrackAvailability } from "@/types/api";
import { useQueueFeedback } from "@/hooks/useQueueFeedback";
interface AlbumInfoProps {
    albumInfo: {
        name: string;
        artists: string;
        images: string;
        release_date: string;
        total_tracks: number;
        is_explicit?: boolean;
        artist_id?: string;
        artist_url?: string;
    };
    trackList: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    selectedTracks: string[];
    downloadedTracks: Set<string>;
    failedTracks: Set<string>;
    skippedTracks: Set<string>;
    currentPage: number;
    itemsPerPage: number;
    downloadedLyrics?: Set<string>;
    failedLyrics?: Set<string>;
    skippedLyrics?: Set<string>;
    downloadingLyricsTrack?: string | null;
    checkingAvailabilityTrack?: string | null;
    availabilityMap?: Map<string, TrackAvailability>;
    downloadedCovers?: Set<string>;
    failedCovers?: Set<string>;
    skippedCovers?: Set<string>;
    downloadingCoverTrack?: string | null;
    isBulkDownloadingCovers?: boolean;
    isBulkDownloadingLyrics?: boolean;
    isMetadataLoading?: boolean;
    onSearchChange: (value: string) => void;
    onSortChange: (value: string) => void;
    onToggleTrack: (id: string) => void;
    onToggleSelectAll: (tracks: TrackMetadata[]) => void;
    onSelectTrackRange?: (ids: string[], select: boolean) => void;
    onDownloadLyrics?: (spotifyId: string, name: string, artists: string, albumName: string, folderName?: string, isArtistDiscography?: boolean, position?: number, albumArtist?: string, releaseDate?: string, discNumber?: number) => void;
    onDownloadCover?: (coverUrl: string, trackName: string, artistName: string, albumName: string, folderName?: string, isArtistDiscography?: boolean, position?: number, trackId?: string, albumArtist?: string, releaseDate?: string, discNumber?: number) => void;
    onCheckAvailability?: (spotifyId: string) => void;
    onDownloadAllLyrics?: () => void;
    onDownloadAllCovers?: () => void;
    onQueueAll: () => void;
    onQueueSelected?: () => void;
    onQueueTrack?: (track: TrackMetadata, position?: number) => void;
    onOpenFolder: () => void;
    onPageChange: (page: number) => void;
    onArtistClick?: (artist: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onTrackClick?: (track: TrackMetadata) => void;
    onBack?: () => void;
}
export function AlbumInfo({ albumInfo, trackList, searchQuery, sortBy, selectedTracks, downloadedTracks, failedTracks, skippedTracks, currentPage, itemsPerPage, downloadedLyrics, failedLyrics, skippedLyrics, downloadingLyricsTrack, checkingAvailabilityTrack, availabilityMap, downloadedCovers, failedCovers, skippedCovers, downloadingCoverTrack, isBulkDownloadingCovers, isBulkDownloadingLyrics, isMetadataLoading = false, onSearchChange: _onSearchChange, onSortChange: _onSortChange, onToggleTrack, onToggleSelectAll, onSelectTrackRange, onDownloadLyrics, onDownloadCover, onCheckAvailability, onDownloadAllLyrics, onDownloadAllCovers, onQueueAll, onQueueSelected, onQueueTrack, onOpenFolder, onPageChange, onArtistClick, onTrackClick, onBack, }: AlbumInfoProps) {
    const { t } = useTranslation();
    const settings = getSettings();
    const albumArtistNames = splitArtistNames(albumInfo.artists);
    const artistSeparator = albumInfo.artists.includes(";") ? "; " : ", ";
    const fetchedTrackCount = trackList.length;
    const totalTrackCount = albumInfo.total_tracks;
    const showStreamingProgress = isMetadataLoading && totalTrackCount > 0 && fetchedTrackCount < totalTrackCount;
    const clickableAlbumArtists = (() => {
        const artistsByName = new Map<string, {
            id: string;
            name: string;
            external_urls: string;
        }>();
        for (const track of trackList) {
            const clickableTrackArtists = buildClickableArtists(track.artists, track.artists_data, track.artist_id, track.artist_url);
            for (const artist of clickableTrackArtists) {
                const normalizedName = artist.name.trim().toLowerCase();
                if (!normalizedName || !artist.external_urls || artistsByName.has(normalizedName)) {
                    continue;
                }
                artistsByName.set(normalizedName, artist);
            }
        }
        return albumArtistNames.map((name) => {
            const normalizedName = name.trim().toLowerCase();
            const matchedArtist = artistsByName.get(normalizedName);
            if (matchedArtist) {
                return {
                    ...matchedArtist,
                    name,
                };
            }
            if (albumArtistNames.length === 1 && albumInfo.artist_id && albumInfo.artist_url) {
                return {
                    id: albumInfo.artist_id,
                    name,
                    external_urls: albumInfo.artist_url,
                };
            }
            return {
                id: "",
                name,
                external_urls: "",
            };
        });
    })();
    const [downloadingAlbumCover, setDownloadingAlbumCover] = useState(false);
    const { areQueued, isCollectionQueued } = useQueueFeedback();
    const allTracksQueued = isCollectionQueued("album", albumInfo.name) || areQueued(trackList.map((track) => track.spotify_id));
    const selectedTracksQueued = areQueued(selectedTracks);
    const handleDownloadAlbumCover = async () => {
        if (!albumInfo.images)
            return;
        setDownloadingAlbumCover(true);
        try {
            const os = settings.operatingSystem;
            let outputDir = settings.downloadPath;
            const albumName = albumInfo.name;
            const artistName = albumInfo.artists;
            const placeholder = "__SLASH_PLACEHOLDER__";
            const templateData: TemplateData = {
                artist: artistName?.replace(/\//g, placeholder),
                album: albumName?.replace(/\//g, placeholder),
                album_artist: artistName?.replace(/\//g, placeholder),
                title: albumName?.replace(/\//g, placeholder),
                year: albumInfo.release_date?.substring(0, 4),
                date: albumInfo.release_date,
            };
            if (settings.folderTemplate) {
                const folderPath = parseTemplate(settings.folderTemplate, templateData);
                if (folderPath) {
                    const parts = folderPath.split("/").filter((p: string) => p.trim());
                    for (const part of parts) {
                        outputDir = joinPath(os, outputDir, sanitizePath(part.replace(new RegExp(placeholder, "g"), " "), os));
                    }
                }
            }
            const response = await downloadCover({
                cover_url: albumInfo.images,
                track_name: albumName,
                artist_name: "",
                album_name: "",
                album_artist: "",
                release_date: "",
                output_dir: outputDir,
                filename_format: "title",
                track_number: false,
                position: 0,
                disc_number: 0,
            });
            if (response.success) {
                if (response.already_exists)
                    toast.info(t("translation.common.coverAlreadyExists"));
                else
                    toast.success(t("translation.albumInfo.separateAlbumCoverDownloaded"));
            }
            else {
                toast.error(translateMessage(response.error || t("translation.common.failedDownloadCover")));
            }
        }
        catch (err) {
            toast.error(err instanceof Error ? translateMessage(err.message) : t("translation.migrated.AlbumInfo.failedToDownloadCover"));
        }
        finally {
            setDownloadingAlbumCover(false);
        }
    };
    return (<div className="collection-layout flex flex-col gap-7">
      <div className="min-w-0 flex-1 space-y-4">
        <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={downloadedTracks} failedTracks={failedTracks} skippedTracks={skippedTracks} currentPage={currentPage} itemsPerPage={itemsPerPage} showCheckboxes={true} hideAlbumColumn={true} folderName={albumInfo.name} downloadedLyrics={downloadedLyrics} failedLyrics={failedLyrics} skippedLyrics={skippedLyrics} downloadingLyricsTrack={downloadingLyricsTrack} checkingAvailabilityTrack={checkingAvailabilityTrack} availabilityMap={availabilityMap} onToggleTrack={onToggleTrack} onToggleSelectAll={onToggleSelectAll} onSelectTrackRange={onSelectTrackRange} onQueueTrack={onQueueTrack} onDownloadLyrics={onDownloadLyrics} onDownloadCover={onDownloadCover} downloadedCovers={downloadedCovers} failedCovers={failedCovers} skippedCovers={skippedCovers} downloadingCoverTrack={downloadingCoverTrack} onCheckAvailability={onCheckAvailability} onPageChange={onPageChange} onArtistClick={onArtistClick} onTrackClick={onTrackClick}/>
      </div>

      <InspectorPane
        eyebrow={t("translation.common.album")}
        title={albumInfo.name}
        subtitle={<>
          {clickableAlbumArtists.length > 0 ? clickableAlbumArtists.map((artist, index) => (<span key={getClickableArtistKey(artist)}>
              {onArtistClick && artist.external_urls ? (<button type="button" className="cursor-pointer bg-transparent p-0 text-inherit underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary" onClick={() => onArtistClick({
                    id: artist.id,
                    name: artist.name,
                    external_urls: artist.external_urls,
                })}>
                  {artist.name}
                </button>) : (artist.name)}
              {index < clickableAlbumArtists.length - 1 && artistSeparator}
            </span>)) : albumInfo.artists}
          {albumInfo.is_explicit ? ` · ${t("translation.common.explicit")}` : ""}
          {" · "}
          <span className="font-mono tabular-nums">{albumInfo.release_date}</span>
          {" · "}
          <span className="font-mono tabular-nums">{showStreamingProgress ? t("translation.migrated.AlbumInfo.tracks", { value1: fetchedTrackCount.toLocaleString(), value2: totalTrackCount.toLocaleString() }) : t("translation.downloads.trackCount", { count: Math.max(totalTrackCount, fetchedTrackCount) })}</span>
        </>}
        cover={albumInfo.images || undefined}
        actions={<>
          <Button size="sm" variant={allTracksQueued ? "outline" : "default"} onClick={onQueueAll}>
            {allTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
            {t(allTracksQueued ? "translation.downloads.requested" : "translation.downloads.download")}
          </Button>
          {selectedTracks.length > 0 && onQueueSelected && (<Button size="sm" variant="outline" onClick={onQueueSelected}>{selectedTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
            {selectedTracksQueued ? t("translation.downloads.requested") : t("translation.downloads.downloadSelected", { value1: selectedTracks.length.toLocaleString() })}</Button>)}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="gap-1.5">{t("translation.common.more")}<Ellipsis className="size-3.5"/></Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {onDownloadAllLyrics && <DropdownMenuItem disabled={isBulkDownloadingLyrics} onSelect={onDownloadAllLyrics}>
                {isBulkDownloadingLyrics ? <Spinner /> : <FileText className="size-3.5"/>}
                {t("translation.common.downloadAllLyrics")}
              </DropdownMenuItem>}
              {onDownloadAllCovers && <DropdownMenuItem disabled={isBulkDownloadingCovers} onSelect={onDownloadAllCovers}>
                {isBulkDownloadingCovers ? <Spinner /> : <ImageDown className="size-3.5"/>}
                {t("translation.common.downloadAllSeparateCovers")}
              </DropdownMenuItem>}
              {albumInfo.images && <DropdownMenuItem disabled={downloadingAlbumCover} onSelect={() => void handleDownloadAlbumCover()}>
                {downloadingAlbumCover ? <Spinner /> : <ImageDown className="size-3.5"/>}
                {t("translation.albumInfo.downloadSeparateAlbumCover")}
              </DropdownMenuItem>}
              {downloadedTracks.size > 0 && <DropdownMenuItem onSelect={onOpenFolder}>
                <FolderOpen className="size-3.5"/>
                {t("translation.common.openFolder")}
              </DropdownMenuItem>}
            </DropdownMenuContent>
          </DropdownMenu>
          {onBack ? (<Button variant="ghost" size="sm" className="h-8 px-2 text-xs text-muted-foreground" onClick={onBack}>
              {t("translation.common.back")}
            </Button>) : null}
        </>}
      />
    </div>);
}
