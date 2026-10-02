import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
    ArrowLeft,
    ArrowRight,
    Bug,
    Ellipsis,
    ExternalLink,
    Minus,
    Square,
    X,
} from "lucide-react";
import { WindowMinimise, WindowToggleMaximise, Quit } from "../../wailsjs/runtime/runtime";
import { Menubar, MenubarContent, MenubarMenu, MenubarItem, MenubarTrigger, MenubarLabel, MenubarSeparator } from "@/components/ui/menubar";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { OmnibarSearch } from "@/components/OmnibarSearch";
import { openExternal } from "@/lib/utils";
import { cn } from "@/lib/utils";
import type { DestinationPage, PageType } from "@/pages";

const noDrag = { "--wails-draggable": "no-drag" } as React.CSSProperties;

interface OmnibarBinding {
    value: string;
    loading: boolean;
    onChange: (value: string) => void;
    onSubmit: () => void;
}

interface TitleBarProps {
    canGoBack?: boolean;
    canGoForward?: boolean;
    navigationDisabled?: boolean;
    onBack?: () => void;
    onForward?: () => void;
    currentPage: PageType;
    onPageChange: (page: DestinationPage | "debug") => void;
    queueCount?: number;
    omnibar: OmnibarBinding;
}

export function TitleBar({ canGoBack = false, canGoForward = false, navigationDisabled = false, onBack, onForward, currentPage, onPageChange, queueCount = 0, omnibar }: TitleBarProps) {
    const { t } = useTranslation();
    const [isIssuesDialogOpen, setIsIssuesDialogOpen] = useState(false);
    const [hasIssueAgreement, setHasIssueAgreement] = useState(false);
    const version = __APP_VERSION__;
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
    const destinations: Array<{ page: DestinationPage; label: string; active: boolean; count?: number }> = [
        { page: "main", label: t("translation.sidebar.library"), active: currentPage === "main" },
        { page: "queue", label: t("translation.queue.queue"), active: currentPage === "queue", count: queueCount },
        { page: "history", label: t("translation.sidebar.history"), active: currentPage === "history" },
        { page: "devices", label: t("translation.devices.destination"), active: currentPage === "devices" },
        { page: "tools", label: t("translation.sidebar.tools"), active: currentPage.startsWith("audio-") || ["tools", "tempo-key-analyzer", "replaygain", "file-manager", "lyrics-manager", "enrich"].includes(currentPage) },
        { page: "settings", label: t("translation.sidebar.settings"), active: currentPage === "settings" },
    ];
    return (<>
      <header
        className="fixed inset-x-0 top-0 z-40 flex h-11 items-center gap-3 border-b bg-background pr-0 pl-3"
        style={{ "--wails-draggable": "drag" } as React.CSSProperties}
        onDoubleClick={() => WindowToggleMaximise()}
      >
        <div className="flex shrink-0 items-center gap-2" style={noDrag}>
          <img src="/icon.svg" alt="" className="size-[22px] rounded-[2px]"/>
          <span className="text-[13px] font-semibold tracking-tight">Auralis</span>
        </div>

        <div className="flex h-full shrink-0 items-center gap-0.5" style={noDrag}>
          <button
            type="button"
            onClick={onBack}
            disabled={!canGoBack || navigationDisabled}
            className="flex size-7 cursor-pointer items-center justify-center rounded-[2px] transition-colors hover:bg-muted disabled:cursor-default disabled:opacity-35 disabled:hover:bg-transparent"
            aria-label={t("translation.common.goPreviousPage")}
          >
            <ArrowLeft className="size-3.5"/>
          </button>
          <button
            type="button"
            onClick={onForward}
            disabled={!canGoForward || navigationDisabled}
            className="flex size-7 cursor-pointer items-center justify-center rounded-[2px] transition-colors hover:bg-muted disabled:cursor-default disabled:opacity-35 disabled:hover:bg-transparent"
            aria-label={t("translation.common.goNextPage")}
          >
            <ArrowRight className="size-3.5"/>
          </button>
        </div>

        <div className="min-w-0 flex-1 sm:max-w-xl lg:max-w-2xl" style={noDrag}>
          <OmnibarSearch value={omnibar.value} busy={omnibar.loading} onChange={omnibar.onChange} onSubmit={omnibar.onSubmit}/>
        </div>

        <nav className="ml-auto flex h-full shrink-0 items-stretch gap-0.5" style={noDrag} aria-label={t("translation.sidebar.tools")}>
          {destinations.map((destination) => (<button
            key={destination.page}
            type="button"
            onClick={() => onPageChange(destination.page)}
            className={cn(
                "relative flex cursor-pointer items-center gap-1.5 px-2.5 text-[12.5px] transition-colors",
                destination.active
                    ? "font-semibold text-primary after:absolute after:inset-x-2.5 after:bottom-2 after:h-px after:bg-primary"
                    : "font-medium text-muted-foreground hover:text-foreground",
            )}
          >
            <span className="whitespace-nowrap">{destination.label}</span>
            {destination.count ? (<span className="font-mono text-[11px] tabular-nums opacity-80">{destination.count > 99 ? "99+" : destination.count}</span>) : null}
          </button>))}
          <Menubar className="border-none bg-transparent shadow-none px-0">
            <MenubarMenu>
              <MenubarTrigger className="size-8 cursor-pointer rounded-[2px] p-0 transition-colors data-[state=open]:bg-muted hover:bg-muted flex items-center justify-center text-muted-foreground hover:text-foreground" aria-label={t("translation.common.more")}>
                <Ellipsis className="size-4"/>
              </MenubarTrigger>
              <MenubarContent align="end" className="min-w-44">
                <div className="px-2 py-1">
                  <MenubarLabel className="p-0 font-mono text-[11px] font-normal text-muted-foreground">
                    Auralis v{version}
                  </MenubarLabel>
                </div>
                <MenubarSeparator />
                <MenubarItem onClick={() => onPageChange("debug")} className="cursor-pointer gap-2">
                  <span>{t("translation.sidebar.debugLogs")}</span>
                </MenubarItem>
                <MenubarItem onSelect={() => setIsIssuesDialogOpen(true)} className="cursor-pointer gap-2">
                  <Bug className="size-3.5"/>
                  <span>{t("translation.sidebar.reportBugsRequestFeatures")}</span>
                </MenubarItem>
                <MenubarItem onClick={() => openExternal("https://github.com/vekhyat/Auralis")} className="cursor-pointer gap-2">
                  <ExternalLink className="size-3.5"/>
                  <span>{t("translation.titleBar.website")}</span>
                </MenubarItem>
              </MenubarContent>
            </MenubarMenu>
          </Menubar>
        </nav>

        <div className="flex h-full shrink-0 items-stretch" style={noDrag}>
          <button onClick={() => WindowMinimise()} className="flex w-11 cursor-pointer items-center justify-center text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label={t("translation.titleBar.minimize")}>
            <Minus className="size-3.5"/>
          </button>
          <button onClick={() => WindowToggleMaximise()} className="flex w-11 cursor-pointer items-center justify-center text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label={t("translation.titleBar.maximize")}>
            <Square className="size-3"/>
          </button>
          <button onClick={() => Quit()} className="flex w-11 cursor-pointer items-center justify-center text-muted-foreground transition-colors hover:bg-destructive hover:text-white" aria-label={t("translation.common.close")}>
            <X className="size-3.5"/>
          </button>
        </div>
      </header>

      <Dialog open={isIssuesDialogOpen} onOpenChange={handleIssuesDialogChange}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("translation.sidebar.beforeOpeningIssues")}</DialogTitle>
            <DialogDescription />
          </DialogHeader>
          <div className="space-y-4 text-sm">
            <div className="border border-primary/30 bg-primary/5 p-4">
              <p className="font-semibold">{t("translation.sidebar.important")}</p>
              <p className="mt-1 text-muted-foreground">{t("translation.sidebar.searchIssuesFirst")}</p>
            </div>
            <label className="flex cursor-pointer items-start gap-3 border p-4">
              <Checkbox className="mt-0.5 shrink-0" checked={hasIssueAgreement} onCheckedChange={(checked) => setHasIssueAgreement(checked === true)} />
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
    </>);
}
