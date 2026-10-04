import { t } from "@/i18n";
import { useState } from "react";
import { X } from "lucide-react";
import { useDownloadProgress } from "@/hooks/useDownloadProgress";
const DISMISSED_KEY = "auralis_cooldown_dismissed_event";
export function CooldownBanner() {
    const progress = useDownloadProgress();
    const isCooldown = Boolean(progress.cooldown) && (progress.cooldown_secs ?? 0) > 0;
    const eventID = progress.cooldown_event_id ?? 0;
    const [dismissedEventID, setDismissedEventID] = useState(() => {
        const stored = Number(localStorage.getItem(DISMISSED_KEY));
        return Number.isFinite(stored) ? stored : 0;
    });
    const dismiss = (id: number) => {
        setDismissedEventID(id);
        localStorage.setItem(DISMISSED_KEY, String(id));
    };
    if (!isCooldown || dismissedEventID === eventID) {
        return null;
    }
    const cooldownMinutes = Math.max(1, Math.ceil((progress.cooldown_secs ?? 0) / 60));
    const cooldownMessage = t("translation.migrated.CooldownBanner.scheduledBreak", { count: cooldownMinutes });
    // A ruled notice on the paper, not an amber pill floating over the chrome.
    return (<div className="fixed top-14 left-1/2 z-50 -translate-x-1/2 animate-in fade-in slide-in-from-top-2">
      <div className="flex items-center gap-2.5 border border-border bg-card px-3.5 py-2 text-foreground shadow-none">
        <span aria-hidden="true" className="h-4 w-px shrink-0 bg-primary"/>
        <p className="text-xs font-medium leading-tight">{cooldownMessage}</p>
        <button type="button" onClick={() => dismiss(eventID)} aria-label={t("translation.migrated.CooldownBanner.dismiss")} className="ml-1 shrink-0 cursor-pointer p-1 text-muted-foreground transition-colors hover:text-foreground">
          <X className="size-3.5"/>
        </button>
      </div>
    </div>);
}
