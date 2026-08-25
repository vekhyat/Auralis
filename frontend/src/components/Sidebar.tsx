import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { HomeIcon } from "@/components/ui/home";
import { HistoryIcon } from "@/components/ui/history-icon";
import { ListOrderedIcon } from "@/components/ui/list-ordered-icon";
import { SettingsIcon } from "@/components/ui/settings";
import { TerminalIcon } from "@/components/ui/terminal";
import { BugReportIcon } from "@/components/ui/bug-report-icon";
import { ToolCaseIcon } from "@/components/ui/tool-case";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import { openExternal } from "@/lib/utils";
import { cn } from "@/lib/utils";

export type PageType = "main" | "settings" | "debug" | "tools" | "audio-analysis" | "tempo-key-analyzer" | "replaygain" | "audio-converter" | "audio-resampler" | "file-manager" | "lyrics-manager" | "enrich" | "history" | "queue";

interface SidebarProps {
    currentPage: PageType;
    onPageChange: (page: PageType) => void;
    queueBadgeCount?: number;
}

const TOOL_PAGES: PageType[] = ["tools", "audio-analysis", "tempo-key-analyzer", "replaygain", "audio-converter", "audio-resampler", "file-manager", "lyrics-manager", "enrich"];

function NavButton({
    active,
    onClick,
    icon,
    label,
    badge,
}: {
    active: boolean;
    onClick: () => void;
    icon: ReactNode;
    label: string;
    badge?: ReactNode;
}) {
    return (
        <button
            type="button"
            title={label}
            onClick={onClick}
            className={cn(
                "flex h-9 w-full items-center gap-2.5 rounded-md px-2.5 text-[13px] font-medium transition-colors",
                active ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
        >
            <span className="flex size-4 items-center justify-center [&_svg]:size-4">{icon}</span>
            <span className="min-w-0 flex-1 truncate text-left">{label}</span>
            {badge}
        </button>
    );
}

export function Sidebar({ currentPage, onPageChange, queueBadgeCount = 0 }: SidebarProps) {
    const { t } = useTranslation();
    const [isIssuesDialogOpen, setIsIssuesDialogOpen] = useState(false);
    const [hasIssueAgreement, setHasIssueAgreement] = useState(false);
    const handleIssuesDialogChange = (open: boolean) => {
        setIsIssuesDialogOpen(open);
        if (!open) {
            setHasIssueAgreement(false);
        }
    };
    const handleOpenIssues = () => {
        openExternal("https://github.com/vekhyat/Auralis/issues");
        handleIssuesDialogChange(false);
    };
    return (
        <div className="fixed bottom-0 left-0 top-10 z-30 flex w-44 flex-col border-r border-border bg-card/80 px-2 py-3">
            <div className="flex flex-1 flex-col gap-5 overflow-y-auto">
                <div>
                    <p className="px-2.5 pb-1.5 text-[11px] font-medium text-muted-foreground">Library</p>
                    <div className="flex flex-col gap-0.5">
                        <NavButton
                            active={currentPage === "main"}
                            onClick={() => onPageChange("main")}
                            icon={<HomeIcon size={16} />}
                            label={t("translation.sidebar.home")}
                        />
                        <NavButton
                            active={currentPage === "queue"}
                            onClick={() => onPageChange("queue")}
                            icon={<ListOrderedIcon size={16} />}
                            label={t("translation.queue.queue")}
                            badge={
                                queueBadgeCount > 0 ? (
                                    <span className="flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 font-mono text-[10px] font-semibold text-primary-foreground">
                                        {queueBadgeCount > 99 ? "99+" : queueBadgeCount}
                                    </span>
                                ) : null
                            }
                        />
                        <NavButton
                            active={currentPage === "history"}
                            onClick={() => onPageChange("history")}
                            icon={<HistoryIcon size={16} />}
                            label={t("translation.sidebar.history")}
                        />
                    </div>
                </div>

                <div>
                    <p className="px-2.5 pb-1.5 text-[11px] font-medium text-muted-foreground">Work</p>
                    <div className="flex flex-col gap-0.5">
                        <NavButton
                            active={TOOL_PAGES.includes(currentPage)}
                            onClick={() => onPageChange("tools")}
                            icon={<ToolCaseIcon size={16} />}
                            label={t("translation.sidebar.tools")}
                        />
                        <NavButton
                            active={currentPage === "settings"}
                            onClick={() => onPageChange("settings")}
                            icon={<SettingsIcon size={16} />}
                            label={t("translation.sidebar.settings")}
                        />
                    </div>
                </div>
            </div>

            <div className="mt-auto flex flex-col gap-0.5 border-t border-border pt-2">
                <NavButton
                    active={currentPage === "debug"}
                    onClick={() => onPageChange("debug")}
                    icon={<TerminalIcon size={16} loop={true} />}
                    label={t("translation.sidebar.debugLogs")}
                />
                <NavButton
                    active={false}
                    onClick={() => setIsIssuesDialogOpen(true)}
                    icon={<BugReportIcon size={16} loop={true} />}
                    label={t("translation.sidebar.reportBugsRequestFeatures")}
                />
            </div>

            <Dialog open={isIssuesDialogOpen} onOpenChange={handleIssuesDialogChange}>
                <DialogContent className="max-w-xl">
                    <DialogHeader>
                        <DialogTitle>{t("translation.sidebar.beforeOpeningIssues")}</DialogTitle>
                        <DialogDescription />
                    </DialogHeader>
                    <div className="space-y-4 text-sm">
                        <div className="rounded-md border border-primary/30 bg-primary/8 p-4">
                            <p className="font-semibold">{t("translation.sidebar.important")}</p>
                            <p className="mt-1 text-muted-foreground">{t("translation.sidebar.searchIssuesFirst")}</p>
                        </div>
                        <label className="flex cursor-pointer items-center gap-3 rounded-md border p-4">
                            <Checkbox className="shrink-0" checked={hasIssueAgreement} onCheckedChange={(checked) => setHasIssueAgreement(checked === true)} />
                            <span className="leading-5 text-foreground/90">{t("translation.sidebar.issueAgreement")}</span>
                        </label>
                    </div>
                    <DialogFooter className="gap-2 sm:justify-between">
                        <Button variant="outline" onClick={() => handleIssuesDialogChange(false)}>
                            {t("translation.sidebar.cancel")}
                        </Button>
                        <Button disabled={!hasIssueAgreement} onClick={handleOpenIssues}>
                            {t("translation.sidebar.openIssues")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
