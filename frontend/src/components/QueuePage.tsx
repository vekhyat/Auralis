import { t } from "@/i18n";
import { Fragment, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "@/components/ui/pagination";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Trash2, RotateCcw, CircleCheckBig, XCircle, Music2, ListOrdered } from "lucide-react";
import { isProtectedQueueStatus } from "@/lib/queue-guards";
import { canRetryQueuePersistence, queuePersistenceNotice } from "@/lib/queue-persistence";
import { getQueuePersistenceState, removeQueueItem, removeTrackFromQueueItem, retryQueueItem, retryQueuePersistence, subscribeQueuePersistence, type QueueItem, type QueueItemType, type QueuePersistenceState } from "@/lib/queue";
import type { TrackMetadata } from "@/types/api";
const ITEMS_PER_PAGE = 50;
interface QueuePageProps {
    items: QueueItem[];
    isSuspended?: boolean;
    onOpenLibrary?: () => void;
    onOpenFolder?: () => void;
    isProcessing: boolean;
    isPausing: boolean;
    processingType: QueueItemType | null;
    downloadedTracks: Set<string>;
    failedTracks: Set<string>;
    skippedTracks: Set<string>;
    downloadingTracks: Set<string>;
    onStart: (type?: QueueItemType) => void;
    onPause: (type?: QueueItemType) => void;
    onStop: (type?: QueueItemType) => void;
    isDirectDownloading?: boolean;
    onStopDirect?: () => void;
}
function formatDuration(ms?: number): string {
    if (!ms || ms <= 0)
        return "";
    const totalSeconds = Math.floor(ms / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) {
        return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
    }
    return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
function getPaginationPages(current: number, total: number): (number | "ellipsis")[] {
    if (total <= 10)
        return Array.from({ length: total }, (_, i) => i + 1);
    const pages: (number | "ellipsis")[] = [1];
    if (current <= 7) {
        for (let i = 2; i <= 10; i++)
            pages.push(i);
        pages.push("ellipsis");
        pages.push(total);
    }
    else if (current >= total - 7) {
        pages.push("ellipsis");
        for (let i = total - 9; i <= total; i++)
            pages.push(i);
    }
    else {
        pages.push("ellipsis", current - 1, current, current + 1, "ellipsis", total);
    }
    return pages;
}
export function QueuePage({ isSuspended = false, onOpenLibrary, onOpenFolder: _onOpenFolder, items, isProcessing: _isProcessing, isPausing, processingType: _processingType, downloadedTracks, failedTracks, skippedTracks, downloadingTracks, onStart: _onStart, onPause: _onPause, onStop: _onStop, isDirectDownloading: _isDirectDownloading, onStopDirect: _onStopDirect }: QueuePageProps) {
    const [currentPage, setCurrentPage] = useState(1);
    const [expandedIds, setExpandedIds] = useState<string[]>([]);
    const [persistence, setPersistence] = useState<QueuePersistenceState>(() => getQueuePersistenceState());
    useEffect(() => subscribeQueuePersistence(setPersistence), []);
    const toggleExpanded = (id: string) => {
        setExpandedIds((prev) => prev.includes(id) ? prev.filter((prevId) => prevId !== id) : [...prev, id]);
    };
    const getTrackStatus = (item: QueueItem, track: TrackMetadata): "downloading" | "skipped" | "done" | "failed" | "pending" => {
        const id = track.spotify_id;
        if (!id)
            return "pending";
        if (downloadingTracks.has(id))
            return "downloading";
        if (skippedTracks.has(id))
            return "skipped";
        if (failedTracks.has(id))
            return "failed";
        if (downloadedTracks.has(id))
            return "done";
        return item.trackResults?.[id] ?? "pending";
    };
    const renderTrackStatusIcon = (item: QueueItem, track: TrackMetadata) => {
        switch (getTrackStatus(item, track)) {
            case "downloading":
                return <Spinner className="size-3.5"/>;
            case "skipped":
                return <span className="text-xs text-muted-foreground">{t("translation.queue.skipped")}</span>;
            case "done":
                return <CircleCheckBig className="size-3.5"/>;
            case "failed":
                return <XCircle className="size-3.5 text-destructive"/>;
            default:
                return <div className="size-1.5 rounded-full bg-muted-foreground/40"/>;
        }
    };
    const getProgressSummary = (item: QueueItem) => {
        let done = 0;
        let failed = 0;
        for (const track of item.tracks) {
            const status = getTrackStatus(item, track);
            if (status === "done" || status === "skipped")
                done += 1;
            else if (status === "failed")
                failed += 1;
        }
        return { done, failed };
    };
    const filteredItems = items;
    const persistenceNotice = queuePersistenceNotice(persistence);
    const totalPages = Math.max(1, Math.ceil(filteredItems.length / ITEMS_PER_PAGE));
    const page = Math.min(currentPage, totalPages);
    const startIndex = (page - 1) * ITEMS_PER_PAGE;
    const paginated = filteredItems.slice(startIndex, startIndex + ITEMS_PER_PAGE);
    const renderStatus = (item: QueueItem) => {
        if (item.status === "running") {
            return (<div className="flex items-center justify-center gap-1.5 text-xs font-medium text-primary">
                    <Spinner className="size-3.5"/>
                    {isPausing ? t("translation.queue.pausing") : t("translation.queue.running")}</div>);
        }
        if (item.status === "paused") {
            return (<span className="text-xs text-muted-foreground">{t("translation.queue.paused")}</span>);
        }
        if (item.status === "done") {
            return (<span className="text-xs">{t("translation.queue.done")}</span>);
        }
        if (item.status === "partial")
            return (<span className="text-xs text-muted-foreground">{t("translation.queue.partial")}</span>);
        if (item.status === "skipped")
            return (<span className="text-xs text-muted-foreground">{t("translation.queue.skipped")}</span>);
        if (item.status === "failed") {
            return (<TooltipProvider>
                    <Tooltip delayDuration={0}>
                        <TooltipTrigger asChild>
                            <span className="text-xs text-destructive">
                                {t("translation.queue.failed")}</span>
                        </TooltipTrigger>
                        <TooltipContent>
                            <p className="max-w-xs wrap-break-word">{item.error || t("translation.queue.failed")}</p>
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>);
        }
        return (<span className="text-xs text-muted-foreground">{t("translation.queue.pending")}</span>);
    };
    return (<div className="space-y-5">
            <div>
                <h1 className="text-3xl font-semibold tracking-tight">{t("translation.downloads.title")}</h1>
                <p className="mt-2 text-sm text-muted-foreground">{t(isSuspended ? "translation.downloads.pausedHint" : "translation.downloads.autoHint")}</p>
            </div>
            {persistenceNotice !== "quiet" && (<div className="flex flex-wrap items-center justify-between gap-2 border border-border px-3 py-2">
                    <p className="text-sm">
                        {persistenceNotice === "unsaved" ? (<>
                            <span className="font-medium">{t("translation.lyricsManager.saveFailed")}</span>
                            <span className="text-muted-foreground">{" "}{t("translation.app.unsavedChanges")}</span>
                        </>) : (<span className="inline-flex items-center gap-2 text-muted-foreground">
                            <Spinner className="size-3.5"/>
                            {t("translation.common.loading2")}
                        </span>)}
                    </p>
                    {canRetryQueuePersistence(persistence) && (<Button variant="outline" onClick={() => retryQueuePersistence()} className="cursor-pointer gap-2">
                            <RotateCcw className="h-4 w-4"/>
                            {t("translation.queue.retry")}
                        </Button>)}
                </div>)}

            <div>
                {paginated.length === 0 ? (<div className="flex flex-col items-center justify-center gap-3 p-16 text-center text-muted-foreground">
                        <ListOrdered className="size-9 opacity-30"/>
                        {onOpenLibrary && <Button variant="outline" onClick={onOpenLibrary}>{t("translation.downloads.findMusic")}</Button>}
                        <div className="space-y-1">
                            <p className="font-medium text-foreground/80">{t("translation.downloads.empty")}</p>
                            <p className="text-sm">{t("translation.downloads.emptyHint")}</p>
                        </div>
                    </div>) : (<table className="w-full table-fixed">
                        <thead>
                            <tr className="border-b border-border">
                                <th className="h-9 w-12 px-3 text-center align-middle font-mono text-[10px] font-semibold tracking-widest uppercase text-muted-foreground">{"#"}</th>
                                <th className="h-9 w-[35%] px-3 text-left align-middle text-[10px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.common.title")}</th>
                                <th className="hidden h-9 px-3 text-left align-middle text-[10px] font-semibold tracking-widest uppercase text-muted-foreground md:table-cell">{t("translation.common.details")}</th>
                                <th className="hidden h-9 w-20 px-3 text-center align-middle whitespace-nowrap font-mono text-[10px] font-semibold tracking-widest uppercase text-muted-foreground lg:table-cell">{t("translation.common.tracks")}</th>
                                <th className="hidden h-9 w-20 px-3 text-left align-middle whitespace-nowrap font-mono text-[10px] font-semibold tracking-widest uppercase text-muted-foreground xl:table-cell">{t("translation.history.dur")}</th>
                                <th className="h-9 w-28 px-3 text-center align-middle whitespace-nowrap font-mono text-[10px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.queue.status")}</th>
                                <th className="h-9 w-24 px-3 text-center align-middle whitespace-nowrap font-mono text-[10px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.common.actions")}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {paginated.map((item, index) => {
                const canExpand = item.type !== "track";
                const isExpanded = canExpand && expandedIds.includes(item.id);
                const summary = getProgressSummary(item);
                return (<Fragment key={item.id}>
                                <tr onClick={canExpand ? () => toggleExpanded(item.id) : undefined} className={`border-b border-border transition-colors hover:bg-muted/60 ${canExpand ? "cursor-pointer select-none" : ""}`}>
                                    <td className="p-3 text-center align-middle font-mono text-xs tabular-nums text-muted-foreground">
                                        {startIndex + index + 1}
                                    </td>
                                    <td className="min-w-0 p-3 align-middle">
                                        <div className="flex min-w-0 items-center gap-3">
                                            <div className="size-12 shrink-0 overflow-hidden rounded-lg bg-secondary">
                                                {item.image ? (<img src={item.image} alt={item.name} loading="lazy" referrerPolicy="no-referrer" className="h-full w-full object-cover"/>) : (<div className="flex h-full w-full items-center justify-center bg-muted font-mono text-[9px] font-semibold text-muted-foreground">
                                                        {item.type.slice(0, 2).toUpperCase()}
                                                    </div>)}
                                            </div>
                                            <div className="flex flex-col min-w-0 flex-1">
                                                <span className="font-medium text-sm truncate">{item.name}</span>
                                                <span className="text-xs text-muted-foreground truncate">{item.artist}</span>
                                            </div>
                                        </div>
                                    </td>
                                    <td className="hidden p-3 align-middle text-sm text-muted-foreground md:table-cell">
                                        <div className="truncate">{item.type === "track" ? item.info : ""}</div>
                                    </td>
                                    <td className="hidden p-3 text-center align-middle font-mono text-xs tabular-nums text-muted-foreground lg:table-cell">
                                        <div className="flex flex-col items-center">
                                            <span>{item.trackCount.toLocaleString("en-US")}</span>
                                            {canExpand && (summary.done > 0 || summary.failed > 0) && (<span className="text-[10px] leading-none">
                                                <span>{summary.done}</span>
                                                {summary.failed > 0 && (<span className="text-destructive">{t("translation.queue.value1Value2", { value1: "", value2: summary.failed })}</span>)}
                                            </span>)}
                                        </div>
                                    </td>
                                    <td className="hidden p-3 align-middle font-mono text-xs tabular-nums text-muted-foreground xl:table-cell">
                                        {formatDuration(item.durationMs)}
                                    </td>
                                    <td className="p-3 align-middle text-center">
                                        {renderStatus(item)}
                                    </td>
                                    <td className="p-3 align-middle text-center">
                                        <div className="flex items-center justify-center gap-1">
                                            {(item.status === "failed" || item.status === "partial") && (<TooltipProvider>
                                                <Tooltip delayDuration={0}>
                                                    <TooltipTrigger asChild>
                                                        <Button variant="ghost" size="icon" className="cursor-pointer" aria-label={t("translation.queue.retry")} onClick={() => retryQueueItem(item.id)}>
                                                            <RotateCcw className="h-4 w-4"/>
                                                        </Button>
                                                    </TooltipTrigger>
                                                    <TooltipContent>
                                                        <p>{t("translation.queue.retry")}</p>
                                                    </TooltipContent>
                                                </Tooltip>
                                            </TooltipProvider>)}
                                            <TooltipProvider>
                                                <Tooltip delayDuration={0}>
                                                    <TooltipTrigger asChild>
                                                        <Button variant="ghost" size="icon" className="cursor-pointer text-destructive hover:text-destructive" aria-label={t("translation.downloads.cancelRequest")} onClick={() => {
                    if (!isProtectedQueueStatus(item.status))
                        removeQueueItem(item.id);
                }} disabled={isProtectedQueueStatus(item.status)}>
                                                            <Trash2 className="h-4 w-4"/>
                                                        </Button>
                                                    </TooltipTrigger>
                                                    <TooltipContent>
                                                        <p>{t("translation.downloads.cancelRequest")}</p>
                                                    </TooltipContent>
                                                </Tooltip>
                                            </TooltipProvider>
                                        </div>
                                    </td>
                                </tr>
                                
                                {isExpanded && item.tracks.length === 0 && (<tr className="border-b bg-muted/20">
                                    <td className="py-2 px-3 align-middle"/>
                                    <td className="py-2 px-3 align-middle text-sm text-muted-foreground">
                                        {t("translation.queue.noTracksItem")}
                                    </td>
                                    <td className="py-2 px-3 align-middle hidden md:table-cell"/>
                                    <td className="py-2 px-3 align-middle hidden lg:table-cell"/>
                                    <td className="py-2 px-3 align-middle hidden xl:table-cell"/>
                                    <td className="py-2 px-3 align-middle"/>
                                    <td className="py-2 px-3 align-middle"/>
                                </tr>)}
                                {isExpanded && item.tracks.map((track, trackIndex) => (<tr key={track.spotify_id || `${item.id}-${trackIndex}`} className="border-b border-border/60 bg-muted/30">
                                    <td className="px-3 py-2 text-center align-middle font-mono text-xs tabular-nums text-muted-foreground">
                                        {trackIndex + 1}
                                    </td>
                                    <td className="min-w-0 px-3 py-2 align-middle">
                                        <div className="flex min-w-0 items-center gap-3">
                                            <div className="size-6 shrink-0 overflow-hidden rounded-[2px] bg-secondary">
                                                {track.images ? (<img src={track.images} alt={track.name} loading="lazy" referrerPolicy="no-referrer" className="h-full w-full object-cover"/>) : (<div className="flex h-full w-full items-center justify-center bg-muted">
                                                    <Music2 className="size-3 text-muted-foreground opacity-50"/>
                                                </div>)}
                                            </div>
                                            <div className="flex flex-col min-w-0 flex-1">
                                                <span className="text-sm truncate">{track.name}</span>
                                                <span className="text-xs text-muted-foreground truncate">{track.artists}</span>
                                            </div>
                                        </div>
                                    </td>
                                    <td className="py-2 px-3 align-middle text-xs text-muted-foreground hidden md:table-cell">
                                        <div className="truncate">{track.album_name}</div>
                                    </td>
                                    <td className="py-2 px-3 align-middle hidden lg:table-cell"/>
                                    <td className="py-2 px-3 align-middle text-xs text-muted-foreground hidden xl:table-cell font-mono">
                                        {formatDuration(track.duration_ms)}
                                    </td>
                                    <td className="py-2 px-3 align-middle text-center">
                                        <div className="flex items-center justify-center">
                                            {renderTrackStatusIcon(item, track)}
                                        </div>
                                    </td>
                                    <td className="py-2 px-3 align-middle text-center">
                                        <TooltipProvider>
                                            <Tooltip delayDuration={0}>
                                                <TooltipTrigger asChild>
                                                    <Button variant="ghost" size="icon" className="cursor-pointer text-destructive hover:text-destructive" aria-label={t("translation.downloads.cancelRequest")} onClick={() => {
                        if (!isProtectedQueueStatus(item.status))
                            removeTrackFromQueueItem(item.id, trackIndex);
                    }} disabled={isProtectedQueueStatus(item.status)}>
                                                        <Trash2 className="h-4 w-4"/>
                                                    </Button>
                                                </TooltipTrigger>
                                                <TooltipContent>
                                                    <p>{t("translation.downloads.cancelRequest")}</p>
                                                </TooltipContent>
                                            </Tooltip>
                                        </TooltipProvider>
                                    </td>
                                </tr>))}
                            </Fragment>);
            })}
                        </tbody>
                    </table>)}
            </div>

            {totalPages > 1 && (<Pagination>
                    <PaginationContent>
                        <PaginationItem>
                            <PaginationPrevious href="#" onClick={(e) => {
                e.preventDefault();
                if (page > 1)
                    setCurrentPage(page - 1);
            }} className={page === 1 ? "pointer-events-none opacity-50" : "cursor-pointer"}/>
                        </PaginationItem>

                        {(() => {
                let ellipsisCount = 0;
                return getPaginationPages(page, totalPages).map((pageNumber) => (pageNumber === "ellipsis" ? (<PaginationItem key={`ellipsis-queue-${page}-${ellipsisCount++}`}>
                                    <PaginationEllipsis />
                                </PaginationItem>) : (<PaginationItem key={pageNumber}>
                                    <PaginationLink href="#" onClick={(e) => {
                        e.preventDefault();
                        setCurrentPage(pageNumber);
                    }} isActive={page === pageNumber} className="cursor-pointer">
                                        {pageNumber}
                                    </PaginationLink>
                                </PaginationItem>)));
            })()}

                        <PaginationItem>
                            <PaginationNext href="#" onClick={(e) => {
                e.preventDefault();
                if (page < totalPages)
                    setCurrentPage(page + 1);
            }} className={page === totalPages ? "pointer-events-none opacity-50" : "cursor-pointer"}/>
                        </PaginationItem>
                    </PaginationContent>
                </Pagination>)}
        </div>);
}
