import { t } from "@/i18n";
import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { PlugZap, Loader2, Server, Clock3 } from "lucide-react";
import { TidalIcon, QobuzIcon, AmazonIcon, DeezerIcon, AppleIcon, JioSaavnIcon } from "./PlatformIcons";
import { useApiStatus } from "@/hooks/useApiStatus";
import { SPOTIFLAC_NEXT_SOURCES } from "@/lib/api-status";
import { openExternal } from "@/lib/utils";
type CommunityBreakStatus = {
    enabled: boolean;
    is_break: boolean;
    remaining_minutes: number;
    available: boolean;
    error?: string;
};
type CommunityBreakStatuses = Record<string, CommunityBreakStatus>;
type CommunityBreakWindow = Window & {
    go?: {
        main?: {
            App?: {
                GetCommunityBreakStatuses?: () => Promise<CommunityBreakStatuses>;
            };
        };
    };
};
function GetCommunityBreakStatuses(): Promise<CommunityBreakStatuses> {
    const method = (window as CommunityBreakWindow).go?.main?.App?.GetCommunityBreakStatuses;
    if (!method) {
        return Promise.reject(new Error("GetCommunityBreakStatuses is unavailable"));
    }
    return method();
}
function renderBreakInfo(status: CommunityBreakStatus | undefined, loading: boolean) {
    if (loading && !status) {
        return <span className="text-xs text-muted-foreground">{t("translation.migrated.ApiStatusTab.loadingSchedule")}</span>;
    }
    if (!status?.available) {
        return <span className="text-xs text-muted-foreground">{t("translation.migrated.ApiStatusTab.breakScheduleUnavailable")}</span>;
    }
    if (!status.enabled) {
        return <span className="text-xs text-muted-foreground">{t("translation.migrated.ApiStatusTab.scheduledBreakDisabled")}</span>;
    }
    return (<span className="text-xs text-muted-foreground">
      {status.is_break ? t("translation.migrated.ApiStatusTab.breakEndsInMin", { value1: status.remaining_minutes }) : t("translation.migrated.ApiStatusTab.breakStartsInMin", { value1: status.remaining_minutes })}
    </span>);
}
function statusWord(status: "checking" | "online" | "offline" | "idle"): string {
    switch (status) {
        case "online":
            return t("translation.apiStatus.online");
        case "offline":
            return t("translation.apiStatus.offline");
        case "checking":
            return t("translation.apiStatus.checking");
        default:
            return t("translation.apiStatus.idle");
    }
}
function renderStatusIndicator(status: "checking" | "online" | "offline" | "idle") {
    const label = statusWord(status);
    return (<span aria-live="polite" className="inline-flex items-center gap-1.5 font-mono text-[11px] tracking-wide text-muted-foreground uppercase">
      {status === "checking" ? <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true"/> : null}
      <span>{label}</span>
    </span>);
}
function renderPlatformIcon(type: string) {
    if (type === "tidal") {
        return <TidalIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
    }
    if (type === "amazon") {
        return <AmazonIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
    }
    if (type === "deezer") {
        return <DeezerIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
    }
    if (type === "apple") {
        return <AppleIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
    }
    if (type === "jiosaavn") {
        return <JioSaavnIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
    }
    return <QobuzIcon className="w-5 h-5 shrink-0 text-muted-foreground"/>;
}
export function ApiStatusTab() {
    const { sources, statuses, nextStatuses, checkingSources, checkAllCurrent, checkAllNext } = useApiStatus();
    const isCheckingCurrent = sources.some((source) => checkingSources[source.id] === true);
    const isCheckingNext = SPOTIFLAC_NEXT_SOURCES.some((source) => nextStatuses[source.id] === "checking");
    const isChecking = isCheckingCurrent || isCheckingNext;
    const [breakStatuses, setBreakStatuses] = useState<CommunityBreakStatuses>({});
    const [isCheckingBreaks, setIsCheckingBreaks] = useState(true);
    const checkBreaks = useCallback(async () => {
        setIsCheckingBreaks(true);
        try {
            setBreakStatuses(await GetCommunityBreakStatuses());
        }
        catch {
            setBreakStatuses({});
        }
        finally {
            setIsCheckingBreaks(false);
        }
    }, []);
    useEffect(() => {
        let cancelled = false;
        GetCommunityBreakStatuses().then((statuses) => {
            if (cancelled)
                return;
            setBreakStatuses(statuses);
            setIsCheckingBreaks(false);
        }, () => {
            if (cancelled)
                return;
            setBreakStatuses({});
            setIsCheckingBreaks(false);
        });
        return () => {
            cancelled = true;
        };
    }, []);
    const checkAll = () => {
        void checkAllCurrent();
        void checkAllNext();
        void checkBreaks();
    };
    return (<div className="space-y-6">
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold tracking-tight">Auralis</h3>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => openExternal("https://spotbye.qzz.io")} className="gap-2">
              <Server className="h-4 w-4"/>
              {t("translation.common.details")}
            </Button>
            <Button variant="outline" onClick={checkAll} disabled={isChecking || isCheckingBreaks} className="gap-2">
              {isChecking || isCheckingBreaks ? <Loader2 className="h-4 w-4 animate-spin"/> : <PlugZap className="h-4 w-4"/>}
              {t("translation.common.check")}
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {sources.map((source) => {
            const status = statuses[source.id] || "idle";
            return (<div key={source.id} className="space-y-3 p-4 border rounded-[2px] bg-card text-card-foreground">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3">
                    {renderPlatformIcon(source.type)}
                    <p className="font-medium leading-none">{source.name}</p>
                  </div>
                  <div className="flex items-center">{renderStatusIndicator(status)}</div>
                </div>
                {source.id === "tidal" || source.id === "qobuz" || source.id === "amazon" ? (<div className="flex items-center gap-2 border-t pt-3">
                  <Clock3 className="h-3.5 w-3.5 shrink-0 text-muted-foreground"/>
                  {renderBreakInfo(breakStatuses[source.id], isCheckingBreaks)}
                </div>) : null}
              </div>);
        })}
        </div>
        <p className="text-xs text-muted-foreground">
          {t("translation.migrated.ApiStatusTab.theServersAreAvailableForAbout1")}
        </p>
      </div>

      <div className="border-t"/>

      <div className="space-y-4">
        <h3 className="text-sm font-semibold tracking-tight">{t("translation.apiStatus.extendedSources")}</h3>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {SPOTIFLAC_NEXT_SOURCES.map((source) => {
            const status = nextStatuses[source.id] || "idle";
            return (<div key={source.id} className="flex items-center justify-between p-4 border rounded-[2px] bg-card text-card-foreground">
              <div className="flex items-center gap-3">
                {renderPlatformIcon(source.id)}
                <p className="font-medium leading-none">{source.name}</p>
              </div>
              <div className="flex items-center">{renderStatusIndicator(status)}</div>
            </div>);
        })}
        </div>
      </div>
    </div>);
}
