import { useTranslation } from "react-i18next";
import { Progress } from "@/components/ui/progress";
import type { TasteSyncProgress } from "@/hooks/useTasteSync";

const PHASE_KEYS: Record<string, string> = {
    spotify_api: "translation.connections.syncPhaseSpotify",
    lastfm: "translation.connections.syncPhaseLastfm",
    profile: "translation.connections.syncPhaseProfile",
    gaps: "translation.connections.syncPhaseGaps",
};

export function TasteSyncStatus({ progress }: { progress: TasteSyncProgress }) {
    const { t } = useTranslation();
    return <div className="space-y-2" role="status" aria-live="polite">
        <div className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground">
            <span>{t(PHASE_KEYS[progress.phase] ?? "translation.connections.syncStarting")}</span>
            {progress.total > 0 ? <span className="font-mono">{progress.current} / {progress.total}</span> : null}
        </div>
        <Progress aria-label={t("translation.connections.syncTitle")} value={progress.total > 0 ? Math.min(100, progress.current / progress.total * 100) : 0} />
    </div>;
}
