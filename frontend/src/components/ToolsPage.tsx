import { ChevronRight } from "lucide-react";
import { t } from "@/i18n";
import { cn } from "@/lib/utils";
import type { PageType } from "@/pages";
export type ToolGroup = "analysis" | "processing" | "management";
type ToolPage = Extract<PageType, "audio-analysis" | "tempo-key-analyzer" | "replaygain" | "audio-converter" | "audio-resampler" | "file-manager" | "lyrics-manager" | "enrich">;
interface ToolDefinition {
    page: ToolPage;
    titleKey: string;
    descriptionKey: string;
}
interface ToolGroupDefinition {
    id: ToolGroup;
    labelKey: string;
    tools: ToolDefinition[];
}
const TOOL_GROUPS: ToolGroupDefinition[] = [
    {
        id: "analysis",
        labelKey: "translation.tools.analysis",
        tools: [
            {
                page: "audio-analysis",
                titleKey: "translation.common.audioQualityAnalyzer",
                descriptionKey: "translation.tools.audioQualityDescription",
            },
            {
                page: "tempo-key-analyzer",
                titleKey: "translation.common.bpmKeyAnalyzer",
                descriptionKey: "translation.tools.tempoKeyDescription",
            },
            {
                page: "replaygain",
                titleKey: "translation.replayGain.title",
                descriptionKey: "translation.tools.replayGainDescription",
            },
        ],
    },
    {
        id: "processing",
        labelKey: "translation.tools.processing",
        tools: [
            {
                page: "audio-converter",
                titleKey: "translation.common.audioConverter",
                descriptionKey: "translation.tools.audioConverterDescription",
            },
            {
                page: "audio-resampler",
                titleKey: "translation.common.audioResampler",
                descriptionKey: "translation.tools.audioResamplerDescription",
            },
        ],
    },
    {
        id: "management",
        labelKey: "translation.tools.management",
        tools: [
            {
                page: "file-manager",
                titleKey: "translation.common.fileManager",
                descriptionKey: "translation.tools.fileManagerDescription",
            },
            {
                page: "lyrics-manager",
                titleKey: "translation.common.lyricsManager",
                descriptionKey: "translation.tools.lyricsManagerDescription",
            },
            {
                page: "enrich",
                titleKey: "translation.enrich.title",
                descriptionKey: "translation.tools.metadataEnricherDescription",
            },
        ],
    },
];
function ToolRow({ title, description, onOpen }: {
    title: string;
    description: string;
    onOpen: () => void;
}) {
    return (<button type="button" onClick={onOpen} className="flex min-h-12 w-full cursor-pointer items-center justify-between gap-4 border-b border-border px-1 py-3 text-left transition-colors hover:bg-muted/60 focus-visible:bg-muted focus-visible:outline-none" aria-label={title}>
        <span className="flex min-w-0 flex-col">
            <span className="truncate text-[13px] font-medium">{title}</span>
            <span className="mt-0.5 truncate text-xs text-muted-foreground">{description}</span>
        </span>
        <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true"/>
    </button>);
}
interface ToolsPageProps {
    activeGroup: ToolGroup;
    onActiveGroupChange: (group: ToolGroup) => void;
    onPageChange: (page: PageType) => void;
}
export function ToolsPage({ activeGroup, onActiveGroupChange, onPageChange }: ToolsPageProps) {
    const selectedGroup = TOOL_GROUPS.find((group) => group.id === activeGroup) ?? TOOL_GROUPS[0];
    return (<div className="mx-auto w-full max-w-3xl space-y-5">
            <h1 className="text-lg font-semibold tracking-tight">{t("translation.sidebar.tools")}</h1>

            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border pb-2">
                {TOOL_GROUPS.map((group) => (<button
                    key={group.id}
                    type="button"
                    onClick={() => onActiveGroupChange(group.id)}
                    className={cn(
                        "cursor-pointer text-[13px] transition-colors",
                        activeGroup === group.id
                            ? "font-semibold text-primary underline decoration-primary underline-offset-[6px]"
                            : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    {t(group.labelKey)}
                </button>))}
            </div>

            <div className="border-b border-border">
                {selectedGroup.tools.map((tool) => (<ToolRow
                    key={tool.page}
                    title={t(tool.titleKey)}
                    description={t(tool.descriptionKey)}
                    onOpen={() => onPageChange(tool.page)}
                />))}
            </div>
        </div>);
}
