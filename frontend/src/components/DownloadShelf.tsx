import { useTranslation } from "react-i18next";
import { Download, FolderOpen, Pause, Play, X } from "lucide-react";
import { Button } from "./ui/button";
import { Spinner } from "./ui/spinner";
import { CoverArt } from "./ArtworkCard";
import { useDownloadProgress } from "@/hooks/useDownloadProgress";
import type { QueueItem } from "@/lib/queue";

export function DownloadShelf({ items, isProcessing, isPausing, isSuspended, onOpen, onPause, onResume, onStop, onFolder }: {
    items: QueueItem[]; isProcessing: boolean; isPausing: boolean; isSuspended: boolean;
    onOpen: () => void; onPause: () => void; onResume: () => void; onStop: () => void; onFolder: () => void;
}) {
    const { t } = useTranslation();
    const progress = useDownloadProgress();
    const active = items.find((item) => item.status === "running");
    const waiting = items.filter((item) => item.status === "pending" || item.status === "paused");
    const latest = active ?? waiting[0];
    const busy = isProcessing || progress.is_downloading;
    return <footer className="download-shelf fixed inset-x-0 bottom-0 z-30 flex h-[76px] items-center gap-4 border-t bg-card px-5">
        <button type="button" onClick={onOpen} className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left" aria-label={t("translation.queue.openQueue")}>
            {latest ? <CoverArt src={latest.image} className="size-12 shrink-0" /> : <div className="flex size-12 shrink-0 items-center justify-center rounded-lg bg-secondary"><Download className="size-5 text-primary" /></div>}
            <div className="min-w-0">
                <p className="truncate text-sm font-semibold">{latest?.name ?? t("translation.downloads.ready")}</p>
                <p className="mt-0.5 truncate text-xs text-muted-foreground">{busy ? t(isPausing ? "translation.queue.pausing" : "translation.downloads.downloading") : waiting.length ? t("translation.downloads.waitingResume") : t("translation.downloads.autoHint")}{latest ? ` · ${latest.artist}` : ""}</p>
            </div>
        </button>
        {busy && <div className="hidden items-center gap-2 text-xs text-muted-foreground sm:flex"><Spinner className="size-3.5" /><span className="font-mono tabular-nums">{progress.mb_downloaded.toFixed(1)} MB{progress.speed_mbps > 0 ? ` · ${progress.speed_mbps.toFixed(1)} MB/s` : ""}</span></div>}
        <Button variant="ghost" size="sm" onClick={onOpen}>{t("translation.downloads.title")}{waiting.length > 0 && <span className="rounded bg-secondary px-1.5 text-xs tabular-nums">{waiting.length}</span>}</Button>
        {busy ? <><Button variant="outline" size="icon" disabled={isPausing} onClick={onPause} aria-label={t("translation.queue.pauseAll")}><Pause className="size-4" /></Button><Button variant="ghost" size="icon" onClick={onStop} aria-label={t("translation.downloads.cancelCurrent")}><X className="size-4" /></Button></> : (waiting.length > 0 || isSuspended) && <Button variant="outline" size="sm" disabled={waiting.length === 0} onClick={onResume}><Play className="size-4" />{t("translation.queue.resume")}</Button>}
        <Button variant="ghost" size="icon" onClick={onFolder} aria-label={t("translation.common.openFolder")}><FolderOpen className="size-4" /></Button>
    </footer>;
}
