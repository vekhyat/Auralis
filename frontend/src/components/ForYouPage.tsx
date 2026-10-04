import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
    Sparkles,
    Download,
    MoreVertical,
    EyeOff,
    UserX,
    Pin,
    PinOff,
    RefreshCw,
    SlidersHorizontal,
    Search,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { CoverArt } from "@/components/ArtworkCard";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import {
    GetTasteShelves,
    GetTasteSummary,
    DismissTasteItem,
    BanTasteArtist,
    PinTasteArtist,
    UnpinTasteArtist,
    SyncTasteNow,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { taste } from "../../wailsjs/go/models";

interface ForYouPageProps {
    onDownloadUrl: (url: string) => void;
    /** Artists from Last.fm have no Spotify ID, so they open a search. */
    onSearch: (query: string) => void;
    onNavigateToSettings?: () => void;
}

interface TasteShelf {
    id: string;
    seed: string;
    items: taste.Item[];
}

const SHELF_TITLE_KEYS: Record<string, string> = {
    "liked-not-downloaded": "translation.forYou.shelfLiked",
    "finish-albums": "translation.forYou.shelfFinishAlbums",
    "similar-artists": "translation.forYou.shelfSimilar",
    "discography-gaps": "translation.forYou.shelfDiscographyGaps",
};

const REASON_KEYS: Record<string, string> = {
    saved_spotify: "translation.forYou.reasonSavedSpotify",
    loved_lastfm: "translation.forYou.reasonLovedLastfm",
    plays_a_lot: "translation.forYou.reasonPlaysALot",
    saved_album: "translation.forYou.reasonSavedAlbum",
    liked_tracks: "translation.forYou.reasonLikedTracks",
    similar_to: "translation.forYou.reasonSimilarTo",
    missing_album: "translation.forYou.reasonMissingAlbum",
};

/** Spotify URL the existing download path can queue, or null for artists. */
function downloadUrl(item: taste.Item): string | null {
    if (item.kind === "album" && item.album_id) {
        return `https://open.spotify.com/album/${item.album_id}`;
    }
    if (item.kind === "track" && item.spotify_id) {
        return `https://open.spotify.com/track/${item.spotify_id}`;
    }
    return null;
}

interface SyncProgressEvent {
    phase: string;
    message: string;
    current: number;
    total: number;
    done: boolean;
    error?: string;
}

export function ForYouPage({ onDownloadUrl, onSearch, onNavigateToSettings }: ForYouPageProps) {
    const { t } = useTranslation();
    const [shelves, setShelves] = useState<TasteShelf[]>([]);
    const [summary, setSummary] = useState<taste.Summary | null>(null);
    const [loading, setLoading] = useState(true);
    const [syncing, setSyncing] = useState(false);
    const [downloadingIds, setDownloadingIds] = useState<Set<string>>(new Set());

    const loadData = useCallback(async () => {
        try {
            const [shelvesData, summaryData] = await Promise.all([
                GetTasteShelves(),
                GetTasteSummary(),
            ]);
            setShelves(
                (shelvesData || []).map((s) => ({
                    id: s.id,
                    seed: s.seed || "",
                    items: s.items || [],
                }))
            );
            setSummary(summaryData || null);
        } catch (err) {
            console.error("Failed to load taste shelves:", err);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        const timer = window.setTimeout(() => {
            void loadData();
        }, 0);

        const unsubSync = EventsOn("taste:sync-progress", (p: SyncProgressEvent) => {
            if (p.done) {
                setSyncing(false);
                if (p.error) {
                    toast.error(t("translation.connections.syncError", { error: p.error }));
                }
                void loadData();
            }
        });

        return () => {
            window.clearTimeout(timer);
            unsubSync();
        };
    }, [loadData, t]);

    const shelfTitle = (shelf: TasteShelf) =>
        shelf.id === "similar-artists" && shelf.seed
            ? t("translation.forYou.shelfBecauseYouListen", { artist: shelf.seed })
            : t(SHELF_TITLE_KEYS[shelf.id] ?? "translation.forYou.title");

    const reasonText = (item: taste.Item) => {
        const key = item.reason_code ? REASON_KEYS[item.reason_code] : undefined;
        if (!key) return item.reason;
        return t(key, { artist: item.reason_arg || item.artist, count: Number(item.reason_arg) || 0 });
    };

    const handleDownloadItem = (item: taste.Item) => {
        const url = downloadUrl(item);
        if (!url) {
            onSearch(item.artist);
            return;
        }
        setDownloadingIds((prev) => new Set(prev).add(item.id));
        onDownloadUrl(url);
        toast.success(
            t("translation.forYou.downloading", {
                title: item.title,
                artist: item.artist,
            })
        );
        setTimeout(() => {
            setDownloadingIds((prev) => {
                const next = new Set(prev);
                next.delete(item.id);
                return next;
            });
        }, 1500);
    };

    const handleDownloadShelf = (shelf: TasteShelf) => {
        const urls = shelf.items.map(downloadUrl).filter((url): url is string => url !== null);
        if (urls.length === 0) return;
        urls.forEach(onDownloadUrl);
        toast.success(
            t("translation.forYou.shelfEnqueued", {
                count: urls.length,
                shelf: shelfTitle(shelf),
            })
        );
    };

    const handleDismissItem = async (itemId: string) => {
        try {
            await DismissTasteItem(itemId);
            setShelves((prev) =>
                prev.map((s) => ({
                    ...s,
                    items: s.items.filter((it) => it.id !== itemId),
                }))
            );
            toast.info(t("translation.forYou.itemDismissed"));
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleBanArtist = async (artist: string) => {
        try {
            await BanTasteArtist(artist);
            setShelves((prev) =>
                prev.map((s) => ({
                    ...s,
                    items: s.items.filter((it) => it.artist.toLowerCase() !== artist.toLowerCase()),
                }))
            );
            toast.info(t("translation.forYou.artistBanned", { artist }));
            void loadData();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handlePinArtist = async (artist: string) => {
        try {
            await PinTasteArtist(artist);
            toast.success(t("translation.forYou.artistPinned", { artist }));
            void loadData();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleUnpinArtist = async (artist: string) => {
        try {
            await UnpinTasteArtist(artist);
            toast.info(t("translation.forYou.artistUnpinned", { artist }));
            void loadData();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleSync = async () => {
        setSyncing(true);
        try {
            await SyncTasteNow();
        } catch (err) {
            setSyncing(false);
            toast.error(String(err));
        }
    };

    const hasAnyItems = shelves.some((s) => s.items && s.items.length > 0);
    const pinned = new Set((summary?.pinned_artists ?? []).map((name) => name.toLowerCase()));

    if (loading) {
        return (
            <div className="flex min-h-[400px] items-center justify-center">
                <RefreshCw className="size-6 animate-spin text-muted-foreground" />
            </div>
        );
    }

    return (
        <div className="mx-auto w-full max-w-[1600px] px-6 py-8 space-y-8">
            {/* Header & Controls */}
            <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border pb-4">
                <div>
                    <div className="flex items-center gap-2">
                        <Sparkles className="size-6 text-primary" />
                        <h1 className="text-2xl font-semibold tracking-tight">
                            {t("translation.forYou.title")}
                        </h1>
                    </div>
                    <p className="text-xs text-muted-foreground mt-1 max-w-[70ch]">
                        {t("translation.forYou.subtitle")}
                    </p>
                </div>

                <div className="flex items-center gap-2">
                    <Button
                        variant="outline"
                        size="sm"
                        disabled={syncing}
                        onClick={handleSync}
                        className="gap-1.5"
                    >
                        <RefreshCw className={`size-3.5 ${syncing ? "animate-spin" : ""}`} />
                        {t("translation.forYou.sync")}
                    </Button>
                    {onNavigateToSettings ? (
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={onNavigateToSettings}
                            className="gap-1.5"
                        >
                            <SlidersHorizontal className="size-3.5" />
                            {t("translation.forYou.settings")}
                        </Button>
                    ) : null}
                </div>
            </div>

            {/* Layout: Main Shelves + Taste Side Panel */}
            <div className="grid grid-cols-1 xl:grid-cols-4 gap-8">
                {/* Shelves Column */}
                <div className="xl:col-span-3 space-y-10">
                    {!hasAnyItems ? (
                        <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border bg-card/40 p-12 text-center">
                            <Sparkles className="size-12 text-muted-foreground/60 mb-4" strokeWidth={1.5} />
                            <h2 className="text-lg font-semibold">{t("translation.forYou.emptyTitle")}</h2>
                            <p className="mt-1.5 max-w-[50ch] text-xs text-muted-foreground">
                                {t("translation.forYou.emptyDesc")}
                            </p>
                            {onNavigateToSettings ? (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    onClick={onNavigateToSettings}
                                    className="mt-6"
                                >
                                    {t("translation.forYou.configureConnections")}
                                </Button>
                            ) : null}
                        </div>
                    ) : (
                        shelves.map((shelf) => {
                            if (!shelf.items || shelf.items.length === 0) return null;
                            return (
                                <section key={shelf.id} className="space-y-4">
                                    <div className="flex items-center justify-between">
                                        <div className="flex items-baseline gap-2">
                                            <h2 className="text-base font-semibold tracking-tight">
                                                {shelfTitle(shelf)}
                                            </h2>
                                            <span className="text-xs text-muted-foreground">
                                                ({shelf.items.length})
                                            </span>
                                        </div>
                                        {shelf.items.some((item) => downloadUrl(item) !== null) ? <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => handleDownloadShelf(shelf)}
                                            className="h-7 text-xs text-muted-foreground hover:text-foreground gap-1.5"
                                        >
                                            <Download className="size-3.5" />
                                            {t("translation.forYou.downloadShelf")}
                                        </Button> : null}
                                    </div>

                                    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3.5">
                                        {shelf.items.map((item) => (
                                            <div
                                                key={item.id}
                                                className="group relative flex flex-col justify-between rounded-xl border border-border/50 bg-card p-2.5 transition-all duration-200 hover:-translate-y-1 hover:border-border hover:shadow-sm motion-reduce:transform-none"
                                            >
                                                <div className="space-y-2">
                                                    <div className="relative aspect-square w-full overflow-hidden rounded-lg">
                                                        <CoverArt
                                                            src={item.image}
                                                            className="size-full object-cover transition-transform duration-200 group-hover:scale-105"
                                                        />
                                                    </div>

                                                    <div className="space-y-0.5">
                                                        <div
                                                            className="truncate text-xs font-semibold text-foreground"
                                                            title={item.title}
                                                        >
                                                            {item.title}
                                                        </div>
                                                        <div
                                                            className="truncate text-[11px] text-muted-foreground"
                                                            title={item.artist}
                                                        >
                                                            {item.artist}
                                                        </div>
                                                    </div>

                                                    {reasonText(item) ? (
                                                        <div
                                                            className="inline-block max-w-full truncate rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground"
                                                            title={reasonText(item)}
                                                        >
                                                            {reasonText(item)}
                                                        </div>
                                                    ) : null}
                                                </div>

                                                <div className="mt-3 flex items-center justify-between gap-1 pt-2 border-t border-border/40">
                                                    <Button
                                                        variant="secondary"
                                                        size="sm"
                                                        disabled={downloadingIds.has(item.id)}
                                                        onClick={() => handleDownloadItem(item)}
                                                        className="h-7 flex-1 text-xs gap-1 font-normal"
                                                    >
                                                        {downloadUrl(item) ? <Download className="size-3" /> : <Search className="size-3" />}
                                                        {downloadingIds.has(item.id)
                                                            ? t("translation.forYou.queued")
                                                            : downloadUrl(item)
                                                                ? t("translation.common.download")
                                                                : t("translation.forYou.findArtist")}
                                                    </Button>

                                                    <DropdownMenu>
                                                        <DropdownMenuTrigger asChild>
                                                            <Button
                                                                variant="ghost"
                                                                size="icon"
                                                                aria-label={t("translation.common.more")}
                                                                className="size-7 text-muted-foreground hover:text-foreground"
                                                            >
                                                                <MoreVertical className="size-3.5" />
                                                            </Button>
                                                        </DropdownMenuTrigger>
                                                        <DropdownMenuContent align="end" className="w-44">
                                                            <DropdownMenuItem
                                                                onClick={() => handleDismissItem(item.id)}
                                                                className="cursor-pointer text-xs"
                                                            >
                                                                <EyeOff className="size-3.5 mr-2" />
                                                                {t("translation.forYou.dismiss")}
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem
                                                                onClick={() => handlePinArtist(item.artist)}
                                                                className="cursor-pointer text-xs"
                                                            >
                                                                <Pin className="size-3.5 mr-2" />
                                                                {t("translation.forYou.pinArtist")}
                                                            </DropdownMenuItem>
                                                            <DropdownMenuItem
                                                                variant="destructive"
                                                                onClick={() => handleBanArtist(item.artist)}
                                                                className="cursor-pointer text-xs"
                                                            >
                                                                <UserX className="size-3.5 mr-2" />
                                                                {t("translation.forYou.notInterestedArtist")}
                                                            </DropdownMenuItem>
                                                        </DropdownMenuContent>
                                                    </DropdownMenu>
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                </section>
                            );
                        })
                    )}
                </div>

                {/* Taste Overview Side Panel */}
                <div className="space-y-6">
                    <div className="rounded-xl border border-border bg-card p-4 space-y-5">
                        <div className="flex items-center gap-2 border-b border-border pb-3">
                            <Sparkles className="size-4 text-primary" />
                            <h3 className="text-sm font-semibold tracking-tight">
                                {t("translation.forYou.yourTaste")}
                            </h3>
                        </div>

                        {/* Top Artists */}
                        <div className="space-y-2.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                {t("translation.forYou.topArtists")}
                            </span>
                            {summary?.top_artists && summary.top_artists.length > 0 ? (
                                <ul className="space-y-1.5">
                                    {summary.top_artists.slice(0, 8).map((artist, idx) => (
                                        <li
                                            key={artist}
                                            className="group flex items-center justify-between text-xs py-1 px-1.5 rounded hover:bg-muted/50"
                                        >
                                            <div className="flex items-center gap-2 truncate">
                                                <span className="text-[10px] font-mono text-muted-foreground w-3.5 text-right">
                                                    {idx + 1}
                                                </span>
                                                <span className="truncate font-medium text-foreground">
                                                    {artist}
                                                </span>
                                            </div>
                                            <div className="opacity-0 group-hover:opacity-100 flex items-center gap-1 transition-opacity">
                                                {pinned.has(artist.toLowerCase()) ? (
                                                    <button
                                                        type="button"
                                                        title={t("translation.forYou.unpinArtist")}
                                                        aria-label={t("translation.forYou.unpinArtist")}
                                                        onClick={() => handleUnpinArtist(artist)}
                                                        className="p-1 rounded text-primary cursor-pointer"
                                                    >
                                                        <PinOff className="size-3" />
                                                    </button>
                                                ) : (
                                                    <button
                                                        type="button"
                                                        title={t("translation.forYou.pinArtist")}
                                                        aria-label={t("translation.forYou.pinArtist")}
                                                        onClick={() => handlePinArtist(artist)}
                                                        className="p-1 hover:text-primary rounded text-muted-foreground cursor-pointer"
                                                    >
                                                        <Pin className="size-3" />
                                                    </button>
                                                )}
                                                <button
                                                    type="button"
                                                    title={t("translation.forYou.notInterestedArtist")}
                                                    aria-label={t("translation.forYou.notInterestedArtist")}
                                                    onClick={() => handleBanArtist(artist)}
                                                    className="p-1 hover:text-destructive rounded text-muted-foreground cursor-pointer"
                                                >
                                                    <UserX className="size-3" />
                                                </button>
                                            </div>
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <p className="text-xs text-muted-foreground italic">
                                    {t("translation.forYou.noTasteData")}
                                </p>
                            )}
                        </div>

                        {/* Top Genres */}
                        <div className="space-y-2.5 border-t border-border pt-3">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                {t("translation.forYou.topGenres")}
                            </span>
                            {summary?.top_genres && summary.top_genres.length > 0 ? (
                                <div className="flex flex-wrap gap-1.5">
                                    {summary.top_genres.slice(0, 8).map((genre) => (
                                        <span
                                            key={genre}
                                            className="rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground capitalize"
                                        >
                                            {genre}
                                        </span>
                                    ))}
                                </div>
                            ) : (
                                <p className="text-xs text-muted-foreground italic">
                                    {t("translation.forYou.noGenreData")}
                                </p>
                            )}
                        </div>

                        {/* Metadata summary */}
                        <div className="border-t border-border pt-3 text-[11px] text-muted-foreground space-y-1">
                            <div>
                                {t("translation.forYou.eventsAnalyzed", {
                                    count: summary?.event_count ?? 0,
                                })}
                            </div>
                            {summary?.last_sync ? (
                                <div>
                                    {t("translation.forYou.syncedAt", {
                                        time: new Date(summary.last_sync).toLocaleString(),
                                    })}
                                </div>
                            ) : null}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
