import { t } from "@/i18n";
import type { TrackAvailability } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { AmazonAvailabilityIcon, QobuzAvailabilityIcon, TidalAvailabilityIcon } from "./PlatformIcons";

type ProviderEntry = {
    id: string;
    name: string;
    found: boolean;
    url?: string;
    icon: React.ReactNode;
};

function getProviderEntries(availability: TrackAvailability): ProviderEntry[] {
    const tidalUrl = availability.tidal_url?.trim() || "";
    const qobuzUrl = availability.qobuz_url?.trim() || "";
    const amazonUrl = availability.amazon_url?.trim() || "";
    return [
        {
            id: "tidal",
            name: t("literal.common.tidal"),
            found: tidalUrl !== "",
            url: tidalUrl,
            icon: <TidalAvailabilityIcon className="size-3.5 shrink-0"/>,
        },
        {
            id: "qobuz",
            name: t("literal.common.qobuz"),
            found: qobuzUrl !== "",
            url: qobuzUrl,
            icon: <QobuzAvailabilityIcon className="size-3.5 shrink-0"/>,
        },
        {
            id: "amazon",
            name: t("literal.common.amazonMusic"),
            found: amazonUrl !== "",
            url: amazonUrl,
            icon: <AmazonAvailabilityIcon className="size-3.5 shrink-0"/>,
        },
    ];
}
export function hasAvailabilityLinks(availability?: TrackAvailability): boolean {
    if (!availability) {
        return false;
    }
    return getProviderEntries(availability).some((entry) => entry.found);
}
/**
 * Availability as words: provider names that are links when found, a quiet
 * "not found" when not. Brand marks stay small and uncolored.
 */
export function AvailabilityLinks({ availability }: {
    availability?: TrackAvailability;
}) {
    if (!availability) {
        return <p>{t("translation.availabilityLinks.checkAvailability")}</p>;
    }
    const entries = getProviderEntries(availability);
    return (<div className="flex w-[260px] max-w-[260px] flex-col gap-1 pointer-events-auto">
            {entries.map((entry) => entry.found ? (<button key={entry.id} type="button" onClick={() => entry.url && openExternal(entry.url)} title={entry.url} className="flex min-w-0 cursor-pointer items-center gap-2 text-left text-xs text-foreground transition-colors hover:text-primary">
                    <span className="text-muted-foreground">{entry.icon}</span>
                    <span className="min-w-0 truncate whitespace-nowrap leading-5 underline decoration-transparent underline-offset-4 transition-colors group-hover:decoration-border hover:decoration-current">
                        {entry.name}
                    </span>
                </button>) : (<div key={entry.id} className="flex min-w-0 items-center gap-2 text-left text-xs">
                    <span className="opacity-40">{entry.icon}</span>
                    <span className="min-w-0 truncate whitespace-nowrap leading-5 text-muted-foreground">
                        {entry.name} — {t("translation.availabilityLinks.notFound")}
                    </span>
                </div>))}
        </div>);
}
