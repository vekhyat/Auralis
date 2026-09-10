import type { ReactNode } from "react";
import { ExternalLink } from "lucide-react";
import { cn } from "@/lib/utils";
import { openExternal } from "@/lib/utils";

export interface InspectorRow {
    label: string;
    value: ReactNode;
}

interface InspectorPaneProps {
    eyebrow?: string;
    title: ReactNode;
    subtitle?: ReactNode;
    cover?: string;
    /** Rendered above the title when there is no cover (type word). */
    fallbackMark?: string;
    rows?: InspectorRow[];
    /** Primary + secondary actions, stacked as a quiet column. */
    actions?: ReactNode;
    footer?: ReactNode;
    className?: string;
}

/**
 * The right-hand desk pane. One item at rest: sharp 120px cover at most,
 * a definition list instead of hero type, actions as a text/button row.
 */
export function InspectorPane({ eyebrow, title, subtitle, cover, fallbackMark, rows = [], actions, footer, className }: InspectorPaneProps) {
    return (<aside className={cn("w-80 shrink-0 border-l border-border pl-6", className)}>
      <div className="flex flex-col gap-5">
        {(eyebrow || fallbackMark) ? (<p className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{eyebrow ?? fallbackMark}</p>) : null}
        {cover ? (<img src={cover} alt="" className="h-[120px] w-[120px] rounded-[2px] object-cover"/>) : null}
        <div className="space-y-1">
          <h2 className="text-lg leading-snug font-semibold break-words">{title}</h2>
          {subtitle ? (<p className="text-sm text-muted-foreground">{subtitle}</p>) : null}
        </div>
        {rows.length > 0 ? (<dl className="border-t border-border">
          {rows.map((row) => (<div key={row.label} className="flex items-baseline justify-between gap-4 border-b border-border py-2">
            <dt className="shrink-0 text-xs text-muted-foreground">{row.label}</dt>
            <dd className="min-w-0 truncate text-right text-[13px]">{row.value}</dd>
          </div>))}
        </dl>) : null}
        {actions ? (<div className="flex flex-wrap items-center gap-2">{actions}</div>) : null}
        {footer ? (<div className="text-xs text-muted-foreground">{footer}</div>) : null}
      </div>
    </aside>);
}

/** Quiet secondary action used across inspectors: an external link as words. */
export function InspectorLinkAction({ label, url }: {
    label: string;
    url: string;
}) {
    return (<button
      type="button"
      onClick={() => openExternal(url)}
      className="inline-flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      <ExternalLink className="size-3"/>
      <span>{label}</span>
    </button>);
}
