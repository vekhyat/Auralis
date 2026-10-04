import { useCallback, useEffect, useState } from "react";
import { t } from "@/i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { getSettings } from "@/lib/settings";
import { cn } from "@/lib/utils";
import {
    ApplyLibraryPlan,
    CancelLibraryScan,
    GetLibraryReport,
    LibraryProfiles,
    PreviewLibraryFixes,
    SelectFolder,
    StartLibraryScan,
    UndoLastLibraryFix,
} from "../../wailsjs/go/main/App";
import { library, main } from "../../wailsjs/go/models";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import {
    Activity,
    Check,
    CheckCircle2,
    ChevronDown,
    ChevronRight,
    Folder,
    FolderOpen,
    Info,
    Play,
    RefreshCw,
    Square,
    Undo2,
    Wrench,
} from "lucide-react";

interface ScanProgressPayload {
    phase: string;
    current: number;
    total: number;
    path: string;
}

export function LibraryHealthPage() {
    const [rootFolder, setRootFolder] = useState<string>(() => {
        try {
            const settings = getSettings();
            return settings?.downloadPath || "";
        } catch {
            return "";
        }
    });
    const [profiles, setProfiles] = useState<library.Profile[]>([]);
    const [selectedProfile, setSelectedProfile] = useState<string>("poweramp");
    const [isScanning, setIsScanning] = useState<boolean>(false);
    const [isCancelling, setIsCancelling] = useState<boolean>(false);
    const [scanProgress, setScanProgress] = useState<ScanProgressPayload | null>(null);
    const [report, setReport] = useState<main.LibraryReport | null>(null);
    const [expandedRules, setExpandedRules] = useState<Record<string, boolean>>({});
    const [expandedAlbums, setExpandedAlbums] = useState<Record<string, boolean>>({});
    const [selectedIssueIds, setSelectedIssueIds] = useState<Record<string, boolean>>({});
    const [previewPlan, setPreviewPlan] = useState<library.Plan | null>(null);
    const [isPreviewOpen, setIsPreviewOpen] = useState<boolean>(false);
    const [isApplying, setIsApplying] = useState<boolean>(false);
    const [isUndoing, setIsUndoing] = useState<boolean>(false);

    // Load initial settings and profiles
    useEffect(() => {
        let mounted = true;

        LibraryProfiles()
            .then((profs) => {
                if (mounted && profs && profs.length > 0) {
                    setProfiles(profs);
                }
            })
            .catch(() => {});

        GetLibraryReport()
            .then((rep) => {
                if (mounted && rep && rep.root) {
                    setReport(rep);
                    if (rep.root) setRootFolder(rep.root);
                    if (rep.profile_id) setSelectedProfile(rep.profile_id);
                }
            })
            .catch(() => {});

        return () => {
            mounted = false;
        };
    }, []);

    // Listen for scan events
    useEffect(() => {
        const onProgress = (data: ScanProgressPayload) => {
            setScanProgress(data);
            setIsScanning(true);
        };

        const onComplete = (data: main.LibraryReport) => {
            setReport(data);
            setIsScanning(false);
            setIsCancelling(false);
            setScanProgress(null);
            setSelectedIssueIds({});
        };

        const onError = (errMsg: string) => {
            setIsScanning(false);
            setIsCancelling(false);
            setScanProgress(null);
            toast.error(t("translation.libraryHealth.scanErrorToast", { error: errMsg }));
        };

        const onCancelled = () => {
            setIsScanning(false);
            setIsCancelling(false);
            setScanProgress(null);
            toast.info(t("translation.libraryHealth.scanCancelledToast"));
        };

        EventsOn("library:scan-progress", onProgress);
        EventsOn("library:scan-complete", onComplete);
        EventsOn("library:scan-error", onError);
        EventsOn("library:scan-cancelled", onCancelled);

        return () => {
            EventsOff("library:scan-progress");
            EventsOff("library:scan-complete");
            EventsOff("library:scan-error");
            EventsOff("library:scan-cancelled");
        };
    }, []);

    const handlePickFolder = async () => {
        try {
            const folder = await SelectFolder(rootFolder);
            if (folder && folder.trim() !== "") {
                setRootFolder(folder);
            }
        } catch (err) {
            console.error("Failed to select folder", err);
        }
    };

    const handleStartScan = async () => {
        if (!rootFolder || rootFolder.trim() === "") {
            toast.error(t("translation.libraryHealth.noFolderSelected"));
            return;
        }
        setIsScanning(true);
        setIsCancelling(false);
        setScanProgress({ phase: "starting", current: 0, total: 1, path: rootFolder });
        try {
            await StartLibraryScan(rootFolder, selectedProfile);
        } catch (err: unknown) {
            setIsScanning(false);
            const msg = err instanceof Error ? err.message : String(err);
            toast.error(t("translation.libraryHealth.scanErrorToast", { error: msg }));
        }
    };

    const handleCancelScan = async () => {
        setIsCancelling(true);
        try {
            await CancelLibraryScan();
        } catch (err) {
            console.error("Failed to cancel scan", err);
        }
    };

    const toggleRule = (ruleId: string) => {
        setExpandedRules((prev) => ({ ...prev, [ruleId]: !prev[ruleId] }));
    };

    const toggleAlbum = (key: string) => {
        setExpandedAlbums((prev) => ({ ...prev, [key]: !prev[key] }));
    };

    const handleSelectIssue = (id: string, checked: boolean) => {
        setSelectedIssueIds((prev) => ({ ...prev, [id]: checked }));
    };

    const handleSelectRuleIssues = (ruleIssues: library.Issue[], selectAll: boolean) => {
        setSelectedIssueIds((prev) => {
            const next = { ...prev };
            for (const issue of ruleIssues) {
                if (issue.fixable) {
                    next[issue.id] = selectAll;
                }
            }
            return next;
        });
    };

    const handlePreviewFixes = useCallback(
        async (specificIds?: string[]) => {
            try {
                let idsToPreview: string[] = [];
                if (specificIds) {
                    idsToPreview = specificIds;
                } else {
                    idsToPreview = Object.keys(selectedIssueIds).filter((id) => selectedIssueIds[id]);
                }
                const plan = await PreviewLibraryFixes(idsToPreview);
                setPreviewPlan(plan);
                setIsPreviewOpen(true);
            } catch (err) {
                const msg = err instanceof Error ? err.message : String(err);
                toast.error(msg);
            }
        },
        [selectedIssueIds]
    );

    const handlePreviewAllSafe = useCallback(async () => {
        try {
            const plan = await PreviewLibraryFixes([]);
            setPreviewPlan(plan);
            setIsPreviewOpen(true);
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            toast.error(msg);
        }
    }, []);

    const handleApplyPlan = async () => {
        if (!previewPlan) return;
        setIsApplying(true);
        try {
            const result = await ApplyLibraryPlan(previewPlan);
            setIsPreviewOpen(false);
            setPreviewPlan(null);
            if (result.errors && result.errors.length > 0) {
                toast.warning(
                    t("translation.libraryHealth.fixAppliedToastErrors", {
                        applied: result.applied,
                        errors: result.errors.length,
                    })
                );
            } else {
                toast.success(
                    t("translation.libraryHealth.fixAppliedToast", {
                        applied: result.applied,
                    })
                );
            }
            // Trigger background scan to refresh status
            await handleStartScan();
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            toast.error(msg);
        } finally {
            setIsApplying(false);
        }
    };

    const handleUndoLastFix = async () => {
        setIsUndoing(true);
        try {
            const result = await UndoLastLibraryFix();
            if (!result || result.applied === 0) {
                toast.info(t("translation.libraryHealth.undoNoBatch"));
            } else {
                toast.success(
                    t("translation.libraryHealth.undoAppliedToast", {
                        applied: result.applied,
                    })
                );
                // Trigger background scan to refresh status
                await handleStartScan();
            }
        } catch (err) {
            const msg = err instanceof Error ? err.message : String(err);
            toast.error(msg);
        } finally {
            setIsUndoing(false);
        }
    };

    // Group issues by rule
    const groupedIssues: Record<string, library.Issue[]> = {};
    if (report && report.issues) {
        for (const issue of report.issues) {
            const rId = issue.rule_id || "other";
            if (!groupedIssues[rId]) {
                groupedIssues[rId] = [];
            }
            groupedIssues[rId].push(issue);
        }
    }

    const totalIssuesCount = report?.issues ? report.issues.length : 0;
    const fixableIssuesCount = report?.issues ? report.issues.filter((i) => i.fixable).length : 0;
    const selectedCount = Object.values(selectedIssueIds).filter(Boolean).length;
    const healthScore = report ? Math.round(report.health_score) : 100;

    const getScoreColor = (score: number) => {
        if (score >= 90) return "text-emerald-500 border-emerald-500/30 bg-emerald-500/10";
        if (score >= 70) return "text-amber-500 border-amber-500/30 bg-amber-500/10";
        return "text-destructive border-destructive/30 bg-destructive/10";
    };

    const getRuleTitle = (ruleId: string): string => {
        switch (ruleId) {
            case "album_artist":
                return t("translation.libraryHealth.ruleAlbumArtist");
            case "compilation":
                return t("translation.libraryHealth.ruleCompilation");
            case "track_number":
                return t("translation.libraryHealth.ruleTrackNumber");
            case "disc_number":
                return t("translation.libraryHealth.ruleDiscNumber");
            case "cover":
                return t("translation.libraryHealth.ruleCover");
            case "path":
                return t("translation.libraryHealth.rulePath");
            case "mixed_format":
                return t("translation.libraryHealth.ruleMixedFormat");
            case "duplicate":
                return t("translation.libraryHealth.ruleDuplicate");
            case "orphan":
                return t("translation.libraryHealth.ruleOrphan");
            default:
                return ruleId;
        }
    };

    return (
        <div className="flex flex-col gap-6 max-w-5xl mx-auto pb-16">
            {/* Header & Description */}
            <div className="flex flex-col gap-1.5">
                <div className="flex items-center gap-2.5">
                    <Activity className="size-6 text-primary" />
                    <h1 className="text-2xl font-semibold tracking-tight">
                        {t("translation.libraryHealth.title")}
                    </h1>
                </div>
                <p className="text-sm text-muted-foreground">
                    {t("translation.libraryHealth.subtitle")}
                </p>
            </div>

            {/* Controls Card */}
            <Card className="border-border bg-card">
                <CardContent className="pt-6 flex flex-col gap-4">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        {/* Folder Picker */}
                        <div className="flex flex-col gap-2">
                            <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                {t("translation.libraryHealth.rootFolder")}
                            </label>
                            <div className="flex gap-2">
                                <div className="flex-1 truncate rounded-md border border-input bg-background px-3 py-2 font-mono text-xs text-foreground flex items-center">
                                    <Folder className="size-4 mr-2 text-muted-foreground shrink-0" />
                                    <span className="truncate">{rootFolder || t("translation.libraryHealth.noFolderSelected")}</span>
                                </div>
                                <Button
                                    type="button"
                                    variant="outline"
                                    onClick={handlePickFolder}
                                    disabled={isScanning}
                                    className="gap-2 shrink-0"
                                >
                                    <FolderOpen className="size-4" />
                                    <span>{t("translation.libraryHealth.selectFolder")}</span>
                                </Button>
                            </div>
                        </div>

                        {/* Profile Picker */}
                        <div className="flex flex-col gap-2">
                            <label className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                                {t("translation.libraryHealth.profile")}
                            </label>
                            <Select
                                value={selectedProfile}
                                onValueChange={setSelectedProfile}
                                disabled={isScanning}
                            >
                                <SelectTrigger className="w-full">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {profiles.length > 0 ? (
                                        profiles.map((p) => (
                                            <SelectItem key={p.id} value={p.id}>
                                                {p.label || p.id}
                                            </SelectItem>
                                        ))
                                    ) : (
                                        <>
                                            <SelectItem value="poweramp">
                                                {t("translation.libraryHealth.profilePoweramp")}
                                            </SelectItem>
                                            <SelectItem value="rockbox">
                                                {t("translation.libraryHealth.profileRockbox")}
                                            </SelectItem>
                                            <SelectItem value="apple">
                                                {t("translation.libraryHealth.profileApple")}
                                            </SelectItem>
                                            <SelectItem value="mediaserver">
                                                {t("translation.libraryHealth.profileMediaserver")}
                                            </SelectItem>
                                            <SelectItem value="vanilla">
                                                {t("translation.libraryHealth.profileVanilla")}
                                            </SelectItem>
                                        </>
                                    )}
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    {/* Scan actions & progress */}
                    <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 pt-2 border-t border-border">
                        <div className="flex items-center gap-2">
                            {isScanning ? (
                                <Button
                                    type="button"
                                    variant="destructive"
                                    onClick={handleCancelScan}
                                    disabled={isCancelling}
                                    className="gap-2"
                                >
                                    <Square className="size-4 fill-current" />
                                    <span>
                                        {isCancelling
                                            ? t("translation.libraryHealth.cancelling")
                                            : t("translation.libraryHealth.cancelScan")}
                                    </span>
                                </Button>
                            ) : (
                                <Button
                                    type="button"
                                    onClick={handleStartScan}
                                    className="gap-2 bg-primary hover:bg-primary/90 text-primary-foreground font-medium"
                                >
                                    <Play className="size-4 fill-current" />
                                    <span>{t("translation.libraryHealth.scan")}</span>
                                </Button>
                            )}

                            <Button
                                type="button"
                                variant="outline"
                                onClick={handleUndoLastFix}
                                disabled={isScanning || isUndoing}
                                className="gap-2"
                            >
                                <Undo2 className="size-4" />
                                <span>
                                    {isUndoing
                                        ? t("translation.libraryHealth.undoing")
                                        : t("translation.libraryHealth.undoLast")}
                                </span>
                            </Button>
                        </div>

                        {report && report.issues && report.issues.some((i) => i.fixable) && !isScanning && (
                            <div className="flex items-center gap-2">
                                {selectedCount > 0 && (
                                    <Button
                                        type="button"
                                        variant="outline"
                                        onClick={() => handlePreviewFixes()}
                                        className="gap-2"
                                    >
                                        <Wrench className="size-4" />
                                        <span>
                                            {t("translation.libraryHealth.fixSelected", {
                                                count: selectedCount,
                                            })}
                                        </span>
                                    </Button>
                                )}
                                <Button
                                    type="button"
                                    onClick={handlePreviewAllSafe}
                                    className="gap-2 bg-emerald-600 hover:bg-emerald-700 text-white font-medium"
                                >
                                    <Check className="size-4" />
                                    <span>{t("translation.libraryHealth.fixAllSafe")}</span>
                                </Button>
                            </div>
                        )}
                    </div>

                    {/* Progress Bar */}
                    {isScanning && scanProgress && (
                        <div className="flex flex-col gap-2 pt-2">
                            <div className="flex items-center justify-between text-xs text-muted-foreground font-mono">
                                <span className="truncate max-w-md">{scanProgress.path || scanProgress.phase}</span>
                                <span>
                                    {scanProgress.total > 0
                                        ? `${Math.round((scanProgress.current / scanProgress.total) * 100)}% (${scanProgress.current}/${scanProgress.total})`
                                        : `${scanProgress.current} files`}
                                </span>
                            </div>
                            <Progress
                                value={
                                    scanProgress.total > 0
                                        ? (scanProgress.current / scanProgress.total) * 100
                                        : 50
                                }
                                className="h-2"
                            />
                        </div>
                    )}
                </CardContent>
            </Card>

            {/* Health Score Overview */}
            {report && (
                <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
                    {/* Score Card */}
                    <Card className="col-span-2 sm:col-span-1 border-border bg-card">
                        <CardContent className="pt-4 pb-4 flex flex-col items-center justify-center text-center">
                            <div
                                className={cn(
                                    "size-14 rounded-full border-2 flex items-center justify-center text-xl font-bold mb-1",
                                    getScoreColor(healthScore)
                                )}
                            >
                                {healthScore}%
                            </div>
                            <span className="text-xs text-muted-foreground font-medium">
                                {t("translation.libraryHealth.healthScore")}
                            </span>
                        </CardContent>
                    </Card>

                    {/* Tracks Count */}
                    <Card className="border-border bg-card">
                        <CardContent className="pt-4 pb-4 flex flex-col items-center justify-center text-center">
                            <span className="text-2xl font-bold font-mono text-foreground mb-1">
                                {report.total_tracks}
                            </span>
                            <span className="text-xs text-muted-foreground font-medium">
                                {t("translation.libraryHealth.totalTracks")}
                            </span>
                        </CardContent>
                    </Card>

                    {/* Albums Count */}
                    <Card className="border-border bg-card">
                        <CardContent className="pt-4 pb-4 flex flex-col items-center justify-center text-center">
                            <span className="text-2xl font-bold font-mono text-foreground mb-1">
                                {report.total_albums}
                            </span>
                            <span className="text-xs text-muted-foreground font-medium">
                                {t("translation.libraryHealth.totalAlbums")}
                            </span>
                        </CardContent>
                    </Card>

                    {/* Total Issues */}
                    <Card className="border-border bg-card">
                        <CardContent className="pt-4 pb-4 flex flex-col items-center justify-center text-center">
                            <span
                                className={cn(
                                    "text-2xl font-bold font-mono mb-1",
                                    totalIssuesCount > 0 ? "text-amber-500" : "text-emerald-500"
                                )}
                            >
                                {totalIssuesCount}
                            </span>
                            <span className="text-xs text-muted-foreground font-medium">
                                {t("translation.libraryHealth.totalIssues")}
                            </span>
                        </CardContent>
                    </Card>

                    {/* Fixable Issues */}
                    <Card className="border-border bg-card">
                        <CardContent className="pt-4 pb-4 flex flex-col items-center justify-center text-center">
                            <span
                                className={cn(
                                    "text-2xl font-bold font-mono mb-1",
                                    fixableIssuesCount > 0 ? "text-primary" : "text-muted-foreground"
                                )}
                            >
                                {fixableIssuesCount}
                            </span>
                            <span className="text-xs text-muted-foreground font-medium">
                                {t("translation.libraryHealth.fixableIssues")}
                            </span>
                        </CardContent>
                    </Card>
                </div>
            )}

            {/* Empty State / Perfect Health */}
            {!report && !isScanning && (
                <Card className="border-border bg-card/50 border-dashed">
                    <CardContent className="py-12 flex flex-col items-center justify-center text-center gap-3 text-muted-foreground">
                        <Activity className="size-10 stroke-1 opacity-50" />
                        <p className="text-sm max-w-sm">
                            {t("translation.libraryHealth.emptyState")}
                        </p>
                    </CardContent>
                </Card>
            )}

            {report && totalIssuesCount === 0 && (
                <Card className="border-emerald-500/20 bg-emerald-500/5">
                    <CardContent className="py-10 flex flex-col items-center justify-center text-center gap-3">
                        <CheckCircle2 className="size-12 text-emerald-500" />
                        <p className="text-base font-medium text-emerald-500">
                            {t("translation.libraryHealth.noIssues")}
                        </p>
                    </CardContent>
                </Card>
            )}

            {/* Issues Grouped By Rule */}
            {report && totalIssuesCount > 0 && (
                <div className="flex flex-col gap-3">
                    {Object.entries(groupedIssues).map(([ruleId, ruleIssues]) => {
                        const isExpanded = !!expandedRules[ruleId];
                        const fixableInRule = ruleIssues.filter((i) => i.fixable);
                        const hasFixable = fixableInRule.length > 0;
                        const allSelected =
                            hasFixable && fixableInRule.every((i) => selectedIssueIds[i.id]);

                        // Group per album within the rule
                        const issuesByAlbum: Record<string, library.Issue[]> = {};
                        for (const iss of ruleIssues) {
                            const alb = iss.album_dir || "General";
                            if (!issuesByAlbum[alb]) issuesByAlbum[alb] = [];
                            issuesByAlbum[alb].push(iss);
                        }

                        return (
                            <Card key={ruleId} className="border-border bg-card overflow-hidden">
                                <div
                                    className="p-4 flex items-center justify-between gap-3 cursor-pointer hover:bg-muted/40 transition-colors"
                                    onClick={() => toggleRule(ruleId)}
                                >
                                    <div className="flex items-center gap-3 min-w-0">
                                        <button
                                            type="button"
                                            className="text-muted-foreground hover:text-foreground"
                                            onClick={(e) => {
                                                e.stopPropagation();
                                                toggleRule(ruleId);
                                            }}
                                        >
                                            {isExpanded ? (
                                                <ChevronDown className="size-4" />
                                            ) : (
                                                <ChevronRight className="size-4" />
                                            )}
                                        </button>

                                        {hasFixable && (
                                            <div
                                                onClick={(e) => e.stopPropagation()}
                                                className="flex items-center"
                                            >
                                                <Checkbox
                                                    checked={allSelected}
                                                    onCheckedChange={(checked) =>
                                                        handleSelectRuleIssues(ruleIssues, !!checked)
                                                    }
                                                />
                                            </div>
                                        )}

                                        <div className="flex items-center gap-2 truncate">
                                            <span className="font-semibold text-sm truncate">
                                                {getRuleTitle(ruleId)}
                                            </span>
                                            <Badge variant="secondary" className="font-mono text-xs">
                                                {ruleIssues.length}
                                            </Badge>
                                        </div>
                                    </div>

                                    <div className="flex items-center gap-2 shrink-0">
                                        {hasFixable && (
                                            <Badge
                                                variant="outline"
                                                className="border-primary/30 text-primary text-[11px] font-mono"
                                            >
                                                {fixableInRule.length} {t("translation.libraryHealth.fixableIssues")}
                                            </Badge>
                                        )}
                                    </div>
                                </div>

                                {isExpanded && (
                                    <CardContent className="pt-0 pb-4 px-4 border-t border-border/50 bg-background/50 flex flex-col gap-3">
                                        {Object.entries(issuesByAlbum).map(([albumName, albIssues]) => {
                                            const albumKey = `${ruleId}:${albumName}`;
                                            const isAlbumExpanded =
                                                expandedAlbums[albumKey] !== false; // default open

                                            return (
                                                <div
                                                    key={albumKey}
                                                    className="mt-3 rounded-md border border-border bg-card overflow-hidden"
                                                >
                                                    <div
                                                        className="px-3 py-2 bg-muted/30 flex items-center justify-between text-xs font-mono cursor-pointer"
                                                        onClick={() => toggleAlbum(albumKey)}
                                                    >
                                                        <div className="flex items-center gap-2 truncate">
                                                            {isAlbumExpanded ? (
                                                                <ChevronDown className="size-3.5 text-muted-foreground" />
                                                            ) : (
                                                                <ChevronRight className="size-3.5 text-muted-foreground" />
                                                            )}
                                                            <Folder className="size-3.5 text-primary shrink-0" />
                                                            <span className="font-semibold truncate">
                                                                {albumName}
                                                            </span>
                                                        </div>
                                                        <span className="text-muted-foreground shrink-0">
                                                            {albIssues.length} issues
                                                        </span>
                                                    </div>

                                                    {isAlbumExpanded && (
                                                        <div className="divide-y divide-border/50">
                                                            {albIssues.map((issue) => (
                                                                <div
                                                                    key={issue.id}
                                                                    className="px-3 py-2.5 flex items-start justify-between gap-3 text-xs"
                                                                >
                                                                    <div className="flex items-start gap-2.5 min-w-0">
                                                                        {issue.fixable ? (
                                                                            <Checkbox
                                                                                checked={
                                                                                    !!selectedIssueIds[issue.id]
                                                                                }
                                                                                onCheckedChange={(checked) =>
                                                                                    handleSelectIssue(
                                                                                        issue.id,
                                                                                        !!checked
                                                                                    )
                                                                                }
                                                                                className="mt-0.5"
                                                                            />
                                                                        ) : (
                                                                            <div className="size-4 shrink-0 mt-0.5 flex items-center justify-center text-muted-foreground">
                                                                                <Info className="size-3.5" />
                                                                            </div>
                                                                        )}
                                                                        <div className="flex flex-col gap-1 min-w-0">
                                                                            <span className="text-foreground leading-snug">
                                                                                {issue.message}
                                                                            </span>
                                                                            {issue.path && (
                                                                                <span className="font-mono text-[11px] text-muted-foreground truncate">
                                                                                    {issue.path}
                                                                                </span>
                                                                            )}
                                                                        </div>
                                                                    </div>

                                                                    <div className="flex items-center gap-2 shrink-0">
                                                                        {issue.severity === "error" && (
                                                                            <Badge
                                                                                variant="destructive"
                                                                                className="text-[10px] uppercase font-mono"
                                                                            >
                                                                                {t("translation.libraryHealth.severityError")}
                                                                            </Badge>
                                                                        )}
                                                                        {issue.severity === "warning" && (
                                                                            <Badge
                                                                                variant="secondary"
                                                                                className="text-[10px] uppercase font-mono text-amber-500 border border-amber-500/30"
                                                                            >
                                                                                {t("translation.libraryHealth.severityWarning")}
                                                                            </Badge>
                                                                        )}
                                                                        {issue.fixable && (
                                                                            <Button
                                                                                type="button"
                                                                                size="sm"
                                                                                variant="ghost"
                                                                                onClick={() =>
                                                                                    handlePreviewFixes([issue.id])
                                                                                }
                                                                                className="h-6 px-2 text-[11px] text-primary hover:text-primary"
                                                                            >
                                                                                {t("translation.libraryHealth.previewFixes")}
                                                                            </Button>
                                                                        )}
                                                                    </div>
                                                                </div>
                                                            ))}
                                                        </div>
                                                    )}
                                                </div>
                                            );
                                        })}
                                    </CardContent>
                                )}
                            </Card>
                        );
                    })}
                </div>
            )}

            {/* Preview Plan Dialog */}
            <Dialog open={isPreviewOpen} onOpenChange={setIsPreviewOpen}>
                <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <Wrench className="size-5 text-primary" />
                            <span>{t("translation.libraryHealth.previewTitle")}</span>
                        </DialogTitle>
                        <DialogDescription>
                            {t("translation.libraryHealth.previewDescription")}
                        </DialogDescription>
                    </DialogHeader>

                    {previewPlan && (
                        <div className="flex flex-col gap-3 py-2 flex-1 min-h-0 overflow-hidden">
                            <div className="flex items-center justify-between text-xs font-mono text-muted-foreground px-1">
                                <span>
                                    {t("translation.libraryHealth.previewOperationCount", {
                                        count: previewPlan.operations ? previewPlan.operations.length : 0,
                                    })}
                                </span>
                            </div>

                            <ScrollArea className="flex-1 border border-border rounded-md p-3 bg-muted/20">
                                <div className="flex flex-col gap-2.5">
                                    {previewPlan.operations && previewPlan.operations.length > 0 ? (
                                        previewPlan.operations.map((op, idx) => (
                                            <div
                                                key={idx}
                                                className="p-2.5 rounded border border-border bg-card text-xs flex flex-col gap-1.5"
                                            >
                                                <div className="flex items-center justify-between font-mono">
                                                    <Badge variant="outline" className="text-[10px] uppercase font-mono">
                                                        {op.type === "set_tags" && t("translation.libraryHealth.diffTags")}
                                                        {op.type === "rename" && t("translation.libraryHealth.diffMove")}
                                                        {op.type === "rmdir" && t("translation.libraryHealth.diffRmdir")}
                                                    </Badge>
                                                    <span className="text-[11px] text-muted-foreground truncate max-w-xs">
                                                        {op.path}
                                                    </span>
                                                </div>

                                                {op.type === "set_tags" && op.set && (
                                                    <div className="font-mono text-[11px] bg-background/60 p-2 rounded border border-border/50 divide-y divide-border/30">
                                                        {Object.entries(op.set).map(([tag, val]) => (
                                                            <div key={tag} className="py-1 flex items-center justify-between">
                                                                <span className="font-semibold text-muted-foreground">{tag}:</span>
                                                                <span className="text-emerald-500 truncate max-w-sm">"{String(val)}"</span>
                                                            </div>
                                                        ))}
                                                    </div>
                                                )}

                                                {op.type === "rename" && (
                                                    <div className="font-mono text-[11px] bg-background/60 p-2 rounded border border-border/50 flex flex-col gap-1">
                                                        <div className="text-destructive truncate">
                                                            - {op.path}
                                                        </div>
                                                        <div className="text-emerald-500 truncate">
                                                            + {op.new_path}
                                                        </div>
                                                    </div>
                                                )}

                                                {op.type === "rmdir" && (
                                                    <div className="font-mono text-[11px] bg-background/60 p-2 rounded border border-border/50 text-destructive truncate">
                                                        rmdir {op.path}
                                                    </div>
                                                )}
                                            </div>
                                        ))
                                    ) : (
                                        <div className="py-6 text-center text-xs text-muted-foreground font-mono">
                                            No operations to apply.
                                        </div>
                                    )}
                                </div>
                            </ScrollArea>
                        </div>
                    )}

                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button
                            type="button"
                            variant="outline"
                            onClick={() => setIsPreviewOpen(false)}
                            disabled={isApplying}
                        >
                            {t("translation.libraryHealth.close")}
                        </Button>
                        <Button
                            type="button"
                            onClick={handleApplyPlan}
                            disabled={
                                isApplying ||
                                !previewPlan ||
                                !previewPlan.operations ||
                                previewPlan.operations.length === 0
                            }
                            className="bg-primary text-primary-foreground gap-2"
                        >
                            {isApplying ? (
                                <>
                                    <RefreshCw className="size-4 animate-spin" />
                                    <span>{t("translation.libraryHealth.applyingFixes")}</span>
                                </>
                            ) : (
                                <>
                                    <Check className="size-4" />
                                    <span>{t("translation.libraryHealth.applyFixes")}</span>
                                </>
                            )}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
