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
    Download,
    Folder,
    FolderPlus,
    HardDrive,
    HelpCircle,
    Play,
    RefreshCw,
    RotateCcw,
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
    error?: string;
}

export function AndroidSyncPanel() {
    const [targets, setTargets] = useState<devices.SyncTargetView[]>([]);
    const [selectedId, setSelectedId] = useState<string>("");
    const [profile, setProfile] = useState<devices.DeviceProfile | null>(null);
    const [plan, setPlan] = useState<syncengine.Plan | null>(null);
    const [showPlanDialog, setShowPlanDialog] = useState(false);
    const [planning, setPlanning] = useState(false);
    const [syncing, setSyncing] = useState(false);
    const [syncProgress, setSyncProgress] = useState<ProgressEvent | null>(null);
    const [adbInstalled, setAdbInstalled] = useState<boolean | null>(null);
    const [adbDownloading, setAdbDownloading] = useState(false);
    const [adbProgress, setAdbProgress] = useState<{ percent: number; status: string }>({ percent: 0, status: "" });
    const [showHelp, setShowHelp] = useState(false);

    const refreshTargets = useCallback(async () => {
        try {
            const list = await ListSyncTargets();
            const views = (list || []) as devices.SyncTargetView[];
            setTargets(views);
            setSelectedId((prev) => {
                if (views.some((v) => v.id === prev)) return prev;
                return views[0]?.id || "";
            });
        } catch (err) {
            console.error("Failed to list sync targets:", err);
        }
    }, []);

    useEffect(() => {
        let mounted = true;
        const pull = () => {
            void ListSyncTargets().then((list) => {
                if (!mounted) return;
                const views = (list || []) as devices.SyncTargetView[];
                setTargets(views);
                setSelectedId((prev) => (views.some((v) => v.id === prev) ? prev : views[0]?.id || ""));
            }).catch((err) => {
                console.error("Failed to list sync targets:", err);
            });
        };
        const check = () => {
            void IsPlatformToolsInstalled().then((ok) => {
                if (!mounted) return;
                setAdbInstalled(ok);
            }).catch(() => {
                if (!mounted) return;
                setAdbInstalled(false);
            });
        };

        pull();
        check();

        const handleDevices = () => {
            pull();
        };

        EventsOn("devices:changed", handleDevices);

        EventsOn("sync:progress", (event: ProgressEvent) => {
            setSyncProgress(event);
            if (event?.phase === "done" || event?.phase === "error" || event?.phase === "cancelled") {
                setSyncing(false);
                if (event.phase === "done") {
                    toast.success(t("translation.sync.statusDone"));
                } else if (event.phase === "cancelled") {
                    toast.info(t("translation.sync.statusCancelled"));
                } else if (event.phase === "error") {
                    toast.error(t("translation.sync.statusError", { error: event.error || "" }));
                }
                pull();
            }
        });

        EventsOn("platform-tools:progress", (data: { percent: number; status: string }) => {
            setAdbProgress(data);
            if (data.percent >= 100) {
                setAdbDownloading(false);
                setAdbInstalled(true);
                toast.success(t("translation.sync.adbInstalled"));
                pull();
            }
        });

        return () => {
            mounted = false;
            EventsOff("devices:changed");
            EventsOff("sync:progress");
            EventsOff("platform-tools:progress");
        };
    }, []);

    const activeTarget = targets.find((t) => t.id === selectedId) || null;

    useEffect(() => {
        let cancel = false;
        if (!activeTarget) {
            void Promise.resolve().then(() => {
                if (!cancel) setProfile(null);
            });
            return () => {
                cancel = true;
            };
        }
        void GetDeviceProfile(activeTarget.id).then((prof) => {
            if (cancel) return;
            setProfile(prof ? devices.DeviceProfile.createFrom(prof) : null);
        }).catch((err) => {
            console.error("Failed to fetch profile:", err);
        });
        return () => {
            cancel = true;
        };
    }, [activeTarget]);

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

    const handleDownloadAdb = async () => {
        setAdbDownloading(true);
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
            const p = await PlanSync(activeTarget.id);
            setPlan(p ? syncengine.Plan.createFrom(p) : null);
            setShowPlanDialog(true);
        } catch (err) {
            toast.error(String(err));
        } finally {
            setPlanning(false);
        }
    };

    const handleStartSync = async () => {
        if (!activeTarget) return;
        setSyncing(true);
        setShowPlanDialog(false);
        try {
            await StartSync(activeTarget.id);
        } catch (err) {
            setSyncing(false);
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

    const handleCancelSync = async () => {
        try {
            await CancelSync();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleAddFolder = async () => {
        try {
            const folder = await SelectFolder("");
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
            toast.success(t("translation.sync.statusDone"));
            await refreshTargets();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const updateProfile = (patch: Partial<devices.DeviceProfile>) => {
        if (!profile) return;
        setProfile(devices.DeviceProfile.createFrom({ ...profile, ...patch }));
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

                <div className="flex items-center gap-2">
                    <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setShowHelp(!showHelp)}
                        className="text-xs text-muted-foreground"
                    >
                        <HelpCircle className="mr-1.5 size-3.5" />
                        {t("translation.sync.usbHelpTitle")}
                    </Button>
                </div>
            </div>

            {/* ADB Missing / Download Banner */}
            {adbInstalled === false && (
                <div className="flex flex-col gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-xs dark:bg-amber-950/20">
                    <div className="flex items-start justify-between gap-3">
                        <div className="flex items-start gap-2.5">
                            <AlertCircle className="mt-0.5 size-4 text-amber-600 dark:text-amber-400" />
                            <div className="flex flex-col gap-1">
                                <span className="font-semibold text-foreground">{t("translation.sync.downloadAdb")}</span>
                                <span className="text-muted-foreground">{t("translation.sync.downloadAdbHint")}</span>
                            </div>
                        </div>
                        <Button
                            size="sm"
                            variant="default"
                            disabled={adbDownloading}
                            onClick={() => void handleDownloadAdb()}
                            className="shrink-0"
                        >
                            <Download className="mr-1.5 size-3.5" />
                            {adbDownloading ? t("translation.sync.downloadingAdb") : t("translation.sync.downloadAdb")}
                        </Button>
                    </div>
                    {adbDownloading && (
                        <div className="mt-2 flex flex-col gap-1.5">
                            <Progress value={adbProgress.percent} className="h-1.5 w-full" />
                            <span className="font-mono text-[10px] text-muted-foreground">{adbProgress.status} ({adbProgress.percent}%)</span>
                        </div>
                    )}
                </div>
            )}

            {/* Setup Guide Accordion */}
            {showHelp && (
                <div className="flex flex-col gap-2 rounded-lg border bg-muted/30 p-4 text-xs">
                    <h3 className="font-semibold text-foreground">{t("translation.sync.usbHelpTitle")}</h3>
                    <p className="text-muted-foreground">{t("translation.sync.usbHelpStep1")}</p>
                    <p className="text-muted-foreground">{t("translation.sync.usbHelpStep2")}</p>
                    <p className="text-muted-foreground">{t("translation.sync.usbHelpStep3")}</p>
                    <div className="mt-2 border-t pt-2 text-muted-foreground">
                        <span className="font-medium text-foreground">{t("translation.sync.usbHelpSyncthing")}</span>
                    </div>
                </div>
            )}

            {/* Device Targets Selector */}
            {targets.length === 0 ? (
                <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-center">
                    <Smartphone className="size-10 text-muted-foreground/60" />
                    <h3 className="mt-3 text-sm font-semibold">{t("translation.sync.emptyDevices")}</h3>
                    <p className="mt-1 max-w-sm text-xs text-muted-foreground">{t("translation.sync.emptyDevicesHint")}</p>
                    <Button variant="outline" size="sm" onClick={() => void handleAddFolder()} className="mt-4">
                        <FolderPlus className="mr-1.5 size-3.5" />
                        {t("translation.sync.addFolder")}
                    </Button>
                </div>
            ) : (
                <div className="flex flex-col gap-4">
                    {/* Device Selector Cards */}
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                        {targets.map((tgt) => {
                            const isSelected = tgt.id === selectedId;
                            const usedPercent = tgt.totalBytes > 0
                                ? Math.max(0, Math.min(100, ((tgt.totalBytes - tgt.freeBytes) / tgt.totalBytes) * 100))
                                : 0;
                            return (
                                <button
                                    key={tgt.id}
                                    type="button"
                                    onClick={() => setSelectedId(tgt.id)}
                                    className={cn(
                                        "flex flex-col gap-2.5 rounded-lg border p-3.5 text-left transition-colors cursor-pointer",
                                        isSelected
                                            ? "border-primary bg-primary/5 shadow-xs"
                                            : "border-border bg-card hover:bg-muted/40",
                                    )}
                                >
                                    <div className="flex items-start justify-between gap-2">
                                        <div className="flex items-center gap-2 min-w-0">
                                            {tgt.kind === "adb" && <Smartphone className="size-4 shrink-0 text-primary" />}
                                            {tgt.kind === "drive" && <HardDrive className="size-4 shrink-0 text-primary" />}
                                            {tgt.kind === "folder" && <Folder className="size-4 shrink-0 text-primary" />}
                                            <span className="font-semibold text-xs truncate">
                                                {tgt.name || tgt.model || tgt.id}
                                            </span>
                                        </div>
                                        <span
                                            className={cn(
                                                "shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium",
                                                tgt.connected
                                                    ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                                                    : "bg-muted text-muted-foreground",
                                            )}
                                        >
                                            {tgt.connected ? t("translation.sync.connected") : t("translation.sync.offline")}
                                        </span>
                                    </div>

                                    <div className="text-[11px] text-muted-foreground truncate">
                                        {tgt.kind === "adb" && (tgt.model || t("translation.sync.kindAdb"))}
                                        {tgt.kind === "drive" && (tgt.root || t("translation.sync.kindDrive"))}
                                        {tgt.kind === "folder" && (tgt.root || t("translation.sync.kindFolder"))}
                                    </div>

                                    {/* Storage bar */}
                                    {tgt.totalBytes > 0 && (
                                        <div className="mt-1 flex flex-col gap-1">
                                            <div className="flex justify-between text-[10px] text-muted-foreground font-mono">
                                                <span>{formatBytes(tgt.freeBytes)} free</span>
                                                <span>{formatBytes(tgt.totalBytes)}</span>
                                            </div>
                                            <Progress value={usedPercent} className="h-1" />
                                        </div>
                                    )}
                                </button>
                            );
                        })}
                    </div>

                    {/* Active Target Configuration Panel */}
                    {activeTarget && (
                        <div className="flex flex-col gap-5 rounded-lg border bg-card p-5">
                            {/* Target Header */}
                            <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-4">
                                <div className="flex items-center gap-2.5">
                                    {activeTarget.kind === "adb" && <Smartphone className="size-5 text-primary" />}
                                    {activeTarget.kind === "drive" && <HardDrive className="size-5 text-primary" />}
                                    {activeTarget.kind === "folder" && <Folder className="size-5 text-primary" />}
                                    <div>
                                        <h2 className="text-sm font-semibold">{activeTarget.name || activeTarget.model}</h2>
                                        <p className="text-xs text-muted-foreground font-mono">{activeTarget.root || activeTarget.id}</p>
                                    </div>
                                </div>

                                {activeTarget.kind === "folder" && (
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => void handleRemoveFolder(activeTarget.id)}
                                        className="text-destructive hover:bg-destructive/10"
                                    >
                                        <Trash2 className="mr-1.5 size-3.5" />
                                        {t("translation.sync.removeTarget")}
                                    </Button>
                                )}
                            </div>

                            {/* Profile & Formatting Settings */}
                            {profile && (
                                <div className="flex flex-col gap-4">
                                    <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                                        <Sliders className="size-3.5" />
                                        <span>{t("translation.sync.profile")}</span>
                                    </div>

                                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                        {/* Format Policy */}
                                        <div className="flex flex-col gap-1.5">
                                            <label className="text-xs font-medium">{t("translation.sync.formatPolicy")}</label>
                                            <Select
                                                value={profile.format_policy?.mode || "keep"}
                                                onValueChange={(val) => {
                                                    updateProfile({
                                                        format_policy: syncengine.FormatPolicy.createFrom({ mode: val }),
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
                                                    updateProfile({ profile_id: val });
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
                                                onChange={(e) => updateProfile({ target_folder: e.target.value })}
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
                            )}

                            {/* Plan & Sync Actions */}
                            <div className="flex flex-col gap-3 border-t pt-4">
                                <div className="flex flex-wrap items-center justify-between gap-3">
                                    <div className="flex flex-wrap items-center gap-2">
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
                                        <Button
                                            variant="outline"
                                            size="sm"
                                            disabled={syncing || !activeTarget.connected}
                                            onClick={() => void handleResumeSync()}
                                        >
                                            <RotateCcw className="mr-1.5 size-3.5" />
                                            {t("translation.sync.resume")}
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
                                                {syncProgress.phase === "done" && t("translation.sync.statusDone")}
                                                {syncProgress.phase === "starting" && t("translation.sync.statusStarting")}
                                                {syncProgress.phase === "cancelled" && t("translation.sync.statusCancelled")}
                                                {syncProgress.phase === "error" && t("translation.sync.statusError", { error: syncProgress.error || "" })}
                                            </span>
                                            {syncProgress.opTotal && syncProgress.opTotal > 0 && (
                                                <span className="font-mono text-[10px] text-muted-foreground">
                                                    {syncProgress.done || 0} / {syncProgress.opTotal}
                                                </span>
                                            )}
                                        </div>

                                        {syncProgress.bytesTotal && syncProgress.bytesTotal > 0 && (
                                            <Progress
                                                value={Math.round(((syncProgress.bytesDone || 0) / syncProgress.bytesTotal) * 100)}
                                                className="h-1.5 w-full"
                                            />
                                        )}

                                        {syncProgress.name && (
                                            <span className="font-mono text-[11px] text-muted-foreground truncate">
                                                {syncProgress.name}
                                            </span>
                                        )}
                                    </div>
                                )}
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Plan Preview Modal */}
            <Dialog open={showPlanDialog} onOpenChange={setShowPlanDialog}>
                <DialogContent className="sm:max-w-2xl">
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
                                <span>{t("translation.sync.planSize", { size: formatBytes(plan.free_needed || 0) })}</span>
                                <span>
                                    {t("translation.sync.freeOf", {
                                        free: formatBytes(plan.free_available || 0),
                                        total: formatBytes(plan.free_available || 0),
                                    })}
                                </span>
                            </div>

                            {plan.free_needed > plan.free_available && plan.free_available > 0 && (
                                <div className="flex items-center gap-2 rounded bg-amber-500/10 p-2 text-amber-600 dark:text-amber-400">
                                    <AlertCircle className="size-4 shrink-0" />
                                    <span>
                                        {t("translation.sync.planSpaceWarning", {
                                            free: formatBytes(plan.free_available),
                                            needed: formatBytes(plan.free_needed),
                                        })}
                                    </span>
                                </div>
                            )}

                            {plan.groups && plan.groups.length > 0 ? (
                                <div className="flex flex-col gap-2">
                                    {plan.groups.slice(0, 100).map((grp, idx) => {
                                        const groupOps = (plan.ops || []).filter((op) => op.album === grp.album);
                                        return (
                                            <div key={idx} className="flex flex-col rounded border p-2">
                                                <div className="flex items-center justify-between font-semibold">
                                                    <span>{grp.album || "Unknown Album"}</span>
                                                    <div className="flex items-center gap-2 font-mono text-[11px]">
                                                        {grp.adds > 0 && <span className="text-emerald-500">+{grp.adds}</span>}
                                                        {grp.updates > 0 && <span className="text-blue-500">~{grp.updates}</span>}
                                                        {grp.moves > 0 && <span className="text-amber-500">→{grp.moves}</span>}
                                                        {grp.deletes > 0 && <span className="text-rose-500">-{grp.deletes}</span>}
                                                        {grp.keeps > 0 && <span className="text-muted-foreground">={grp.keeps}</span>}
                                                    </div>
                                                </div>
                                                {groupOps.length > 0 && (
                                                    <div className="mt-1 flex flex-col gap-1 pl-2 text-[11px] text-muted-foreground">
                                                        {groupOps.slice(0, 5).map((op, oidx) => (
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
                                                                <span className="truncate">{op.remote}</span>
                                                            </div>
                                                        ))}
                                                        {groupOps.length > 5 && (
                                                            <span className="text-[10px] text-muted-foreground/60 italic">
                                                                + {groupOps.length - 5} more tracks...
                                                            </span>
                                                        )}
                                                    </div>
                                                )}
                                            </div>
                                        );
                                    })}
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

                    <DialogFooter className="gap-2">
                        <Button variant="outline" onClick={() => setShowPlanDialog(false)}>
                            {t("translation.common.cancel")}
                        </Button>
                        <Button
                            disabled={!activeTarget?.connected || syncing || (plan?.adds === 0 && plan?.updates === 0 && plan?.moves === 0 && plan?.deletes === 0)}
                            onClick={() => void handleStartSync()}
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
