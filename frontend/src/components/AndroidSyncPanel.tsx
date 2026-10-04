import { useCallback, useEffect, useState } from "react";
import { t } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { cn } from "@/lib/utils";
import {
    AddFolderTarget,
    CancelSync,
    DownloadPlatformTools,
    GetDeviceProfile,
    IsPlatformToolsInstalled,
    ListSyncTargets,
    PlanSync,
    RemoveFolderTarget,
    ResumeSync,
    SaveDeviceProfile,
    SelectFolder,
    StartSync,
} from "../../wailsjs/go/main/App";
import { devices, syncengine } from "../../wailsjs/go/models";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import {
    AlertCircle,
    Check,
    ChevronDown,
    ChevronRight,
    Download,
    Folder,
    FolderPlus,
    HardDrive,
    HelpCircle,
    Play,
    RefreshCw,
    Sliders,
    Smartphone,
    Trash2,
    X,
} from "lucide-react";

function formatBytes(value: number): string {
    if (!value || value <= 0) return "0 MB";
    const gb = value / (1024 * 1024 * 1024);
    if (gb >= 10) return `${Math.round(gb)} GB`;
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    return `${Math.max(1, Math.round(value / (1024 * 1024)))} MB`;
}

interface ProgressEvent {
    phase: string;
    opTotal?: number;
    done?: number;
    bytesTotal?: number;
    bytesDone?: number;
    name?: string;
    speedBps?: number;
    etaSec?: number;
}

export function AndroidSyncPanel() {
    const [targets, setTargets] = useState<devices.SyncTargetView[]>([]);
    const [selectedId, setSelectedId] = useState<string>("");
    const [adbInstalled, setAdbInstalled] = useState<boolean>(true);
    const [adbDownloading, setAdbDownloading] = useState<boolean>(false);
    const [adbDownloadProgress, setAdbDownloadProgress] = useState<number>(0);
    const [adbDownloadStatus, setAdbDownloadStatus] = useState<string>("");

    const [profile, setProfile] = useState<devices.DeviceProfile | null>(null);
    const [plan, setPlan] = useState<syncengine.Plan | null>(null);
    const [showPlanDialog, setShowPlanDialog] = useState<boolean>(false);
    const [planning, setPlanning] = useState<boolean>(false);
    const [syncing, setSyncing] = useState<boolean>(false);
    const [syncProgress, setSyncProgress] = useState<ProgressEvent | null>(null);
    const [showHelp, setShowHelp] = useState<boolean>(false);

    // Refresh targets list
    const refreshTargets = useCallback(async () => {
        try {
            const list = await ListSyncTargets();
            setTargets(list || []);
            if (list && list.length > 0) {
                setSelectedId((curr) => (list.some((item) => item.id === curr) ? curr : list[0].id));
            } else {
                setSelectedId("");
            }
        } catch (err) {
            console.error("Failed to list sync targets:", err);
        }
    }, []);

    // Check ADB installed
    const checkAdb = useCallback(async () => {
        try {
            const installed = await IsPlatformToolsInstalled();
            setAdbInstalled(installed);
        } catch {
            setAdbInstalled(false);
        }
    }, []);

    useEffect(() => {
        void refreshTargets();
        void checkAdb();

        EventsOn("devices:changed", (updated: devices.SyncTargetView[]) => {
            if (updated) {
                setTargets(updated);
                setSelectedId((curr) => (updated.some((item) => item.id === curr) ? curr : updated[0]?.id || ""));
            }
        });

        EventsOn("sync:progress", (event: ProgressEvent) => {
            setSyncProgress(event);
            if (event.phase === "starting" || event.phase === "transcoding" || event.phase === "transferring" || event.phase === "deleting") {
                setSyncing(true);
            } else if (event.phase === "done") {
                setSyncing(false);
                toast.success(t("translation.sync.statusDone"));
                window.setTimeout(() => setSyncProgress(null), 3000);
            } else if (event.phase === "cancelled") {
                setSyncing(false);
                toast.info(t("translation.sync.statusCancelled"));
            } else if (event.phase === "error") {
                setSyncing(false);
                toast.error(event.name || t("translation.sync.statusError", { error: "Unknown" }));
            }
        });

        EventsOn("platform-tools:progress", (data: { percent: number; status: string }) => {
            setAdbDownloadProgress(data.percent);
            setAdbDownloadStatus(data.status);
            if (data.percent >= 100) {
                setAdbDownloading(false);
                setAdbInstalled(true);
                toast.success(t("translation.sync.adbInstalled"));
                void refreshTargets();
            }
        });

        return () => {
            EventsOff("devices:changed");
            EventsOff("sync:progress");
            EventsOff("platform-tools:progress");
        };
    }, [refreshTargets, checkAdb]);

    const activeTarget = targets.find((t) => t.id === selectedId) || null;

    // Load profile when active target changes
    useEffect(() => {
        if (!selectedId) {
            setProfile(null);
            return;
        }
        void GetDeviceProfile(selectedId).then((p) => {
            setProfile(p);
        }).catch((err) => {
            console.error("Failed to get device profile:", err);
        });
    }, [selectedId]);

    const handleSaveProfile = async () => {
        if (!profile) return;
        try {
            await SaveDeviceProfile(profile);
            toast.success(t("translation.sync.settingsSaved"));
            await refreshTargets();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleAddFolder = async () => {
        try {
            const folder = await SelectFolder();
            if (!folder) return;
            const target = await AddFolderTarget("", folder);
            toast.success(t("translation.sync.connected"));
            await refreshTargets();
            if (target?.id) {
                setSelectedId(target.id);
            }
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleRemoveFolder = async (id: string) => {
        try {
            await RemoveFolderTarget(id);
            await refreshTargets();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleDownloadAdb = async () => {
        setAdbDownloading(true);
        setAdbDownloadProgress(0);
        setAdbDownloadStatus("Starting download...");
        try {
            await DownloadPlatformTools();
        } catch (err) {
            setAdbDownloading(false);
            toast.error(String(err));
        }
    };

    const handlePreviewSync = async () => {
        if (!activeTarget) return;
        setPlanning(true);
        try {
            const previewPlan = await PlanSync(activeTarget.id);
            setPlan(previewPlan);
            setShowPlanDialog(true);
        } catch (err) {
            toast.error(String(err));
        } finally {
            setPlanning(false);
        }
    };

    const handleStartSync = async () => {
        if (!activeTarget) return;
        setShowPlanDialog(false);
        setSyncing(true);
        try {
            await StartSync(activeTarget.id);
        } catch (err) {
            setSyncing(false);
            toast.error(String(err));
        }
    };

    const handleCancelSync = async () => {
        try {
            await CancelSync();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleResumeSync = async () => {
        if (!activeTarget) return;
        setSyncing(true);
        try {
            await ResumeSync(activeTarget.id);
        } catch (err) {
            setSyncing(false);
            toast.error(String(err));
        }
    };

    return (
        <div className="flex flex-col gap-6">
            {/* Top Toolbar */}
            <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={() => void refreshTargets()}>
                        <RefreshCw className="mr-1.5 size-3.5" />
                        {t("translation.common.refresh")}
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => void handleAddFolder()}>
                        <FolderPlus className="mr-1.5 size-3.5" />
                        {t("translation.sync.addFolder")}
                    </Button>
                </div>
                <Button
                    variant="ghost"
                    size="sm"
                    className="text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => setShowHelp((prev) => !prev)}
                >
                    <HelpCircle className="mr-1.5 size-3.5" />
                    {t("translation.sync.usbHelpTitle")}
                </Button>
            </div>

            {/* ADB Missing Notice Banner */}
            {!adbInstalled && (
                <div className="flex flex-col gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-foreground dark:text-amber-200">
                    <div className="flex items-center justify-between gap-4">
                        <div className="flex items-center gap-2.5">
                            <AlertCircle className="size-4 shrink-0 text-amber-500" />
                            <div>
                                <p className="font-medium">{t("translation.sync.downloadAdb")}</p>
                                <p className="text-xs text-muted-foreground">{t("translation.sync.downloadAdbHint")}</p>
                            </div>
                        </div>
                        <Button
                            size="sm"
                            variant="default"
                            disabled={adbDownloading}
                            onClick={() => void handleDownloadAdb()}
                        >
                            <Download className="mr-1.5 size-3.5" />
                            {adbDownloading ? t("translation.sync.downloadingAdb") : t("translation.sync.downloadAdb")}
                        </Button>
                    </div>
                    {adbDownloading && (
                        <div className="mt-2 flex flex-col gap-1">
                            <Progress value={adbDownloadProgress} className="h-1.5" />
                            <div className="flex justify-between text-xs text-muted-foreground">
                                <span>{adbDownloadStatus}</span>
                                <span>{adbDownloadProgress}%</span>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* USB Setup Guide */}
            {showHelp && (
                <div className="flex flex-col gap-2 rounded-lg border bg-muted/30 p-4 text-xs leading-relaxed text-muted-foreground">
                    <div className="flex items-center justify-between pb-1">
                        <span className="font-semibold text-foreground">{t("translation.sync.usbHelpTitle")}</span>
                        <Button variant="ghost" size="icon-sm" onClick={() => setShowHelp(false)}>
                            <X className="size-3.5" />
                        </Button>
                    </div>
                    <p>{t("translation.sync.usbHelpStep1")}</p>
                    <p>{t("translation.sync.usbHelpStep2")}</p>
                    <p>{t("translation.sync.usbHelpStep3")}</p>
                    <p className="mt-1 border-t pt-2 text-foreground/80">{t("translation.sync.usbHelpSyncthing")}</p>
                </div>
            )}

            {/* Target Select Tabs / Cards */}
            {targets.length === 0 ? (
                <div className="rounded-lg border border-dashed py-12 text-center">
                    <Smartphone className="mx-auto mb-3 size-8 text-muted-foreground opacity-50" />
                    <p className="text-sm font-medium">{t("translation.sync.emptyDevices")}</p>
                    <p className="mt-1 text-xs text-muted-foreground">{t("translation.sync.emptyDevicesHint")}</p>
                    <Button variant="outline" size="sm" className="mt-4" onClick={() => void handleAddFolder()}>
                        <FolderPlus className="mr-1.5 size-3.5" />
                        {t("translation.sync.addFolder")}
                    </Button>
                </div>
            ) : (
                <div className="flex flex-col gap-6">
                    {/* Device selector buttons if multiple */}
                    {targets.length > 1 && (
                        <div className="flex flex-wrap gap-2 border-b pb-2">
                            {targets.map((tgt) => (
                                <button
                                    key={tgt.id}
                                    type="button"
                                    onClick={() => setSelectedId(tgt.id)}
                                    className={cn(
                                        "flex items-center gap-2 rounded-md px-3 py-1.5 text-xs font-medium transition-colors",
                                        tgt.id === selectedId
                                            ? "bg-primary text-primary-foreground"
                                            : "bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground",
                                    )}
                                >
                                    {tgt.kind === "adb" && <Smartphone className="size-3.5" />}
                                    {tgt.kind === "massstorage" && <HardDrive className="size-3.5" />}
                                    {tgt.kind === "folder" && <Folder className="size-3.5" />}
                                    <span>{tgt.name || tgt.model || tgt.id}</span>
                                </button>
                            ))}
                        </div>
                    )}

                    {activeTarget && profile && (
                        <div className="flex flex-col gap-6">
                            {/* Device Overview Card */}
                            <div className="flex flex-col gap-4 rounded-lg border bg-card p-5">
                                <div className="flex flex-wrap items-start justify-between gap-4">
                                    <div className="flex items-center gap-3">
                                        <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                            {activeTarget.kind === "adb" && <Smartphone className="size-5" />}
                                            {activeTarget.kind === "massstorage" && <HardDrive className="size-5" />}
                                            {activeTarget.kind === "folder" && <Folder className="size-5" />}
                                        </div>
                                        <div>
                                            <div className="flex items-center gap-2">
                                                <h2 className="text-base font-semibold">{activeTarget.name}</h2>
                                                <span
                                                    className={cn(
                                                        "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium",
                                                        activeTarget.connected
                                                            ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                                                            : "bg-zinc-500/10 text-zinc-600 dark:text-zinc-400",
                                                    )}
                                                >
                                                    {activeTarget.connected
                                                        ? t("translation.sync.connected")
                                                        : t("translation.sync.offline")}
                                                </span>
                                            </div>
                                            <p className="font-mono text-xs text-muted-foreground">{activeTarget.root}</p>
                                        </div>
                                    </div>

                                    {activeTarget.kind === "folder" && (
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            className="text-xs text-destructive hover:bg-destructive/10"
                                            onClick={() => void handleRemoveFolder(activeTarget.id)}
                                        >
                                            <Trash2 className="mr-1.5 size-3.5" />
                                            {t("translation.sync.removeTarget")}
                                        </Button>
                                    )}
                                </div>

                                {activeTarget.totalBytes > 0 && (
                                    <div className="flex flex-col gap-1.5">
                                        <div className="flex justify-between text-xs text-muted-foreground">
                                            <span>
                                                {t("translation.sync.freeOf", {
                                                    free: formatBytes(activeTarget.freeBytes),
                                                    total: formatBytes(activeTarget.totalBytes),
                                                })}
                                            </span>
                                            <span>
                                                {Math.round(
                                                    ((activeTarget.totalBytes - activeTarget.freeBytes) /
                                                        activeTarget.totalBytes) *
                                                        100,
                                                )}
                                                %
                                            </span>
                                        </div>
                                        <Progress
                                            value={
                                                ((activeTarget.totalBytes - activeTarget.freeBytes) /
                                                    activeTarget.totalBytes) *
                                                100
                                            }
                                            className="h-2"
                                        />
                                    </div>
                                )}
                            </div>

                            {/* Settings Form */}
                            <div className="flex flex-col gap-4 rounded-lg border bg-card p-5">
                                <div className="flex items-center gap-2 border-b pb-3">
                                    <Sliders className="size-4 text-muted-foreground" />
                                    <h3 className="text-sm font-semibold">{t("translation.settings.title")}</h3>
                                </div>

                                <div className="grid gap-4 sm:grid-cols-2">
                                    {/* Format Policy */}
                                    <div className="flex flex-col gap-1.5">
                                        <label className="text-xs font-medium">{t("translation.sync.formatPolicy")}</label>
                                        <Select
                                            value={profile.format_policy?.mode || "keep"}
                                            onValueChange={(val) => {
                                                setProfile({
                                                    ...profile,
                                                    format_policy: { ...profile.format_policy, mode: val },
                                                });
                                            }}
                                        >
                                            <SelectTrigger className="h-9 text-xs">
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="keep">{t("translation.sync.formatKeep")}</SelectItem>
                                                <SelectItem value="opus">{t("translation.sync.formatOpus")}</SelectItem>
                                                <SelectItem value="aac">{t("translation.sync.formatAac")}</SelectItem>
                                                <SelectItem value="mp3">{t("translation.sync.formatMp3")}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                        <p className="text-[11px] text-muted-foreground">
                                            {profile.format_policy?.mode === "keep" && t("translation.sync.formatKeepDesc")}
                                            {profile.format_policy?.mode === "opus" && t("translation.sync.formatOpusDesc")}
                                            {profile.format_policy?.mode === "aac" && t("translation.sync.formatAacDesc")}
                                            {profile.format_policy?.mode === "mp3" && t("translation.sync.formatMp3Desc")}
                                        </p>
                                    </div>

                                    {/* Profile Layout */}
                                    <div className="flex flex-col gap-1.5">
                                        <label className="text-xs font-medium">{t("translation.sync.profile")}</label>
                                        <Select
                                            value={profile.profile_id || "poweramp"}
                                            onValueChange={(val) => {
                                                setProfile({ ...profile, profile_id: val });
                                            }}
                                        >
                                            <SelectTrigger className="h-9 text-xs">
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="poweramp">{t("translation.sync.profilePoweramp")}</SelectItem>
                                                <SelectItem value="mediastore">{t("translation.sync.profileMediaStore")}</SelectItem>
                                                <SelectItem value="rockbox">{t("translation.sync.profileRockbox")}</SelectItem>
                                                <SelectItem value="mediaserver">{t("translation.sync.profileMediaServer")}</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>

                                    {/* Target Folder Root */}
                                    <div className="flex flex-col gap-1.5 sm:col-span-2">
                                        <label className="text-xs font-medium">{t("translation.sync.targetFolder")}</label>
                                        <Input
                                            value={profile.target_folder || ""}
                                            onChange={(e) => setProfile({ ...profile, target_folder: e.target.value })}
                                            className="h-9 text-xs font-mono"
                                            placeholder="/sdcard/Music"
                                        />
                                    </div>
                                </div>

                                <div className="flex justify-end pt-2">
                                    <Button size="sm" variant="outline" onClick={() => void handleSaveProfile()}>
                                        <Check className="mr-1.5 size-3.5" />
                                        {t("translation.sync.saveSettings")}
                                    </Button>
                                </div>
                            </div>

                            {/* Sync Actions Bar */}
                            <div className="flex flex-col gap-3 rounded-lg border bg-card p-5">
                                <div className="flex flex-wrap items-center justify-between gap-3">
                                    <div className="flex items-center gap-2">
                                        <Button
                                            variant="outline"
                                            size="sm"
                                            disabled={planning || syncing}
                                            onClick={() => void handlePreviewSync()}
                                        >
                                            <RefreshCw className={cn("mr-1.5 size-3.5", planning && "animate-spin")} />
                                            {t("translation.sync.preview")}
                                        </Button>
                                        <Button
                                            variant="default"
                                            size="sm"
                                            disabled={syncing || !activeTarget.connected}
                                            onClick={() => void handleStartSync()}
                                        >
                                            <Play className="mr-1.5 size-3.5" />
                                            {t("translation.sync.syncNow")}
                                        </Button>
                                    </div>

                                    {syncing && (
                                        <Button variant="destructive" size="sm" onClick={() => void handleCancelSync()}>
                                            <X className="mr-1.5 size-3.5" />
                                            {t("translation.sync.cancel")}
                                        </Button>
                                    )}
                                </div>

                                {/* Active Progress Bar */}
                                {syncProgress && (
                                    <div className="mt-2 flex flex-col gap-2 rounded-md bg-muted/40 p-3 text-xs">
                                        <div className="flex items-center justify-between">
                                            <span className="font-medium text-foreground">
                                                {syncProgress.phase === "transcoding" && t("translation.sync.statusTranscoding")}
                                                {syncProgress.phase === "transferring" && t("translation.sync.statusTransferring")}
                                                {syncProgress.phase === "deleting" && t("translation.sync.statusCleaning")}
                                                {syncProgress.phase === "starting" && t("translation.sync.statusStarting")}
                                                {syncProgress.phase === "done" && t("translation.sync.statusDone")}
                                                {syncProgress.phase === "cancelled" && t("translation.sync.statusCancelled")}
                                                {syncProgress.phase === "error" && t("translation.sync.statusError", { error: syncProgress.name })}
                                            </span>
                                            {syncProgress.opTotal ? (
                                                <span className="tabular-nums text-muted-foreground">
                                                    {syncProgress.done || 0} / {syncProgress.opTotal}
                                                </span>
                                            ) : null}
                                        </div>
                                        {syncProgress.name && (
                                            <p className="truncate font-mono text-[11px] text-muted-foreground">{syncProgress.name}</p>
                                        )}
                                        {syncProgress.opTotal ? (
                                            <Progress
                                                value={((syncProgress.done || 0) / syncProgress.opTotal) * 100}
                                                className="h-1.5"
                                            />
                                        ) : null}
                                    </div>
                                )}
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Preview Plan Modal Dialog */}
            <Dialog open={showPlanDialog} onOpenChange={setShowPlanDialog}>
                <DialogContent className="sm:max-w-xl">
                    <DialogHeader>
                        <DialogTitle>{t("translation.sync.planTitle")}</DialogTitle>
                        <DialogDescription>
                            {plan &&
                                t("translation.sync.planSummary", {
                                    adds: plan.adds || 0,
                                    updates: plan.updates || 0,
                                    moves: plan.moves || 0,
                                    deletes: plan.deletes || 0,
                                })}
                        </DialogDescription>
                    </DialogHeader>

                    {plan && (
                        <div className="flex max-h-[50vh] flex-col gap-3 overflow-y-auto pr-1 text-xs">
                            <div className="flex items-center justify-between rounded bg-muted/50 p-2 font-medium">
                                <span>{t("translation.sync.planSize", { size: formatBytes(plan.freeNeeded || 0) })}</span>
                                <span>
                                    {t("translation.sync.freeOf", {
                                        free: formatBytes(plan.freeAvailable || 0),
                                        total: formatBytes(plan.freeAvailable || 0),
                                    })}
                                </span>
                            </div>

                            {plan.freeNeeded > plan.freeAvailable && plan.freeAvailable > 0 && (
                                <div className="flex items-center gap-2 rounded bg-amber-500/10 p-2 text-amber-600 dark:text-amber-400">
                                    <AlertCircle className="size-4 shrink-0" />
                                    <span>
                                        {t("translation.sync.planSpaceWarning", {
                                            free: formatBytes(plan.freeAvailable),
                                            needed: formatBytes(plan.freeNeeded),
                                        })}
                                    </span>
                                </div>
                            )}

                            {plan.groups && plan.groups.length > 0 ? (
                                <div className="flex flex-col gap-2">
                                    {plan.groups.slice(0, 100).map((grp, idx) => (
                                        <div key={idx} className="flex flex-col rounded border p-2">
                                            <div className="flex items-center justify-between font-semibold">
                                                <span>{grp.album || "Unknown Album"}</span>
                                                <span className="text-[10px] text-muted-foreground">{grp.artist}</span>
                                            </div>
                                            <div className="mt-1 flex flex-col gap-1 pl-2 text-[11px] text-muted-foreground">
                                                {grp.ops?.map((op, oidx) => (
                                                    <div key={oidx} className="flex items-center gap-2">
                                                        <span
                                                            className={cn(
                                                                "font-mono font-bold text-[10px]",
                                                                op.kind === "add" && "text-emerald-500",
                                                                op.kind === "update" && "text-blue-500",
                                                                op.kind === "move" && "text-amber-500",
                                                                op.kind === "delete" && "text-rose-500",
                                                                op.kind === "keep" && "text-muted-foreground",
                                                            )}
                                                        >
                                                            {op.kind.toUpperCase()}
                                                        </span>
                                                        <span className="truncate">{op.remote_path}</span>
                                                    </div>
                                                ))}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            ) : (
                                <p className="py-6 text-center text-muted-foreground">{t("translation.sync.planEmpty")}</p>
                            )}

                            {plan.deletes > 0 && (
                                <p className="text-[11px] text-muted-foreground italic">
                                    {t("translation.sync.planDeletesNote")}
                                </p>
                            )}
                        </div>
                    )}

                    <DialogFooter>
                        <Button variant="outline" onClick={() => setShowPlanDialog(false)}>
                            {t("translation.common.cancel")}
                        </Button>
                        <Button
                            variant="default"
                            onClick={() => void handleStartSync()}
                            disabled={!plan || (plan.adds === 0 && plan.updates === 0 && plan.moves === 0 && plan.deletes === 0)}
                        >
                            <Play className="mr-1.5 size-3.5" />
                            {t("translation.sync.syncNow")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
