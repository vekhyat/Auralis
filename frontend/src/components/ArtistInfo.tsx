import { translateMessage } from "@/i18n";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { FolderOpen, ImageDown, FileText, CheckCheck, Download, CircleCheck } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { InspectorPane } from "./InspectorPane";
import { ArtworkCard } from "./ArtworkCard";
import type { TrackMetadata, TrackAvailability } from "@/types/api";
import { downloadHeader, downloadGalleryImage, downloadAvatar } from "@/lib/api";
import { getAlbumCategoryLabel, getSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { useState, useMemo } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useQueueFeedback } from "@/hooks/useQueueFeedback";
import { addCollectionToQueue } from "@/lib/queue";
const SINGLE_ALBUM_TYPES = new Set(["single", "singles"]);
const EP_ALBUM_TYPES = new Set(["ep", "eps"]);
const AMBIGUOUS_SINGLE_EP_TYPES = new Set(["epsingle", "singleep"]);
const ALBUM_FILTER_ORDER = ["single", "ep", "single_ep", "album", "compilation", "live", "appears_on"];
const normalizeAlbumFilterType = (value: string) => {
    const normalized = value.trim().toLowerCase().replace(/[\s-]+/g, "_");
    const compact = normalized.replace(/[^a-z]/g, "");
    if (compact === "appearson")
        return "appears_on";
    if (AMBIGUOUS_SINGLE_EP_TYPES.has(compact))
        return "single_ep";
    if (EP_ALBUM_TYPES.has(compact))
        return "ep";
    return SINGLE_ALBUM_TYPES.has(compact) ? "single" : normalized;
};
const toTitleCaseLabel = (value: string) => value
    .split(/(\s+)/)
    .map((part) => part.trim() ? part.charAt(0).toLocaleUpperCase() + part.slice(1) : part)
    .join("");
interface ArtistInfoProps {
    artistInfo: {
        name: string;
        images: string;
        header?: string;
        gallery?: string[];
        followers: number;
        total_albums?: number;
        genres: string[];
        biography?: string;
        verified?: boolean;
        listeners?: number;
        rank?: number;
    };
    albumList: Array<{
        id: string;
        name: string;
        images: string;
        release_date: string;
        album_type: string;
        external_urls: string;
        total_tracks?: number;
        is_explicit?: boolean;
    }>;
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
    onPageChange: (page: number) => void;
    onTrackClick?: (track: TrackMetadata) => void;
    onBack?: () => void;
}
export function ArtistInfo({ artistInfo, albumList, trackList, searchQuery, sortBy, selectedTracks, downloadedTracks, failedTracks, skippedTracks, currentPage, itemsPerPage, downloadedLyrics, failedLyrics, skippedLyrics, downloadingLyricsTrack, checkingAvailabilityTrack, availabilityMap, downloadedCovers, failedCovers, skippedCovers, downloadingCoverTrack, isBulkDownloadingCovers, isBulkDownloadingLyrics, onSearchChange, onSortChange, onToggleTrack, onToggleSelectAll, onSelectTrackRange, onDownloadLyrics, onDownloadCover, onCheckAvailability, onDownloadAllLyrics, onDownloadAllCovers, onQueueAll, onQueueSelected, onQueueTrack, onOpenFolder, onAlbumClick, onArtistClick, onPageChange, onTrackClick, onBack, }: ArtistInfoProps) {
    const { t } = useTranslation();
    const { areQueued, isCollectionQueued } = useQueueFeedback();
    const allTracksQueued = isCollectionQueued("artist", artistInfo.name) || areQueued((trackList || []).map((track) => track.spotify_id));
    const selectedTracksQueued = areQueued(selectedTracks);
    const [downloadingHeader, setDownloadingHeader] = useState(false);
    const [downloadingAvatar, setDownloadingAvatar] = useState(false);
    const [downloadingGalleryIndex, setDownloadingGalleryIndex] = useState<number | null>(null);
    const [downloadingAllGallery, setDownloadingAllGallery] = useState(false);
    const [activeTab, setActiveTab] = useState<"albums" | "tracks" | "gallery">("albums");
    const [activeAlbumFilter, setActiveAlbumFilter] = useState<string>("all");
    const displayedAlbumCount = artistInfo.total_albums || albumList.length;
    const totalTrackCount = albumList.reduce((sum, album) => sum + (album.total_tracks || 0), 0);
    const fetchedTrackCount = trackList.length;
    const resolvedTrackCount = totalTrackCount > 0 ? totalTrackCount : fetchedTrackCount;
    const albumFilterCounts = useMemo(() => {
        const counts = new Map<string, number>();
        counts.set("all", (albumList || []).length);
        for (const album of albumList || []) {
            const type = normalizeAlbumFilterType(album.album_type || "");
            if (!type)
                continue;
            counts.set(type, (counts.get(type) || 0) + 1);
        }
        return counts;
    }, [albumList]);
    const albumFilters = useMemo(() => {
        const uniqueTypes = Array.from(new Set((albumList || [])
            .map((album) => normalizeAlbumFilterType(album.album_type || ""))
            .filter(Boolean)));
        const orderedTypes = ALBUM_FILTER_ORDER.filter((type) => uniqueTypes.includes(type));
        const remainingTypes = uniqueTypes.filter((type) => !ALBUM_FILTER_ORDER.includes(type));
        return ["all", ...orderedTypes, ...remainingTypes];
    }, [albumList]);
    const filteredAlbums = useMemo(() => {
        const normalizedActiveFilter = normalizeAlbumFilterType(activeAlbumFilter);
        if (normalizedActiveFilter === "all") {
            return albumList || [];
        }
        return (albumList || []).filter((album) => normalizeAlbumFilterType(album.album_type || "") === normalizedActiveFilter);
    }, [albumList, activeAlbumFilter]);
    const discographyTracks = useMemo(() => {
        const albumIds = new Set(filteredAlbums.map((album) => album.id));
        return (trackList || []).filter((track) => track.album_id && albumIds.has(track.album_id));
    }, [filteredAlbums, trackList]);
    const discographyTracksWithId = useMemo(() => discographyTracks.filter((track) => track.spotify_id), [discographyTracks]);
    const allDiscographySelected = discographyTracksWithId.length > 0 &&
        discographyTracksWithId.every((track) => selectedTracks.includes(track.spotify_id!));
    const queueSelectedAlbums = () => {
        const selected = (trackList || []).filter((track) => track.spotify_id && selectedTracks.includes(track.spotify_id));
        const groups = new Map<string, TrackMetadata[]>();
        for (const track of selected) {
            const key = track.album_id || track.album_name;
            if (key)
                groups.set(key, [...(groups.get(key) || []), track]);
        }
        let added = 0;
        for (const tracks of groups.values()) {
            const first = tracks[0];
            added += addCollectionToQueue({
                type: "album", name: first.album_name, artist: first.album_artist || first.artists,
                info: t("translation.downloads.trackCount", { count: tracks.length }), image: first.images || "",
                folderName: first.album_name, isAlbum: true, tracks,
            }).added;
        }
        if (added > 0)
            toast.success(t("translation.queue.addedValue1Queue", { value1: `${added} ${t("translation.common.albums")}` }));
        else
            toast.info(t("translation.downloads.requested"));
    };
    const filteredAlbumGroups = useMemo(() => {
        const albumTypeMap = new Map(albumList.map(a => [a.id, a.album_type]));
        const albumGroups = trackList.reduce((acc, track) => {
            const key = track.album_id || track.album_name;
            if (!key)
                return acc;
            if (!acc[key]) {
                acc[key] = {
                    name: track.album_name,
                    count: 0,
                    tracks: [],
                    type: (track.album_id && albumTypeMap.get(track.album_id)) || "unknown"
                };
            }
            acc[key].count++;
            acc[key].tracks.push(track);
            return acc;
        }, {} as Record<string, {
            name: string;
            count: number;
            tracks: TrackMetadata[];
            type: string;
        }>);
        return Object.entries(albumGroups).sort((a, b) => {
            const dateA = a[1].tracks[0]?.release_date || "";
            const dateB = b[1].tracks[0]?.release_date || "";
            return dateB.localeCompare(dateA);
        });
    }, [trackList, albumList]);
    const formatAlbumFilterLabel = (value: string) => {
        const count = albumFilterCounts.get(value) || 0;
        if (value === "all")
            return `${t("translation.queue.all")} (${count})`;
        const normalizedValue = normalizeAlbumFilterType(value);
        const translatedCategory = normalizedValue === "album"
            ? t("translation.common.albums")
            : normalizedValue === "live"
                ? t("translation.backend.live")
                : normalizedValue === "compilation"
                    ? t("translation.backend.compilation")
                    : normalizedValue === "single"
                        ? t("translation.artistInfo.singles")
                        : normalizedValue === "ep"
                            ? t("translation.artistInfo.eps")
                            : normalizedValue === "single_ep"
                                ? t("translation.artistInfo.singlesAndEps")
                                : normalizedValue === "appears_on"
                                    ? t("translation.artistInfo.appearsOn")
                                    : "";
        if (translatedCategory) {
            return `${toTitleCaseLabel(translatedCategory)} (${count})`;
        }
        const categoryLabel = getAlbumCategoryLabel(value);
        if (categoryLabel) {
            return `${toTitleCaseLabel(categoryLabel)} (${count})`;
        }
        const label = value
            .split(/[_\s]+/)
            .filter(Boolean)
            .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
            .join(" ");
        return `${toTitleCaseLabel(label)} (${count})`;
    };
    const getAlbumTypeBadgeLabel = (value: string) => {
        switch (normalizeAlbumFilterType(value || "")) {
            case "album":
                return toTitleCaseLabel(t("translation.common.album"));
            case "live":
                return toTitleCaseLabel(t("translation.backend.live"));
            case "compilation":
                return toTitleCaseLabel(t("translation.backend.compilation"));
            case "single":
                return toTitleCaseLabel(t("translation.backend.single"));
            case "ep":
                return toTitleCaseLabel(t("translation.backend.ep"));
            case "single_ep":
                return toTitleCaseLabel(t("translation.artistInfo.singlesAndEps"));
            case "appears_on":
                return toTitleCaseLabel(t("translation.artistInfo.appearsOn"));
            default:
                return toTitleCaseLabel(value || "");
        }
    };
    const handleDownloadHeader = async () => {
        if (!artistInfo.header)
            return;
        setDownloadingHeader(true);
        try {
            const settings = getSettings();
            const response = await downloadHeader({
                header_url: artistInfo.header,
                artist_name: artistInfo.name,
                output_dir: settings.downloadPath,
            });
            if (response.success) {
                if (response.already_exists) {
                    toast.info(t("translation.artistInfo.headerAlreadyExists"));
                }
                else {
                    toast.success(t("translation.artistInfo.headerDownloadedSuccessfully"));
                }
            }
            else {
                toast.error(translateMessage(response.error || t("translation.artistInfo.failedDownloadHeader")));
            }
        }
        catch (error) {
            toast.error(t("translation.migrated.ArtistInfo.errorDownloadingHeader", { value1: error }));
        }
        finally {
            setDownloadingHeader(false);
        }
    };
    const handleDownloadAvatar = async () => {
        if (!artistInfo.images)
            return;
        setDownloadingAvatar(true);
        try {
            const settings = getSettings();
            const response = await downloadAvatar({
                avatar_url: artistInfo.images,
                artist_name: artistInfo.name,
                output_dir: settings.downloadPath,
            });
            if (response.success) {
                if (response.already_exists) {
                    toast.info(t("translation.artistInfo.avatarAlreadyExists"));
                }
                else {
                    toast.success(t("translation.artistInfo.avatarDownloadedSuccessfully"));
                }
            }
            else {
                toast.error(translateMessage(response.error || t("translation.artistInfo.failedDownloadAvatar")));
            }
        }
        catch (error) {
            toast.error(t("translation.migrated.ArtistInfo.errorDownloadingAvatar", { value1: error }));
        }
        finally {
            setDownloadingAvatar(false);
        }
    };
    const handleDownloadGalleryImage = async (imageUrl: string, index: number) => {
        setDownloadingGalleryIndex(index);
        try {
            const settings = getSettings();
            const response = await downloadGalleryImage({
                image_url: imageUrl,
                artist_name: artistInfo.name,
                image_index: index,
                output_dir: settings.downloadPath,
            });
            if (response.success) {
                if (response.already_exists) {
                    toast.info(t("translation.migrated.ArtistInfo.galleryImageAlreadyExists", { value1: index + 1 }));
                }
                else {
                    toast.success(t("translation.migrated.ArtistInfo.galleryImageDownloadedSuccessfully", { value1: index + 1 }));
                }
            }
            else {
                toast.error(translateMessage(response.error || t("translation.artistInfo.failedDownloadGalleryImageValue1", { value1: index + 1 })));
            }
        }
        catch (error) {
            toast.error(t("translation.migrated.ArtistInfo.errorDownloadingGalleryImage", { value1: index + 1, value2: error }));
        }
        finally {
            setDownloadingGalleryIndex(null);
        }
    };
    const handleDownloadAllGallery = async () => {
        if (!artistInfo.gallery || artistInfo.gallery.length === 0)
            return;
        setDownloadingAllGallery(true);
        try {
            const settings = getSettings();
            let successCount = 0;
            let existsCount = 0;
            let failCount = 0;
            for (let index = 0; index < artistInfo.gallery.length; index++) {
                const imageUrl = artistInfo.gallery[index];
                try {
                    const response = await downloadGalleryImage({
                        image_url: imageUrl,
                        artist_name: artistInfo.name,
                        image_index: index,
                        output_dir: settings.downloadPath,
                    });
                    if (response.success) {
                        if (response.already_exists) {
                            existsCount++;
                        }
                        else {
                            successCount++;
                        }
                    }
                    else {
                        failCount++;
                    }
                }
                catch {
                    failCount++;
                }
            }
            if (failCount === 0) {
                if (existsCount > 0 && successCount > 0) {
                    toast.success(t("translation.migrated.ArtistInfo.imagesDownloadedAlreadyExisted", { value1: successCount, value2: existsCount }));
                }
                else if (existsCount > 0) {
                    toast.info(t("translation.migrated.ArtistInfo.allImagesAlreadyExist", { value1: existsCount }));
                }
                else {
                    toast.success(t("translation.migrated.ArtistInfo.allGalleryImagesDownloadedSuccessfully", { value1: successCount }));
                }
            }
            else {
                toast.error(t("translation.migrated.ArtistInfo.imagesFailedToDownload", { value1: failCount }));
            }
        }
        catch (error) {
            toast.error(t("translation.migrated.ArtistInfo.errorDownloadingGalleryImages", { value1: error }));
        }
        finally {
            setDownloadingAllGallery(false);
        }
    };
    const hasGallery = artistInfo.gallery && artistInfo.gallery.length > 0;
    const viewTabs: Array<{ key: "albums" | "tracks" | "gallery"; label: string }> = [
        { key: "albums", label: t("translation.common.albums") },
        { key: "tracks", label: t("translation.artistInfo.allTracks") },
        ...(hasGallery ? [{ key: "gallery" as const, label: t("translation.artistInfo.gallery") }] : []),
    ];
    return (<div className="collection-layout flex flex-col gap-7">
      <div className="min-w-0 flex-1 space-y-4">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border pb-2">
          {viewTabs.map((tab) => (<button
            key={tab.key}
            type="button"
            onClick={() => setActiveTab(tab.key)}
            className={`cursor-pointer text-[13px] transition-colors ${activeTab === tab.key
                ? "font-semibold text-primary underline decoration-primary underline-offset-[6px]"
                : "text-muted-foreground hover:text-foreground"}`}
          >
            {tab.label}
          </button>))}
        </div>

        {activeTab === "albums" && albumList.length > 0 && (<div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.catalog.discography")}</h3>
              <div className="flex flex-wrap items-center gap-1.5">
                  {discographyTracksWithId.length > 0 && (<Button variant="outline" size="sm" onClick={() => onToggleSelectAll(discographyTracks)}>
                      <CheckCheck className="size-3.5"/>
                      {allDiscographySelected ? t("translation.migrated.ArtistInfo.deselectAll") : t("translation.migrated.ArtistInfo.selectAll")}
                  </Button>)}
                  <Button size="sm" variant={allTracksQueued ? "outline" : "default"} onClick={onQueueAll}>
                      {allTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
                      {t(allTracksQueued ? "translation.downloads.requested" : "translation.downloads.download")}
                  </Button>
                  {selectedTracks.length > 0 && onQueueSelected && (<Button size="sm" variant="outline" onClick={queueSelectedAlbums}>{selectedTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}{selectedTracksQueued ? t("translation.downloads.requested") : t("translation.downloads.downloadSelected", { value1: selectedTracks.length.toLocaleString() })}</Button>)}
              </div>
            </div>
            {albumFilters.length > 1 && (<div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {albumFilters.map((filter) => (<button
                    key={filter}
                    type="button"
                    onClick={() => setActiveAlbumFilter(filter)}
                    className={`cursor-pointer text-xs transition-colors ${activeAlbumFilter === filter
                    ? "font-semibold text-primary underline decoration-primary underline-offset-4"
                    : "text-muted-foreground hover:text-foreground"}`}
                  >
                    {formatAlbumFilterLabel(filter)}
                  </button>))}
              </div>)}
            <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 xl:grid-cols-5">
              {filteredAlbums.map((album) => {
                const albumTracks = trackList.filter(t => t.album_id === album.id);
                const tracksWithId = albumTracks.filter(t => t.spotify_id);
                const isSelected = tracksWithId.length > 0 && tracksWithId.every(t => selectedTracks.includes(t.spotify_id!));
                const hasTracks = tracksWithId.length > 0;
                const handleFetch = () => onAlbumClick({
                    id: album.id,
                    name: album.name,
                    external_urls: album.external_urls,
                });
                const handleSelectClick = () => {
                    if (!hasTracks)
                        return;
                    onToggleSelectAll(albumTracks);
                };
                return (<ArtworkCard
                  key={album.id}
                  cover={album.images || undefined}
                  coverFallback="AL"
                  title={album.name}
                  subtitle={<span>
                      <span className="font-mono tabular-nums">{album.release_date?.split("-")[0]}</span>
                      {" · "}
                      <span>{getAlbumTypeBadgeLabel(album.album_type)}</span>
                      {album.total_tracks ? (<>
                          {" · "}
                          <span>{`${album.total_tracks} ${album.total_tracks === 1 ? t("translation.migrated.ArtistInfo.track") : t("translation.migrated.ArtistInfo.tracks")}`}</span>
                        </>) : null}
                    </span>}
                  explicit={album.is_explicit}
                  selected={isSelected}
                  onClick={handleSelectClick}
                  onDoubleClick={handleFetch}
                  trailing={hasTracks ? (<button
                      type="button"
                      className="cursor-pointer px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:text-primary"
                      onClick={handleFetch}
                    >
                      {t("translation.common.fetchAlbum")}
                    </button>) : null}
                />);
            })}
            </div>
            {filteredAlbums.length === 0 && (<div className="border border-dashed border-border p-6 text-sm text-muted-foreground">
                {t("translation.artistInfo.noReleasesFoundSelectedDiscography")}
              </div>)}
          </div>)}

        {activeTab === "gallery" && hasGallery && (<div className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.artistInfo.gallery")}({artistInfo.gallery!.length.toLocaleString()})</h3>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button onClick={() => void handleDownloadAllGallery()} size="icon-sm" variant="outline" disabled={downloadingAllGallery}>
                    {downloadingAllGallery ? <Spinner className="size-3.5"/> : <ImageDown className="size-3.5"/>}
                  </Button>
                </TooltipTrigger>
                <TooltipContent><p>{t("translation.artistInfo.downloadAllGallery")}</p></TooltipContent>
              </Tooltip>
            </div>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
              {artistInfo.gallery!.map((imageUrl, index) => (<div key={`${imageUrl}-${index}`} className="group relative">
                  <div className="relative aspect-square overflow-hidden rounded-[2px] bg-muted">
                    <img src={imageUrl} alt={t("translation.migrated.ArtistInfo.gallery", { value1: artistInfo.name, value2: index + 1 })} className="h-full w-full object-cover"/>
                    <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover:bg-black/40">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button onClick={() => void handleDownloadGalleryImage(imageUrl, index)} size="icon-sm" variant="secondary" disabled={downloadingGalleryIndex === index} className="opacity-0 transition-opacity group-hover:opacity-100">
                            {downloadingGalleryIndex === index ? (<Spinner />) : (<ImageDown className="size-3.5"/>)}
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent><p>{t("translation.artistInfo.downloadImage")} {index + 1}</p></TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                </div>))}
            </div>
          </div>)}

        {activeTab === "tracks" && trackList.length > 0 && (<div className="space-y-3">
            <div className="flex items-center justify-between gap-2">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.artistInfo.allTracks")}</h3>
              <div className="flex flex-wrap items-center gap-1.5">
                <Dialog>
                    <DialogTrigger asChild>
                        <Button variant="outline" size="sm">
                            {t("translation.migrated.ArtistInfo.filterAlbums")}
                        </Button>
                    </DialogTrigger>
                    <DialogContent className="flex h-[80vh] flex-col sm:max-w-125">
                        <DialogHeader>
                            <DialogTitle>{t("translation.migrated.ArtistInfo.selectAlbums")}</DialogTitle>
                        </DialogHeader>
                        <ScrollArea className="flex-1 pr-4">
                            <div className="space-y-4">
                                {filteredAlbumGroups.map(([albumKey, data]) => {
                    const tracksWithId = data.tracks.filter(t => t.spotify_id);
                    const isSelected = tracksWithId.length > 0 && tracksWithId.every(t => selectedTracks.includes(t.spotify_id!));
                    return (<div key={albumKey} className="flex items-start gap-3 p-2 transition-colors hover:bg-muted/50">
                                              <Checkbox id={`album-select-${albumKey}`} checked={isSelected} onCheckedChange={() => onToggleSelectAll(data.tracks)} className="mt-1"/>
                                              <div className="grid flex-1 gap-1.5 leading-none">
                                                  <label htmlFor={`album-select-${albumKey}`} className="cursor-pointer text-sm leading-none font-medium peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
                                                      {data.name}
                                                  </label>
                                                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                                      <span className="border bg-muted px-1.5 py-0.5 font-mono text-[10px] font-semibold">
                                                          {data.type}
                                                      </span>
                                                      <span>·</span>
                                                      <span>{data.count} {t("translation.artistInfo.tracks")}</span>
                                                      <span>·</span>
                                                      <span>{data.tracks[0]?.release_date?.split('-')[0] || t("translation.artistInfo.unknownYear")}</span>
                                                  </div>
                                              </div>
                                          </div>);
                })}
                            </div>
                        </ScrollArea>
                    </DialogContent>
                </Dialog>
                <Button size="sm" variant={allTracksQueued ? "outline" : "default"} onClick={onQueueAll}>
                  {allTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
                  {t(allTracksQueued ? "translation.downloads.requested" : "translation.downloads.download")}
                </Button>
                {selectedTracks.length > 0 && onQueueSelected && (<Button size="sm" variant="outline" onClick={onQueueSelected}>
                    {selectedTracksQueued ? (<CircleCheck className="size-3.5"/>) : (<Download className="size-3.5"/>)}
                    {selectedTracksQueued ? t("translation.downloads.requested") : t("translation.downloads.downloadSelected", { value1: selectedTracks.length.toLocaleString() })}
                  </Button>)}
                {onDownloadAllLyrics && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button size="icon-sm" variant="outline" disabled={isBulkDownloadingLyrics} onClick={onDownloadAllLyrics}>
                        {isBulkDownloadingLyrics ? <Spinner /> : <FileText className="size-3.5"/>}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent><p>{t("translation.common.downloadAllLyrics")}</p></TooltipContent>
                  </Tooltip>)}
                {onDownloadAllCovers && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button size="icon-sm" variant="outline" disabled={isBulkDownloadingCovers} onClick={onDownloadAllCovers}>
                        {isBulkDownloadingCovers ? <Spinner /> : <ImageDown className="size-3.5"/>}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent><p>{t("translation.common.downloadAllSeparateCovers")}</p></TooltipContent>
                  </Tooltip>)}
                {downloadedTracks.size > 0 && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button size="icon-sm" variant="outline" onClick={onOpenFolder}>
                        <FolderOpen className="size-3.5"/>
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent><p>{t("translation.common.openFolder")}</p></TooltipContent>
                  </Tooltip>)}
              </div>
            </div>
            <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
            <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} selectedTracks={selectedTracks} downloadedTracks={downloadedTracks} failedTracks={failedTracks} skippedTracks={skippedTracks} currentPage={currentPage} itemsPerPage={itemsPerPage} showCheckboxes={true} hideAlbumColumn={false} folderName={artistInfo.name} isArtistDiscography={true} downloadedLyrics={downloadedLyrics} failedLyrics={failedLyrics} skippedLyrics={skippedLyrics} downloadingLyricsTrack={downloadingLyricsTrack} checkingAvailabilityTrack={checkingAvailabilityTrack} availabilityMap={availabilityMap} onToggleTrack={onToggleTrack} onToggleSelectAll={onToggleSelectAll} onSelectTrackRange={onSelectTrackRange} onQueueTrack={onQueueTrack} onDownloadLyrics={onDownloadLyrics} onDownloadCover={onDownloadCover} downloadedCovers={downloadedCovers} failedCovers={failedCovers} skippedCovers={skippedCovers} downloadingCoverTrack={downloadingCoverTrack} onCheckAvailability={onCheckAvailability} onPageChange={onPageChange} onAlbumClick={onAlbumClick} onArtistClick={onArtistClick} onTrackClick={onTrackClick}/>
          </div>)}
      </div>

      <InspectorPane
        eyebrow={t("translation.common.artist")}
        title={artistInfo.name}
        subtitle={artistInfo.verified ? t("translation.catalog.verified") : undefined}
        cover={artistInfo.images || undefined}
        rows={[
            ...(artistInfo.rank ? [{ label: t("translation.catalog.rank"), value: <span className="font-mono tabular-nums">#{artistInfo.rank}</span> }] : []),
            { label: t("translation.catalog.followers"), value: <span className="font-mono tabular-nums">{artistInfo.followers.toLocaleString()}</span> },
            ...(artistInfo.listeners ? [{ label: t("translation.catalog.listeners"), value: <span className="font-mono tabular-nums">{artistInfo.listeners.toLocaleString()}</span> }] : []),
            { label: t("translation.common.albums"), value: <span className="font-mono tabular-nums">{displayedAlbumCount.toLocaleString()}</span> },
            { label: t("translation.artistInfo.tracks"), value: <span className="font-mono tabular-nums">{resolvedTrackCount.toLocaleString()}</span> },
            ...(artistInfo.genres.length > 0 ? [{ label: t("translation.catalog.genres"), value: <span className="truncate pl-4">{artistInfo.genres.join(", ")}</span> }] : []),
        ]}
        actions={<>
          {onBack ? (<Button variant="ghost" size="sm" className="-ml-2 h-8 px-2 text-xs text-muted-foreground" onClick={onBack}>
              {t("translation.common.back")}
            </Button>) : null}
          {artistInfo.images && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" disabled={downloadingAvatar} onClick={() => void handleDownloadAvatar()}>
                  {downloadingAvatar ? (<Spinner />) : (<ImageDown className="size-3.5"/>)}
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.artistInfo.downloadAvatar")}</p></TooltipContent>
            </Tooltip>)}
          {artistInfo.header && (<Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon-sm" disabled={downloadingHeader} onClick={() => void handleDownloadHeader()}>
                  {downloadingHeader ? (<Spinner />) : (<ImageDown className="size-3.5"/>)}
                </Button>
              </TooltipTrigger>
              <TooltipContent><p>{t("translation.artistInfo.downloadHeader")}</p></TooltipContent>
            </Tooltip>)}
        </>}
        footer={artistInfo.biography ? (<p className="line-clamp-4">{artistInfo.biography}</p>) : undefined}
      />
    </div>);
}
