import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

export interface CatalogRowProps {
    cover?: string;
    coverAlt?: string;
    /** Shown when there is no cover image (e.g. two-letter type mark). */
    coverFallback?: string;
    title: ReactNode;
    subtitle?: ReactNode;
    /** Quiet right-aligned data: type word, duration, year… */
    meta?: ReactNode;
    /** Leading mono numeral (#, position). */
    index?: number;
    explicit?: boolean;
    selected?: boolean;
    onClick?: () => void;
    onDoubleClick?: () => void;
    /** Trailing inline actions (kept out of the click target by callers). */
    trailing?: ReactNode;
    className?: string;
}

/**
 * The single row grammar of the catalog desk: hairline-ruled, 28px sharp
 * cover, ink title, quiet subtitle, right-aligned data. Used by recents,
 * search results, track lists, discographies and the queue.
 */
export function CatalogRow({ cover, coverAlt = "", coverFallback, title, subtitle, meta, index, explicit, selected, onClick, onDoubleClick, trailing, className }: CatalogRowProps) {
    const { t } = useTranslation();
    return (<div
        role={onClick ? "button" : undefined}
        tabIndex={onClick ? 0 : undefined}
        onClick={onClick}
        onDoubleClick={onDoubleClick}
        onKeyDown={(event) => {
            if (!onClick || event.target !== event.currentTarget) return;
            if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onClick();
            }
        }}
        className={cn(
            "flex min-h-11 items-center gap-3 border-b border-border px-3 py-1.5 transition-colors outline-none",
            onClick && "cursor-pointer select-none",
            selected ? "bg-primary/[0.07]" : "hover:bg-muted/70 focus-visible:bg-muted",
            className,
        )}
    >
        {typeof index === "number" ? (<span className="w-7 shrink-0 text-right font-mono text-xs tabular-nums text-muted-foreground">{index}</span>) : null}
        {cover || coverFallback ? (cover ? (<img src={cover} alt={coverAlt} loading="lazy" referrerPolicy="no-referrer" className="size-7 shrink-0 rounded-[2px] object-cover"/>) : (<span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-[2px] bg-muted font-mono text-[10px] font-semibold text-muted-foreground">{coverFallback}</span>)) : null}
        <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-baseline gap-2">
                <span className={cn("truncate text-[13px] leading-tight", selected ? "font-semibold text-primary" : "font-medium")}>{title}</span>
                {explicit ? (<span className="shrink-0 font-mono text-[9px] tracking-widest uppercase text-muted-foreground" title={t("translation.common.explicit")}>{t("translation.common.explicit")}</span>) : null}
            </div>
            {subtitle ? (<div className="truncate text-xs leading-snug text-muted-foreground">{subtitle}</div>) : null}
        </div>
        {meta ? (<div className="shrink-0 text-right text-xs whitespace-nowrap text-muted-foreground">{meta}</div>) : null}
        {trailing ? (<div className="ml-1 flex shrink-0 items-center gap-0.5" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>{trailing}</div>) : null}
    </div>);
}
