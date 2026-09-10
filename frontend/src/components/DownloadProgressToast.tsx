import { t } from "@/i18n";
import { useDownloadProgress } from "@/hooks/useDownloadProgress";
import { Download } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
interface DownloadProgressToastProps {
    isPreparing?: boolean;
    onOpenQueue?: () => void;
}
export function DownloadProgressToast({ isPreparing = false, onOpenQueue }: DownloadProgressToastProps) {
    const progress = useDownloadProgress();
    const hasTransfer = progress.is_downloading && progress.mb_downloaded > 0;
    if (!progress.is_downloading && !isPreparing) {
        return null;
    }
    // Docked to the bottom-right as a quiet status line; no rail offsets.
    return (<div className="fixed right-4 bottom-4 z-50 animate-in slide-in-from-bottom-5 data-[state=closed]:animate-out data-[state=closed]:slide-out-to-bottom-5">
      <button type="button" onClick={onOpenQueue} className="cursor-pointer border border-border bg-background px-3 py-2 text-left text-foreground transition-colors hover:bg-muted" aria-label={t("translation.queue.openQueue")}>
        <div className="flex items-center gap-3">
          {hasTransfer ? (<Download className="size-3.5 text-primary"/>) : (<Spinner className="size-3.5 text-primary"/>)}
          <div className="flex min-w-20 flex-col">
            {hasTransfer ? (<>
              <p className="font-mono text-sm font-medium tabular-nums">
                {progress.mb_downloaded.toFixed(2)} {t("literal.common.mb")}
              </p>
              {progress.speed_mbps > 0 && (<p className="font-mono text-xs tabular-nums text-muted-foreground">
                  {progress.speed_mbps.toFixed(2)} {t("literal.downloadProgressToast.mbS")}
                </p>)}
            </>) : (<p className="text-sm font-medium">{t("translation.queue.preparing")}</p>)}
          </div>
        </div>
      </button>
    </div>);
}
