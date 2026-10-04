import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Download, FolderOpen, CircleCheckBig, XCircle, FileText, FileCheck, ImageDown, Play, Pause, CircleCheck, Ellipsis } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger, } from "@/components/ui/tooltip";
import type { TrackMetadata, TrackAvailability } from "@/types/api";
import { usePreview } from "@/hooks/usePreview";
import { useQueueFeedback } from "@/hooks/useQueueFeedback";
import { buildClickableArtists, getClickableArtistKey } from "@/lib/artist-links";

interface TrackInfoProps {
    track: TrackMetadata & {
        album_name: string;
        release_date: string;
    };
    isDownloading: boolean;
    downloadingTrack: string | null;
    isDownloaded: boolean;
    isFailed: boolean;
    isSkipped: boolean;
    downloadingLyricsTrack?: string | null;
    downloadedLyrics?: boolean;
    failedLyrics?: boolean;
    skippedLyrics?: boolean;
    checkingAvailability?: boolean;
    availability?: TrackAvailability;
    downloadingCover?: boolean;
    downloadedCover?: boolean;
    failedCover?: boolean;
    skippedCover?: boolean;
    onQueueTrack?: (track: TrackMetadata) => void;
    onDownloadLyrics?: (spotifyId: string, name: string, artists: string, albumName?: string, albumArtist?: string, releaseDate?: string, discNumber?: number) => void;
    onCheckAvailability?: (spotifyId: string) => void;
    onDownloadCover?: (coverUrl: string, trackName: string, artistName: string, albumName?: string, playlistName?: string, position?: number, trackId?: string, albumArtist?: string, releaseDate?: string, discNumber?: number) => void;
    onOpenFolder: () => void;
    onAlbumClick?: (album: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onArtistClick?: (artist: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onPublisherClick?: (publisher: string) => void;
    onBack?: () => void;
}

function statusWord(isSkipped: boolean, isDownloaded: boolean, isFailed: boolean, t: (key: string) => string): { word: string; tone: string } | null {
    if (isDownloaded)
        return { word: t("translation.queue.done"), tone: "text-foreground" };
    if (isSkipped)
        return { word: t("translation.queue.skipped"), tone: "text-muted-foreground" };
    if (isFailed)
        return { word: t("translation.queue.failed"), tone: "text-destructive" };
    return null;
}

/**
 * One fetched track: cover, title, one metadata line, Download, and preview.
 * Lyrics, cover, and folder sit in More.
 */
export function TrackInfo({ track, downloadingTrack, isDownloaded, isFailed, isSkipped, downloadingLyricsTrack, downloadedLyrics, failedLyrics, skippedLyrics, checkingAvailability: _checkingAvailability, availability: _availability, downloadingCover, downloadedCover, failedCover, skippedCover, onQueueTrack, onDownloadLyrics, onCheckAvailability: _onCheckAvailability, onDownloadCover, onOpenFolder, onAlbumClick, onArtistClick, onPublisherClick: _onPublisherClick, onBack, }: TrackInfoProps) {
    const { t } = useTranslation();
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    const { isQueued } = useQueueFeedback();
    const trackQueued = isQueued(track.spotify_id);
    const hasAlbumClick = !!(onAlbumClick && track.album_id && track.album_url);
    const clickableArtists = buildClickableArtists(track.artists, track.artists_data, track.artist_id, track.artist_url);
    const status = statusWord(isSkipped, isDownloaded, isFailed, t);
    const minutes = Math.floor(track.duration_ms / 60000);
    const seconds = Math.floor((track.duration_ms % 60000) / 1000);
    const durationLabel = `${minutes}:${seconds.toString().padStart(2, "0")}`;
    return (<section className="track-detail mx-auto w-full max-w-5xl">
      <div className="flex items-start gap-8">
        {track.images ? (<img src={track.images} alt={track.name} className="h-[240px] w-[240px] shrink-0 rounded-xl object-cover"/>) : null}
        <div className="min-w-0 flex-1 space-y-1">

          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <h2 className="text-3xl leading-tight font-semibold tracking-tight break-words">{track.name}</h2>
            {status ? (<span className={`font-mono text-[11px] uppercase tracking-wider ${status.tone}`}>{status.word}</span>) : null}
          </div>
          <p className="text-sm text-muted-foreground">
            {clickableArtists.length > 0 ? clickableArtists.map((artist, index) => (<span key={getClickableArtistKey(artist)}>
                  {onArtistClick ? (<button type="button" className="cursor-pointer bg-transparent p-0 text-inherit underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary focus-visible:outline-none" onClick={() => onArtistClick({
                    id: artist.id,
                    name: artist.name,
                    external_urls: artist.external_urls,
                })}>
                        {artist.name}
                      </button>) : (artist.name)}
                  {index < clickableArtists.length - 1 && ", "}
                </span>)) : track.artists}
          </p>
          {onBack ? (<Button variant="ghost" size="sm" className="-ml-2 mt-1 h-7 px-2 text-xs text-muted-foreground" onClick={onBack}>
            {t("translation.common.back")}
          </Button>) : null}
        </div>
      </div>

      <p className="mt-4 text-sm text-muted-foreground">
        {hasAlbumClick ? (<button type="button" className="cursor-pointer bg-transparent p-0 text-inherit underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary focus-visible:outline-none" onClick={() => onAlbumClick?.({
            id: track.album_id!,
            name: track.album_name,
            external_urls: track.album_url!,
        })}>
            {track.album_name}
          </button>) : track.album_name}
        {" · "}
        <span className="font-mono tabular-nums">{track.release_date}</span>
        {" · "}
        <span className="font-mono tabular-nums">{durationLabel}</span>
      </p>

      {track.spotify_id ? (<div className="mt-5 flex flex-wrap items-center gap-2">
        <Button onClick={() => onQueueTrack?.(track)} disabled={trackQueued || !onQueueTrack}>
          {downloadingTrack === track.spotify_id ? <Spinner /> : trackQueued ? <CircleCheck className="size-4" /> : <Download className="size-4" />}
          {t(trackQueued ? "translation.downloads.requested" : "translation.trackInfo.download")}
        </Button>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" disabled={loadingPreview === track.spotify_id} onClick={() => playPreview(track.spotify_id!, track.name)} aria-label={playingTrack === track.spotify_id ? t("translation.migrated.TrackInfo.stopPreview") : t("translation.migrated.TrackInfo.playPreview")}>
              {loadingPreview === track.spotify_id ? (<Spinner />) : playingTrack === track.spotify_id ? (<Pause className="size-3.5"/>) : (<Play className="size-3.5"/>)}
            </Button>
          </TooltipTrigger>
          <TooltipContent><p>{playingTrack === track.spotify_id ? t("translation.migrated.TrackInfo.stopPreview") : t("translation.migrated.TrackInfo.playPreview")}</p></TooltipContent>
        </Tooltip>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" className="gap-1.5">{t("translation.common.more")}<Ellipsis className="size-3.5"/></Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {onDownloadLyrics && <DropdownMenuItem disabled={downloadingLyricsTrack === track.spotify_id} onSelect={() => onDownloadLyrics(track.spotify_id!, track.name, track.artists, track.album_name, track.album_artist, track.release_date, track.disc_number)}>
              {downloadingLyricsTrack === track.spotify_id ? <Spinner /> : skippedLyrics ? <FileCheck className="size-3.5"/> : downloadedLyrics ? <CircleCheckBig className="size-3.5"/> : failedLyrics ? <XCircle className="size-3.5"/> : <FileText className="size-3.5"/>}
              {t("translation.common.downloadSeparateLyric")}
            </DropdownMenuItem>}
            {track.images && onDownloadCover && <DropdownMenuItem disabled={downloadingCover} onSelect={() => onDownloadCover(track.images, track.name, track.artists, track.album_name, undefined, undefined, track.spotify_id, track.album_artist, track.release_date, track.disc_number)}>
              {downloadingCover ? <Spinner /> : skippedCover ? <FileCheck className="size-3.5"/> : downloadedCover ? <CircleCheckBig className="size-3.5"/> : failedCover ? <XCircle className="size-3.5"/> : <ImageDown className="size-3.5"/>}
              {t("translation.common.downloadSeparateCover")}
            </DropdownMenuItem>}
            {isDownloaded && <DropdownMenuItem onSelect={onOpenFolder}>
              <FolderOpen className="size-3.5"/>
              {t("translation.common.openFolder")}
            </DropdownMenuItem>}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>) : null}
    </section>);
}
