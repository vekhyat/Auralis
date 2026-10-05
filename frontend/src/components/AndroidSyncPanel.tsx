import { useCallback, useEffect, useState } from "react";
import { t } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { cn } from "@/lib/utils";
import {
    AddFolderTarget,
    CancelSync,
    DownloadPlatformTools,
    GetDeviceProfile,
    HasInterruptedSync,
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
import { EventsOn } from "../../wailsjs/runtime/runtime";
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
import { SelectionRulesEditor } from "./sync/SelectionRulesEditor";
import type { ProgressEvent } from "./sync/types";
import { validateRecentDays, validateTargetFolder } from "./sync/validation";

function formatBytes(value: number): string {
    if (!value || value <= 0) return "0 MB";
    const gb = value / (1024 * 1024 * 1024);
    if (gb >= 10) return `${Math.round(gb)} GB`;
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    return `${Math.max(1, Math.round(value / (1024 * 1024)))} MB`;
}

const PHASE_LABELS: Record<string, string> = {
    copy: "translation.sync.statusTransferring",
    move: "translation.sync.statusTransferring",
    progress: "translation.sync.statusTransferring",
    skipped: "translation.sync.statusTransferring",
    delete: "translation.sync.statusCleaning",
    manifest: "translation.sync.statusCleaning",
    starting: "translation.sync.statusStarting",
    done: "translation.sync.statusDone",
    cancelled: "translation.sync.statusCancelled",
};

function getProfileSnapshot(p: devices.DeviceProfile | null, targetRoot: string = ""): string {
    if (!p) return "";
    return JSON.stringify({
        root: targetRoot,
        folder: p.target_folder || "",
        mode: p.format_policy?.mode || "keep",
        profile_id: p.profile_id || "poweramp",
        rules: {
            whole_library: p.selection_rules?.whole_library ?? true,
            artists: [...(p.selection_rules?.artists || [])].sort(),
            albums: [...(p.selection_rules?.albums || [])].sort(),
            playlists: [...(p.selection_rules?.playlists || [])].sort(),
            recent_days: p.selection_rules?.recent_days || 0,
        },
    });
}

export function AndroidSyncPanel() {
    const [targets, setTargets] = useState<devices.SyncTargetView[]>([]);
    const [selectedId, setSelectedId] = useState<string>("");
    const [profile, setProfile] = useState<devices.DeviceProfile | null>(null);
    const [plan, setPlan] = useState<syncengine.Plan | null>(null);
    const [lastPlannedSnapshot, setLastPlannedSnapshot] = useState<string>("");
    const [showPlanDialog, setShowPlanDialog] = useState(false);
    const [planning, setPlanning] = useState(false);
    const [syncing, setSyncing] = useState(false);
    const [hasInterruptedSync, setHasInterruptedSync] = useState(false);
    const [syncProgress, setSyncProgress] = useState<ProgressEvent | null>(null);
    const [adbInstalled, setAdbInstalled] = useState<boolean | null>(null);
    const [adbDownloading, setAdbDownloading] = useState(false);
    const [adbProgress, setAdbProgress] = useState<{ percent: number; status: string }>({ percent: 0, status: "" });
    const [showHelp, setShowHelp] = useState(false);

    const activeTarget = targets.find((t) => t.id === selectedId) || null;

    const checkInterrupted = useCallback(async (targetId: string) => {
        if (!targetId) {
            setHasInterruptedSync(false);
            return;
        }
        try {
            const has = await HasInterruptedSync(targetId);
            setHasInterruptedSync(Boolean(has));
        } catch {
            setHasInterruptedSync(false);
        }
    }, []);

    const refreshTargets = useCallback(async () => {
        try {
            const list = await ListSyncTargets();
            const views = (list || []) as devices.SyncTargetView[];
            setTargets(views);
            setSelectedId((prev) => {
                if (views.some((v) => v.id === prev)) return prev;
                return views[0]?.id || "";
            });
            if (views.length > 0) {
                const nextId = views.some((v) => v.id === selectedId) ? selectedId : views[0].id;
                void checkInterrupted(nextId);
            } else {
                setHasInterruptedSync(false);
            }
        } catch (err) {
            console.error("Failed to list sync targets:", err);
        }
    }, [checkInterrupted, selectedId]);

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

        const unsubscribers = [EventsOn("devices:changed", handleDevices)];

        unsubscribers.push(EventsOn("sync:progress", (event: ProgressEvent) => {
            setSyncProgress((previous) => ({ ...previous, ...event }));
            if (event?.phase === "done" || event?.phase === "error" || event?.phase === "cancelled") {
                setSyncing(false);
                if (event.phase === "done" && event.skipped) {
                    toast.warning(t("translation.sync.doneWithSkipped", { count: event.skipped }));
                } else if (event.phase === "done") {
                    toast.success(t("translation.sync.statusDone"));
                } else if (event.phase === "cancelled") {
                    toast.info(t("translation.sync.statusCancelled"));
                } else if (event.phase === "error") {
                    toast.error(t("translation.sync.statusError", { error: event.error || "" }));
                }
                pull();
                if (selectedId) {
                    void checkInterrupted(selectedId);
                }
            }
        }));

        unsubscribers.push(EventsOn("platform-tools:progress", (data: { percent: number; status: string }) => {
            setAdbProgress(data);
            if (data.percent >= 100) {
                setAdbDownloading(false);
                setAdbInstalled(true);
                toast.success(t("translation.sync.adbInstalled"));
                pull();
            }
        }));

        return () => {
            mounted = false;
            unsubscribers.forEach((unsubscribe) => unsubscribe());
        };
    }, [checkInterrupted, selectedId]);

    useEffect(() => {
        let cancel = false;
        if (!activeTarget) {
            void Promise.resolve().then(() => {
                if (!cancel) {
                    setProfile(null);
                    setPlan(null);
                    setLastPlannedSnapshot("");
                    setHasInterruptedSync(false);
                }
            });
            return () => {
                cancel = true;
            };
        }
        void GetDeviceProfile(activeTarget.id).then((prof) => {
            if (cancel) return;
            if (prof) {
                const loaded = devices.DeviceProfile.createFrom(prof);
                if (!loaded.selection_rules) {
                    loaded.selection_rules = syncengine.Selection.createFrom({
                        whole_library: true,
                        artists: [],
                        albums: [],
                        playlists: [],
                        recent_days: 0,
                    });
                }
                setProfile(loaded);
            } else {
                setProfile(null);
            }
            setPlan(null);
            setLastPlannedSnapshot("");
            void checkInterrupted(activeTarget.id);
        }).catch((err) => {
            console.error("Failed to fetch profile:", err);
        });
        return () => {
            cancel = true;
        };
    }, [activeTarget, checkInterrupted]);

    const isAdb = activeTarget?.kind === "adb";
    const folderValidation = profile ? validateTargetFolder(profile.target_folder || "", isAdb) : { valid: true };
    const recentValidation = profile?.selection_rules ? validateRecentDays(profile.selection_rules.recent_days || 0) : { valid: true };
    const isConfigValid = folderValidation.valid && recentValidation.valid;

    const currentSnapshot = getProfileSnapshot(profile, activeTarget?.root || activeTarget?.id);
    const isPlanStale = Boolean(plan && lastPlannedSnapshot && lastPlannedSnapshot !== currentSnapshot);

    const handleSaveProfile = async () => {
        if (!profile) return;
        if (!isConfigValid) {
            if (!folderValidation.valid && folderValidation.errorKey) {
                toast.error(t(folderValidation.errorKey));
            } else if (!recentValidation.valid && recentValidation.errorKey) {
                toast.error(t(recentValidation.errorKey));
            }
            return;
        }
        try {
            await SaveDeviceProfile(profile);
            setPlan(null);
            setLastPlannedSnapshot("");
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
        if (!activeTarget || !isConfigValid) {
            if (!folderValidation.valid && folderValidation.errorKey) {
                toast.error(t(folderValidation.errorKey));
            }
            return;
        }
        setPlanning(true);
        try {
            if (profile) {
                await SaveDeviceProfile(profile);
            }
            const p = await PlanSync(activeTarget.id);
            setPlan(p ? syncengine.Plan.createFrom(p) : null);
            setLastPlannedSnapshot(getProfileSnapshot(profile, activeTarget.root || activeTarget.id));
            setShowPlanDialog(true);
        } catch (err) {
            toast.error(String(err));
        } finally {
            setPlanning(false);
        }
    };

    const handleStartSync = async () => {
        if (!activeTarget || !isConfigValid) return;
        if (!plan || isPlanStale) {
            await handlePreviewSync();
            return;
        }
        setSyncProgress({ phase: "starting" });
        setSyncing(true);
        setShowPlanDialog(false);
        try {
            await StartSync(activeTarget.id);
        } catch (err) {
            setSyncing(false);
            toast.error(String(err));
            if (activeTarget) {
                void checkInterrupted(activeTarget.id);
            }
        }
    };

    const handleResumeSync = async () => {
        if (!activeTarget) return;
        setSyncProgress({ phase: "starting" });
        setSyncing(true);
        try {
            await ResumeSync(activeTarget.id);
        } catch (err) {
            setSyncing(false);
            toast.error(String(err));
            void checkInterrupted(activeTarget.id);
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

    const handleBrowseTargetFolder = async () => {
        if (!profile) return;
        try {
            const selected = await SelectFolder(profile.target_folder || "");
            if (selected) {
                updateProfile({ target_folder: selected });
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

            {/* Interrupted Sync Alert Banner */}
            {hasInterruptedSync && !syncing && activeTarget && (
                <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-primary/40 bg-primary/10 p-3.5 text-xs">
                    <div className="flex items-start gap-2.5">
                        <RotateCcw className="mt-0.5 size-4 text-primary shrink-0" />
                        <div className="flex flex-col gap-0.5">
                            <span className="font-semibold text-foreground">{t("translation.sync.resumableFound")}</span>
                            <span className="text-muted-foreground">{t("translation.sync.resumableFoundDesc")}</span>
                        </div>
                    </div>
                    <Button
                        size="sm"
                        variant="default"
                        disabled={!activeTarget.connected}
                        onClick={() => void handleResumeSync()}
                        className="shrink-0"
                    >
                        <RotateCcw className="mr-1.5 size-3.5" />
                        {t("translation.sync.resume")}
                    </Button>
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
                    <div
                        role="region"
                        aria-label={t("translation.sync.ariaDeviceList")}
                        className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
                    >
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
                                    aria-pressed={isSelected}
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
                        <div
                            role="region"
                            aria-label={t("translation.sync.ariaTargetProfile")}
                            className="flex flex-col gap-5 rounded-lg border bg-card p-5"
                        >
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
                                <div className="flex flex-col gap-5">
                                    <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                                        <Sliders className="size-3.5" />
                                        <span>{t("translation.sync.profile")}</span>
                                    </div>

                                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                        {/* Format Policy */}
                                        <div className="flex flex-col gap-1.5">
                                            <Label htmlFor="sync-format-select" className="text-xs font-medium">
                                                {t("translation.sync.formatPolicy")}
                                            </Label>
                                            <Select
                                                value={profile.format_policy?.mode || "keep"}
                                                onValueChange={(val) => {
                                                    updateProfile({
                                                        format_policy: syncengine.FormatPolicy.createFrom({ mode: val }),
                                                    });
                                                }}
                                                disabled={syncing}
                                            >
                                                <SelectTrigger id="sync-format-select" className="h-9 text-xs">
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
                                            <Label htmlFor="sync-profile-select" className="text-xs font-medium">
                                                {t("translation.sync.profile")}
                                            </Label>
                                            <Select
                                                value={profile.profile_id || "poweramp"}
                                                onValueChange={(val) => {
                                                    updateProfile({ profile_id: val });
                                                }}
                                                disabled={syncing}
                                            >
                                                <SelectTrigger id="sync-profile-select" className="h-9 text-xs">
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

                                        {/* Target Folder Root with Managed Root Containment */}
                                        <div className="flex flex-col gap-1.5 sm:col-span-2">
                                            <div className="flex items-center justify-between">
                                                <Label htmlFor="sync-target-folder" className="text-xs font-medium">
                                                    {t("translation.sync.targetFolder")}
                                                </Label>
                                                {!folderValidation.valid && folderValidation.errorKey && (
                                                    <span className="text-[11px] text-destructive flex items-center gap-1 font-medium">
                                                        <AlertCircle className="size-3" />
                                                        {t(folderValidation.errorKey)}
                                                    </span>
                                                )}
                                            </div>
                                            <div className="flex gap-2">
                                                <Input
                                                    id="sync-target-folder"
                                                    value={profile.target_folder || ""}
                                                    onChange={(e) => updateProfile({ target_folder: e.target.value })}
                                                    disabled={syncing}
                                                    className={cn(
                                                        "h-9 text-xs font-mono",
                                                        !folderValidation.valid && "border-destructive focus-visible:ring-destructive",
                                                    )}
                                                    placeholder={isAdb ? "/sdcard/Music" : "D:\\Music"}
                                                />
                                                {activeTarget.kind !== "adb" && (
                                                    <Button
                                                        type="button"
                                                        variant="outline"
                                                        size="sm"
                                                        disabled={syncing}
                                                        onClick={() => void handleBrowseTargetFolder()}
                                                        className="h-9 px-3 text-xs shrink-0"
                                                    >
                                                        {t("translation.sync.browseFolder")}
                                                    </Button>
                                                )}
                                            </div>
                                            <p className="text-[11px] text-muted-foreground">
                                                {isAdb
                                                    ? t("translation.sync.targetFolderHintAdb")
                                                    : t("translation.sync.targetFolderHintDrive")}
                                            </p>
                                        </div>
                                    </div>

                                    {/* Selection Rules: Whole Library, Artists, Albums, Playlists (.m3u8), and Recent Window */}
                                    <SelectionRulesEditor
                                        rules={profile.selection_rules || syncengine.Selection.createFrom({
                                            whole_library: true,
                                            artists: [],
                                            albums: [],
                                            playlists: [],
                                            recent_days: 0,
                                        })}
                                        onChange={(rules) => updateProfile({ selection_rules: rules })}
                                        disabled={syncing}
                                    />

                                    <div className="flex items-center justify-between pt-1">
                                        <div className="text-xs text-muted-foreground">
                                            {isPlanStale && (
                                                <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400 font-medium">
                                                    <AlertCircle className="size-3.5" />
                                                    {t("translation.sync.previewStaleNotice")}
                                                </span>
                                            )}
                                        </div>
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            disabled={syncing || !isConfigValid}
                                            onClick={() => void handleSaveProfile()}
                                        >
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
                                            disabled={planning || syncing || !isConfigValid}
                                            onClick={() => void handlePreviewSync()}
                                        >
                                            <RefreshCw className={cn("mr-1.5 size-3.5", planning && "animate-spin")} />
                                            {planning ? t("translation.sync.previewing") : t("translation.sync.preview")}
                                        </Button>
                                        <Button
                                            variant={isPlanStale ? "outline" : "default"}
                                            size="sm"
                                            disabled={planning || syncing || !activeTarget.connected || !isConfigValid}
                                            onClick={() => void handleStartSync()}
                                        >
                                            <Play className="mr-1.5 size-3.5" />
                                            {t("translation.sync.syncNow")}
                                        </Button>
                                        <Button
                                            variant={hasInterruptedSync ? "default" : "outline"}
                                            size="sm"
                                            disabled={syncing || !activeTarget.connected || !hasInterruptedSync}
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
                                    <div
                                        role="status"
                                        aria-label={t("translation.sync.ariaProgress")}
                                        className="mt-2 flex flex-col gap-2 rounded-md bg-muted/40 p-3 text-xs"
                                    >
                                        <div className="flex items-center justify-between">
                                            <span className="font-medium text-foreground">
                                                {syncProgress.phase === "error"
                                                    ? t("translation.sync.statusError", { error: syncProgress.error || "" })
                                                    : t(PHASE_LABELS[syncProgress.phase] ?? "translation.sync.statusTransferring")}
                                            </span>
                                            {syncProgress.op_total ? (
                                                <span className="font-mono text-[10px] text-muted-foreground">
                                                    {syncProgress.done || 0} / {syncProgress.op_total}
                                                </span>
                                            ) : null}
                                        </div>

                                        {syncProgress.op_total ? (
                                            <Progress
                                                value={Math.round(((syncProgress.done || 0) / syncProgress.op_total) * 100)}
                                                className="h-1.5 w-full"
                                                aria-label={t("translation.sync.ariaProgress")}
                                            />
                                        ) : null}

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
                        <div className="flex items-center gap-2">
                            <DialogTitle>{t("translation.sync.planTitle")}</DialogTitle>
                            {isPlanStale && (
                                <span className="rounded bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold text-amber-600 dark:text-amber-400">
                                    {t("translation.sync.previewStaleBadge")}
                                </span>
                            )}
                        </div>
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
                                    {t("translation.sync.planFree", {
                                        free: formatBytes(plan.free_available || 0),
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
