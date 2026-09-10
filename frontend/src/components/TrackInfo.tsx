import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Download, FolderOpen, CircleCheckBig, XCircle, FileText, FileCheck, ImageDown, Play, Pause, ListPlus, CircleCheck } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger, } from "@/components/ui/tooltip";
import type { TrackMetadata, TrackAvailability } from "@/types/api";
import { usePreview } from "@/hooks/usePreview";
import { useQueueFeedback } from "@/hooks/useQueueFeedback";
import { AvailabilityLinks, hasAvailabilityLinks } from "./AvailabilityLinks";
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
    onDownload: (id: string, name: string, artists: string, albumName?: string, spotifyId?: string, playlistName?: string, durationMs?: number, position?: number, albumArtist?: string, releaseDate?: string, coverUrl?: string, spotifyTrackNumber?: number, spotifyDiscNumber?: number, spotifyTotalTracks?: number, spotifyTotalDiscs?: number, copyright?: string, publisher?: string) => void;
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
 * One fetched track at rest: the inspector is the page. Modest cover,
 * definition-list metadata, actions as a text/button row.
 */
export function TrackInfo({ track, isDownloading, downloadingTrack, isDownloaded, isFailed, isSkipped, downloadingLyricsTrack, downloadedLyrics, failedLyrics, skippedLyrics, checkingAvailability, availability, downloadingCover, downloadedCover, failedCover, skippedCover, onDownload, onQueueTrack, onDownloadLyrics, onCheckAvailability, onDownloadCover, onOpenFolder, onAlbumClick, onArtistClick, onPublisherClick, onBack, }: TrackInfoProps) {
    const { t } = useTranslation();
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    const { isQueued } = useQueueFeedback();
    const trackQueued = isQueued(track.spotify_id);
    const hasAlbumClick = !!(onAlbumClick && track.album_id && track.album_url);
    const clickableArtists = buildClickableArtists(track.artists, track.artists_data, track.artist_id, track.artist_url);
    const formatPlays = (plays: string) => {
        const num = parseInt(plays, 10);
        if (isNaN(num))
            return plays;
        return num.toLocaleString();
    };
    const status = statusWord(isSkipped, isDownloaded, isFailed, t);
    const minutes = Math.floor(track.duration_ms / 60000);
    const seconds = Math.floor((track.duration_ms % 60000) / 1000);
    const durationLabel = `${minutes}:${seconds.toString().padStart(2, "0")}`;
    return (<section className="mx-auto w-full max-w-3xl">
      <div className="flex items-start gap-6">
        {track.images ? (<img src={track.images} alt={track.name} className="h-[120px] w-[120px] shrink-0 rounded-[2px] object-cover"/>) : null}
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.artistInfo.track")}</p>
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <h2 className="text-lg leading-snug font-semibold break-words">{track.name}</h2>
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

      <dl className="mt-6 border-t border-border">
        <div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.common.album")}</dt>
          <dd className="min-w-0 truncate text-right text-[13px]">
            {hasAlbumClick ? (<button type="button" className="max-w-full cursor-pointer truncate bg-transparent p-0 text-left text-inherit underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary focus-visible:outline-none" title={track.album_name} onClick={() => onAlbumClick?.({
                id: track.album_id!,
                name: track.album_name,
                external_urls: track.album_url!,
            })}>
                {track.album_name}
              </button>) : (track.album_name)}
          </dd>
        </div>
        <div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.trackInfo.releaseDate")}</dt>
          <dd className="font-mono text-[13px] tabular-nums">{track.release_date}</dd>
        </div>
        <div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.trackList.duration")}</dt>
          <dd className="font-mono text-[13px] tabular-nums">{durationLabel}</dd>
        </div>
        {track.plays ? (<div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.trackInfo.totalPlays")}</dt>
          <dd className="font-mono text-[13px] tabular-nums">{formatPlays(track.plays)}</dd>
        </div>) : null}
        {track.copyright ? (<div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.common.copyright")}</dt>
          <dd className="min-w-0 truncate pl-6 text-right text-[13px]" title={track.copyright}>{track.copyright}</dd>
        </div>) : null}
        {track.publisher ? (<div className="flex items-baseline justify-between gap-4 border-b border-border py-2">
          <dt className="shrink-0 text-xs text-muted-foreground">{t("translation.trackInfo.recordLabel")}</dt>
          <dd className="min-w-0 truncate text-right text-[13px]">
            <button type="button" className="cursor-pointer bg-transparent p-0 text-inherit underline decoration-border underline-offset-4 transition-colors hover:text-primary hover:decoration-primary focus-visible:outline-none" onClick={() => onPublisherClick?.(track.publisher!)}>
              {track.publisher}
            </button>
          </dd>
        </div>) : null}
      </dl>

      {track.spotify_id ? (<div className="mt-5 flex flex-wrap items-center gap-2">
        <Button size="sm" onClick={() => onDownload(track.spotify_id || "", track.name, track.artists, track.album_name, track.spotify_id, undefined, track.duration_ms, track.track_number, track.album_artist, track.release_date, track.images, track.track_number, track.disc_number, track.total_tracks, track.total_discs, track.copyright, track.publisher)} disabled={isDownloading || downloadingTrack === track.spotify_id}>
          {downloadingTrack === track.spotify_id ? (<Spinner />) : (<>
            <Download className="size-3.5"/>
            {t("translation.trackInfo.download")}
          </>)}
        </Button>
        {onQueueTrack && (<Button variant="outline" size="sm" onClick={() => onQueueTrack(track)}>
          {trackQueued ? (<CircleCheck className="size-3.5"/>) : (<ListPlus className="size-3.5"/>)}
          {t(trackQueued ? "translation.queue.alreadyInQueue" : "translation.queue.addToQueue")}
        </Button>)}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" disabled={loadingPreview === track.spotify_id} onClick={() => playPreview(track.spotify_id!, track.name)}>
              {loadingPreview === track.spotify_id ? (<Spinner />) : playingTrack === track.spotify_id ? (<Pause className="size-3.5"/>) : (<Play className="size-3.5"/>)}
            </Button>
          </TooltipTrigger>
          <TooltipContent><p>{playingTrack === track.spotify_id ? t("translation.migrated.TrackInfo.stopPreview") : t("translation.migrated.TrackInfo.playPreview")}</p></TooltipContent>
        </Tooltip>
        {onDownloadLyrics && (<Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" disabled={downloadingLyricsTrack === track.spotify_id} onClick={() => onDownloadLyrics(track.spotify_id!, track.name, track.artists, track.album_name, track.album_artist, track.release_date, track.disc_number)}>
              {downloadingLyricsTrack === track.spotify_id ? (<Spinner />) : skippedLyrics ? (<FileCheck className="size-3.5"/>) : downloadedLyrics ? (<CircleCheckBig className="size-3.5"/>) : failedLyrics ? (<XCircle className="size-3.5"/>) : (<FileText className="size-3.5"/>)}
            </Button>
          </TooltipTrigger>
          <TooltipContent><p>{t("translation.common.downloadSeparateLyric")}</p></TooltipContent>
        </Tooltip>)}
        {track.images && onDownloadCover && (<Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" disabled={downloadingCover} onClick={() => onDownloadCover(track.images, track.name, track.artists, track.album_name, undefined, undefined, track.spotify_id, track.album_artist, track.release_date, track.disc_number)}>
              {downloadingCover ? (<Spinner />) : skippedCover ? (<FileCheck className="size-3.5"/>) : downloadedCover ? (<CircleCheckBig className="size-3.5"/>) : failedCover ? (<XCircle className="size-3.5"/>) : (<ImageDown className="size-3.5"/>)}
            </Button>
          </TooltipTrigger>
          <TooltipContent><p>{t("translation.common.downloadSeparateCover")}</p></TooltipContent>
        </Tooltip>)}
        {onCheckAvailability && (<Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" disabled={checkingAvailability} onClick={() => onCheckAvailability(track.spotify_id!)}>
              {checkingAvailability ? (<Spinner />) : availability ? (hasAvailabilityLinks(availability) ? (<CircleCheck className="size-3.5"/>) : (<XCircle className="size-3.5"/>)) : (<GlobeFallback/>)}
            </Button>
          </TooltipTrigger>
          <TooltipContent className="pointer-events-auto">
            <AvailabilityLinks availability={availability}/>
          </TooltipContent>
        </Tooltip>)}
        {isDownloaded && (<Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon-sm" onClick={onOpenFolder}>
              <FolderOpen className="size-3.5"/>
            </Button>
          </TooltipTrigger>
          <TooltipContent><p>{t("translation.common.openFolder")}</p></TooltipContent>
        </Tooltip>)}
      </div>) : null}
    </section>);
}

function GlobeFallback() {
    // Quiet availability probe marker: a ruled circle, no traffic light.
    return <span aria-hidden="true" className="inline-block size-3.5 rounded-full border border-current opacity-60"/>;
}
