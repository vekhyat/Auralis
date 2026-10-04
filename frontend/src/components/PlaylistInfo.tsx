import { translateMessage } from "@/i18n";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { FolderOpen, ImageDown, FileText, Download, CircleCheck } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { InspectorPane } from "./InspectorPane";
import { getSettings } from "@/lib/settings";
import { downloadCover } from "@/lib/api";
import { useState } from "react";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { joinPath, sanitizePath } from "@/lib/utils";
import { parseTemplate, type TemplateData } from "@/lib/settings";
import { buildPlaylistFolderName } from "@/lib/playlist";
import type { TrackMetadata, TrackAvailability } from "@/types/api";
import { useQueueFeedback } from "@/hooks/useQueueFeedback";
interface PlaylistInfoProps {
    playlistInfo: {
        owner: {
            name: string;
            display_name: string;
            images: string;
        };
        tracks: {
            total: number;
        };
        followers: {
            total: number;
        };
        cover?: string;
        description?: string;
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
    onAlbumClick: (album: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onArtistClick: (artist: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onTrackClick: (track: TrackMetadata) => void;
    onBack?: () => void;
}
export function PlaylistInfo({ playlistInfo, trackList, searchQuery, sortBy, selectedTracks, downloadedTracks, failedTracks, skippedTracks, currentPage, itemsPerPage, downloadedLyrics, failedLyrics, skippedLyrics, downloadingLyricsTrack, checkingAvailabilityTrack, availabilityMap, downloadedCovers, failedCovers, skippedCovers, downloadingCoverTrack, isBulkDownloadingCovers, isBulkDownloadingLyrics, isMetadataLoading = false, onSearchChange, onSortChange, onToggleTrack, onToggleSelectAll, onSelectTrackRange, onDownloadLyrics, onDownloadCover, onCheckAvailability, onDownloadAllLyrics, onDownloadAllCovers, onQueueAll, onQueueSelected, onQueueTrack, onOpenFolder, onPageChange, onAlbumClick, onArtistClick, onTrackClick, onBack, }: PlaylistInfoProps) {
    const { t } = useTranslation();
    const settings = getSettings();
    const playlistName = playlistInfo.owner.name;
    const playlistFolderName = buildPlaylistFolderName(playlistName, playlistInfo.owner.display_name, settings.playlistOwnerFolderName);
    const [downloadingPlaylistCover, setDownloadingPlaylistCover] = useState(false);
    const { areQueued, isCollectionQueued } = useQueueFeedback();
    const allTracksQueued = isCollectionQueued("playlist", playlistFolderName) || areQueued(trackList.map((track) => track.spotify_id));
    const selectedTracksQueued = areQueued(selectedTracks);
    const fetchedTrackCount = trackList.length;
    const totalTrackCount = playlistInfo.tracks.total;
    const showStreamingProgress = isMetadataLoading && totalTrackCount > 0 && fetchedTrackCount < totalTrackCount;
    const handleDownloadPlaylistCover = async () => {
        if (!playlistInfo.cover)
            return;
        setDownloadingPlaylistCover(true);
        try {
            const os = settings.operatingSystem;
            let outputDir = settings.downloadPath;
            const placeholder = "__SLASH_PLACEHOLDER__";
            const templateData: TemplateData = {
                artist: "",
                album: "",
                album_artist: "",
                title: playlistName.replace(/\//g, placeholder),
                playlist: playlistFolderName.replace(/\//g, placeholder),
            };
            if (settings.createPlaylistFolder && playlistFolderName) {
                outputDir = joinPath(os, outputDir, sanitizePath(playlistFolderName.replace(/\//g, " "), os));
            }
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
                cover_url: playlistInfo.cover,
                track_name: playlistName,
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
                    toast.success(t("translation.playlistInfo.separatePlaylistCoverDownloaded"));
            }
            else {
                toast.error(translateMessage(response.error || t("translation.common.failedDownloadCover")));
            }
        }
        catch (err) {
            toast.error(err instanceof Error ? translateMessage(err.message) : t("translation.migrated.PlaylistInfo.failedToDownloadCover"));
        }
        finally {
            setDownloadingPlaylistCover(false);
        }
    };
    return (<div className="collection-layout flex flex-col gap-7">
      <div className="min-w-0 flex-1 space-y-4">
        <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
        <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={downloadedTracks} failedTracks={failedTracks} skippedTracks={skippedTracks} currentPage={currentPage} itemsPerPage={itemsPerPage} showCheckboxes={true} hideAlbumColumn={false} folderName={playlistFolderName} downloadedLyrics={downloadedLyrics} failedLyrics={failedLyrics} skippedLyrics={skippedLyrics} downloadingLyricsTrack={downloadingLyricsTrack} checkingAvailabilityTrack={checkingAvailabilityTrack} availabilityMap={availabilityMap} downloadedCovers={downloadedCovers} failedCovers={failedCovers} skippedCovers={skippedCovers} downloadingCoverTrack={downloadingCoverTrack} onToggleTrack={onToggleTrack} onToggleSelectAll={onToggleSelectAll} onSelectTrackRange={onSelectTrackRange} onQueueTrack={onQueueTrack} onDownloadLyrics={onDownloadLyrics} onDownloadCover={onDownloadCover} onCheckAvailability={onCheckAvailability} onPageChange={onPageChange} onAlbumClick={onAlbumClick} onArtistClick={onArtistClick} onTrackClick={onTrackClick}/>
      </div>

      <InspectorPane
        eyebrow={t("translation.playlistInfo.playlist")}
        title={playlistName}
        subtitle={playlistInfo.description}
        cover={playlistInfo.cover || undefined}
        rows={[
            {
                label: t("translation.catalog.owner"),
                value: (<span className="flex items-center justify-end gap-2 truncate">
                    {playlistInfo.owner.images ? (<img src={playlistInfo.owner.images} alt="" className="size-4 rounded-[2px] object-cover"/>) : null}
                    <span className="truncate">{playlistInfo.owner.display_name}</span>
                  </span>),
            },
            {
                label: t("translation.artistInfo.tracks"),
                value: showStreamingProgress
                    ? <span className="font-mono tabular-nums">{`${fetchedTrackCount.toLocaleString()} / ${totalTrackCount.toLocaleString()}`}</span>
                    : <span className="font-mono tabular-nums">{Math.max(totalTrackCount, fetchedTrackCount).toLocaleString()}</span>,
            },
            {
                label: t("translation.catalog.followers"),
                value: <span className="font-mono tabular-nums">{playlistInfo.followers.total.toLocaleString()}</span>,
            },
        ]}
        actions={<>
          <Button size="sm" variant={allTracksQueued ? "outline" : "default"} onClick={onQueueAll}>
            {allTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
            {t(allTracksQueued ? "translation.downloads.requested" : "translation.downloads.download")}
          </Button>
          {selectedTracks.length > 0 && onQueueSelected && (<Button size="sm" variant="outline" onClick={onQueueSelected}>{selectedTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
            {selectedTracksQueued ? t("translation.downloads.requested") : t("translation.downloads.downloadSelected", { value1: selectedTracks.length.toLocaleString() })}</Button>)}
          {onDownloadAllLyrics && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" disabled={isBulkDownloadingLyrics} onClick={onDownloadAllLyrics}>
                  {isBulkDownloadingLyrics ? <Spinner /> : <FileText className="size-3.5"/>}
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.common.downloadAllLyrics")}</p></TooltipContent>
            </Tooltip>)}
          {onDownloadAllCovers && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" disabled={isBulkDownloadingCovers} onClick={onDownloadAllCovers}>
                  {isBulkDownloadingCovers ? <Spinner /> : <ImageDown className="size-3.5"/>}
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.common.downloadAllSeparateCovers")}</p></TooltipContent>
            </Tooltip>)}
          {playlistInfo.cover && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" disabled={downloadingPlaylistCover} onClick={() => void handleDownloadPlaylistCover()}>
                  {downloadingPlaylistCover ? <Spinner /> : <ImageDown className="size-3.5"/>}
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.playlistInfo.downloadSeparatePlaylistCover")}</p></TooltipContent>
            </Tooltip>)}
          {downloadedTracks.size > 0 && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" onClick={onOpenFolder}>
                  <FolderOpen className="size-3.5"/>
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.common.openFolder")}</p></TooltipContent>
            </Tooltip>)}
          {onBack ? (<Button variant="ghost" size="sm" className="h-8 px-2 text-xs text-muted-foreground" onClick={onBack}>
              {t("translation.common.back")}
            </Button>) : null}
        </>}
      />
    </div>);
}
