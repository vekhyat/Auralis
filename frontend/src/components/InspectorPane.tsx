import type { ReactNode } from "react";
import { CoverArt } from "./ArtworkCard";
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
    return (<aside className={cn("inspector-pane w-72 shrink-0", className)}>
      <CoverArt src={cover} className="inspector-cover w-full" />
      <div className="inspector-body mt-5 min-w-0">
        <h2 className="text-2xl leading-tight font-semibold tracking-tight break-words">{title}</h2>
        <p className="mt-2 text-sm text-muted-foreground">{[eyebrow ?? fallbackMark, subtitle].filter(Boolean).map((part, index) => <span key={index}>{index > 0 && " · "}{part}</span>)}</p>
        {actions && <div className="my-5 flex flex-wrap items-center gap-2">{actions}</div>}
        {rows.length > 0 && <dl className="grid gap-x-6 border-t border-border">
          {rows.map((row) => <div key={row.label} className="flex items-baseline justify-between gap-4 border-b border-border py-2.5">
            <dt className="shrink-0 text-xs text-muted-foreground">{row.label}</dt>
            <dd className="min-w-0 text-right text-sm break-words">{row.value}</dd>
          </div>)}
        </dl>}
        {footer && <div className="mt-4 text-xs text-muted-foreground">{footer}</div>}
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
