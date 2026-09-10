import { t } from "@/i18n";
import { Fragment, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "@/components/ui/pagination";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Search, Filter, Trash2, Play, Pause, StopCircle, RotateCcw, CircleCheckBig, XCircle, Music2, ListOrdered, Eraser } from "lucide-react";
import { clearFinishedQueueItems, clearQueue, removeQueueItem, removeTrackFromQueueItem, retryQueueItem, type QueueItem, type QueueItemType } from "@/lib/queue";
import type { TrackMetadata } from "@/types/api";
const TABS: Array<{
    value: QueueItemType;
    label: string;
}> = [
    { value: "track", label: "translation.common.tracks" },
    { value: "album", label: "translation.common.albums" },
    { value: "playlist", label: "translation.common.playlists" },
    { value: "artist", label: "translation.common.artists" },
];
const ITEMS_PER_PAGE = 50;
type StatusFilter = "all" | "pending" | "running" | "paused" | "done" | "partial" | "skipped" | "failed";
interface QueuePageProps {
    items: QueueItem[];
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
export function QueuePage({ items, isProcessing, isPausing, processingType, downloadedTracks, failedTracks, skippedTracks, downloadingTracks, onStart, onPause, onStop, isDirectDownloading = false, onStopDirect }: QueuePageProps) {
    const [activeTab, setActiveTab] = useState<QueueItemType>(() => items.find((item) => item.status === "running" || item.status === "paused" || item.status === "pending")?.type || "track");
    const [searchQuery, setSearchQuery] = useState("");
    const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
    const [currentPage, setCurrentPage] = useState(1);
    const [showClearConfirm, setShowClearConfirm] = useState(false);
    const [expandedIds, setExpandedIds] = useState<string[]>([]);
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
                return <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{t("translation.queue.skipped")}</span>;
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
    const tabItems = items.filter((item) => item.type === activeTab);
    const statusMatches = (item: QueueItem) => {
        if (statusFilter === "all")
            return true;
        if (statusFilter === "failed")
            return item.status === "failed" || item.status === "partial" || Object.values(item.trackResults || {}).includes("failed");
        if (statusFilter === "skipped")
            return item.status === "skipped" || Object.values(item.trackResults || {}).includes("skipped");
        return item.status === statusFilter;
    };
    const statusItems = tabItems.filter(statusMatches);
    const filteredItems = searchQuery
        ? statusItems.filter((item) => {
            const query = searchQuery.toLowerCase();
            return (item.name.toLowerCase().includes(query) ||
                item.artist.toLowerCase().includes(query) ||
                item.info.toLowerCase().includes(query));
        })
        : statusItems;
    const pendingCount = items.filter((item) => item.status === "pending").length;
    const pausedCount = items.filter((item) => item.status === "paused").length;
    const runnableCount = pendingCount + pausedCount;
    const tabPendingCount = tabItems.filter((item) => item.status === "pending").length;
    const tabPausedCount = tabItems.filter((item) => item.status === "paused").length;
    const tabRunnableCount = tabPendingCount + tabPausedCount;
    const finishedCount = tabItems.filter((item) => ["done", "partial", "skipped", "failed"].includes(item.status)).length;
    const isTabRunning = isProcessing && (processingType === activeTab || processingType === null);
    const totalPages = Math.max(1, Math.ceil(filteredItems.length / ITEMS_PER_PAGE));
    const page = Math.min(currentPage, totalPages);
    const startIndex = (page - 1) * ITEMS_PER_PAGE;
    const paginated = filteredItems.slice(startIndex, startIndex + ITEMS_PER_PAGE);
    const handleTabChange = (value: QueueItemType) => {
        setActiveTab(value);
        setCurrentPage(1);
    };
    const handleStart = (type?: QueueItemType) => {
        const nextType = type || items.find((item) => item.status === "paused" || item.status === "pending")?.type;
        if (nextType) {
            setActiveTab(nextType);
            setCurrentPage(1);
        }
        onStart(type);
    };
    const handleClearTab = () => {
        clearQueue(activeTab);
        setShowClearConfirm(false);
    };
    const renderStatus = (item: QueueItem) => {
        if (item.status === "running") {
            return (<div className="flex items-center justify-center gap-1.5 text-xs font-medium text-primary">
                    <Spinner className="size-3.5"/>
                    {isPausing ? t("translation.queue.pausing") : t("translation.queue.running")}</div>);
        }
        if (item.status === "paused") {
            return (<span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{t("translation.queue.paused")}</span>);
        }
        if (item.status === "done") {
            return (<span className="font-mono text-[10px] uppercase tracking-wider">{t("translation.queue.done")}</span>);
        }
        if (item.status === "partial")
            return (<span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{t("translation.queue.partial")}</span>);
        if (item.status === "skipped")
            return (<span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{t("translation.queue.skipped")}</span>);
        if (item.status === "failed") {
            return (<TooltipProvider>
                    <Tooltip delayDuration={0}>
                        <TooltipTrigger asChild>
                            <span className="font-mono text-[10px] uppercase tracking-wider text-destructive">
                                {t("translation.queue.failed")}</span>
                        </TooltipTrigger>
                        <TooltipContent>
                            <p className="max-w-xs wrap-break-word">{item.error || t("translation.queue.failed")}</p>
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>);
        }
        return (<span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{t("translation.queue.pending")}</span>);
    };
    return (<div className="space-y-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <h1 className="text-lg font-semibold tracking-tight">{t("translation.queue.queue")}</h1>
                <div className="flex items-center gap-2">
                    {isProcessing ? (<>
                        <Button variant="outline" onClick={() => onPause()} disabled={isPausing} className="cursor-pointer gap-2">
                            <Pause className="h-4 w-4"/>
                            {isPausing ? t("translation.queue.pausing") : t("translation.queue.pauseAll")}
                        </Button>
                        <Button variant="destructive" onClick={() => onStop()} className="cursor-pointer gap-2">
                            <StopCircle className="h-4 w-4"/>
                            {t("translation.queue.stopAll")}
                        </Button>
                    </>) : isDirectDownloading ? (<>
                        <Button variant="destructive" onClick={() => onStopDirect?.()} className="cursor-pointer gap-2">
                            <StopCircle className="h-4 w-4"/>
                            {t("translation.common.stop")}
                        </Button>
                    </>) : (<Button onClick={() => handleStart()} disabled={runnableCount === 0} className="cursor-pointer gap-2">
                            <Play className="h-4 w-4"/>
                            {pausedCount > 0 ? t("translation.queue.resumeAll") : t("translation.queue.startAll")}
                        </Button>)}
                </div>
            </div>

            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border pb-2">
                {TABS.map((tab) => {
            const count = items.filter((item) => item.type === tab.value).length;
            return (<button key={tab.value} type="button" onClick={() => handleTabChange(tab.value)} className={`cursor-pointer text-[13px] transition-colors ${activeTab === tab.value ? "font-semibold text-primary underline decoration-primary underline-offset-[6px]" : "text-muted-foreground hover:text-foreground"}`}>
                            {t(tab.label)}
                            {count > 0 && (<span className="ml-1 font-mono text-[11px] tabular-nums opacity-75">{count.toLocaleString("en-US")}</span>)}
                        </button>);
        })}
            </div>

            <div className="flex items-center gap-2">
                <div className="relative flex-1">
                    <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground"/>
                    <Input placeholder={t("translation.queue.searchQueue")} value={searchQuery} onChange={(e) => {
            setSearchQuery(e.target.value);
            setCurrentPage(1);
        }} className="pl-8 h-9"/>
                </div>
                <Select value={statusFilter} onValueChange={(value) => { setStatusFilter(value as StatusFilter); setCurrentPage(1); }}>
                    <SelectTrigger className="h-9 min-w-36">
                        <Filter className="h-4 w-4 text-muted-foreground"/>
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent align="end">
                        <SelectItem value="all">{t("translation.queue.allStatuses")}</SelectItem>
                        <SelectItem value="pending">{t("translation.queue.pending")}</SelectItem>
                        <SelectItem value="running">{t("translation.queue.running")}</SelectItem>
                        <SelectItem value="paused">{t("translation.queue.paused")}</SelectItem>
                        <SelectItem value="done">{t("translation.queue.done")}</SelectItem>
                        <SelectItem value="partial">{t("translation.queue.partial")}</SelectItem>
                        <SelectItem value="skipped">{t("translation.queue.skipped")}</SelectItem>
                        <SelectItem value="failed">{t("translation.queue.failed")}</SelectItem>
                    </SelectContent>
                </Select>
                <Button variant="outline" onClick={() => clearFinishedQueueItems(activeTab)} disabled={finishedCount === 0} className="cursor-pointer gap-2">
                    <Eraser className="h-4 w-4"/>
                    {t("translation.queue.clearFinished")}</Button>
                <Button variant="destructive" onClick={() => setShowClearConfirm(true)} disabled={tabItems.length === 0} className="cursor-pointer gap-2">
                    <Trash2 className="h-4 w-4"/>
                    {t("translation.common.clearAll")}</Button>
                {isTabRunning ? (<>
                    <Button variant="outline" onClick={() => onPause(activeTab)} disabled={isPausing} className="cursor-pointer gap-2">
                        <Pause className="h-4 w-4"/>
                        {isPausing ? t("translation.queue.pausing") : t("translation.queue.pause")}
                    </Button>
                    <Button variant="destructive" onClick={() => onStop(activeTab)} className="cursor-pointer gap-2">
                        <StopCircle className="h-4 w-4"/>
                        {t("translation.common.stop")}
                    </Button>
                </>) : (<Button onClick={() => handleStart(activeTab)} disabled={isProcessing || isDirectDownloading || tabRunnableCount === 0} className="cursor-pointer gap-2">
                        <Play className="h-4 w-4"/>
                        {tabPausedCount > 0 ? t("translation.queue.resume") : t("translation.queue.start")}
                    </Button>)}
            </div>

            <div>
                {paginated.length === 0 ? (<div className="flex flex-col items-center justify-center gap-3 p-16 text-center text-muted-foreground">
                        <ListOrdered className="size-9 opacity-30"/>
                        <div className="space-y-1">
                            <p className="font-medium text-foreground/80">{t("translation.queue.emptyQueue")}</p>
                            <p className="text-sm">{t("translation.queue.addToQueueHint")}</p>
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
                                            <div className="size-7 shrink-0 overflow-hidden rounded-[2px] bg-secondary">
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
                                        <div className="truncate">{item.info}</div>
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
                                                        <Button variant="ghost" size="icon" className="cursor-pointer" onClick={() => retryQueueItem(item.id)}>
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
                                                        <Button variant="ghost" size="icon" className="cursor-pointer text-destructive hover:text-destructive" onClick={() => removeQueueItem(item.id)} disabled={item.status === "running"}>
                                                            <Trash2 className="h-4 w-4"/>
                                                        </Button>
                                                    </TooltipTrigger>
                                                    <TooltipContent>
                                                        <p>{t("translation.queue.removeFromQueue")}</p>
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
                                                    <Button variant="ghost" size="icon" className="cursor-pointer text-destructive hover:text-destructive" onClick={() => removeTrackFromQueueItem(item.id, trackIndex)} disabled={item.status === "running"}>
                                                        <Trash2 className="h-4 w-4"/>
                                                    </Button>
                                                </TooltipTrigger>
                                                <TooltipContent>
                                                    <p>{t("translation.queue.removeFromQueue")}</p>
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

            <Dialog open={showClearConfirm} onOpenChange={setShowClearConfirm}>
                <DialogContent className="max-w-md [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>{t("translation.queue.clearQueue")}</DialogTitle>
                        <DialogDescription>
                            {t("translation.queue.willRemoveAllQueued")}</DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setShowClearConfirm(false)} className="cursor-pointer">{t("translation.common.cancel")}</Button>
                        <Button variant="destructive" onClick={handleClearTab} className="cursor-pointer">
                            {t("translation.common.clearAll")}</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>);
}
