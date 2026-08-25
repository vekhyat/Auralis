import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { openExternal } from "@/lib/utils";
import { formatRelativeTime } from "@/lib/relative-time";

interface HeaderProps {
    version: string;
    hasUpdate: boolean;
    releaseDate?: string | null;
}

export function Header({ version, hasUpdate, releaseDate }: HeaderProps) {
    const { t } = useTranslation();
    return (
        <div className="flex items-end justify-between gap-4">
            <div className="space-y-1">
                <h1 className="text-[22px] font-semibold tracking-tight">{t("translation.sidebar.home")}</h1>
                <p className="max-w-[52ch] text-sm text-muted-foreground">{t("translation.header.tagline")}</p>
            </div>
            <div className="relative shrink-0">
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge variant="secondary" asChild>
                            <button
                                type="button"
                                onClick={() => openExternal("https://github.com/vekhyat/Auralis/releases/latest")}
                                className="cursor-pointer font-mono text-[11px] hover:opacity-80"
                            >
                                v{version}
                            </button>
                        </Badge>
                    </TooltipTrigger>
                    {hasUpdate && releaseDate && (
                        <TooltipContent>
                            <p>{formatRelativeTime(releaseDate)}</p>
                        </TooltipContent>
                    )}
                </Tooltip>
                {hasUpdate && (
                    <span className="absolute -right-1 -top-1 flex h-2.5 w-2.5">
                        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-75"></span>
                        <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-primary"></span>
                    </span>
                )}
            </div>
        </div>
    );
}
