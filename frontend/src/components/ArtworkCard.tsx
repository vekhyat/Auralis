import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Music2 } from "lucide-react";
import { cn } from "@/lib/utils";

export function CoverArt({ src, className }: { src?: string; className?: string }) {
    const [failedSrc, setFailedSrc] = useState<string>();
    return <div className={cn("cover-art aspect-square overflow-hidden rounded-lg bg-secondary", className)}>
        {src && failedSrc !== src ? <img src={src} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailedSrc(src)} className="size-full object-cover" />
            : <div className="flex size-full items-center justify-center text-muted-foreground"><Music2 className="size-10" strokeWidth={1.25} /></div>}
    </div>;
}

export function ArtworkCard({ cover, title, subtitle, meta, selected, onClick, onDoubleClick, trailing, explicit }: {
    cover?: string; coverFallback?: string; title: string; subtitle?: React.ReactNode; meta?: React.ReactNode; explicit?: boolean;
    selected?: boolean; onClick: () => void; onDoubleClick?: () => void; trailing?: React.ReactNode;
}) {
    const { t } = useTranslation();
    return <div className={cn("artwork-card group relative min-w-0 rounded-xl p-2", selected && "bg-primary/8 ring-1 ring-primary/40")}>
        <button type="button" onClick={onClick} onDoubleClick={onDoubleClick} aria-pressed={selected} className="block w-full cursor-pointer text-left">
            <CoverArt src={cover} className="mb-3 w-full transition-transform duration-200 group-hover:-translate-y-1 motion-reduce:transform-none" />
            <span className="block truncate text-sm font-semibold" title={title}>{title}</span>
            {subtitle && <span className="mt-0.5 block truncate text-xs text-muted-foreground" title={typeof subtitle === "string" ? subtitle : undefined}>{subtitle}</span>}
            {explicit && <span className="mt-1 block text-[11px] text-muted-foreground">{t("translation.common.explicit")}</span>}
            {meta && <span className="mt-2 block text-xs text-muted-foreground">{meta}</span>}
        </button>
        {trailing}
    </div>;
}
