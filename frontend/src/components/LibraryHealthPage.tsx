import { memo, useCallback, useEffect, useMemo, useState } from "react";
import i18n, { t } from "@/i18n";
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
import {
    ALBUMS_PER_PAGE,
    ISSUES_PER_PAGE,
    OPERATIONS_PER_PAGE,
    albumStateKey,
    buildProfileOptions,
    classifyApplyResult,
    classifyLibraryError,
    classifyUndoResult,
    errorMessage,
    formatBatchTime,
    groupIssuesByAlbum,
    groupIssuesByRule,
    nextPageLimit,
    pageWindow,
    progressPercent,
    resolveIssueFolder,
    shouldRescanAfterApply,
    summarizeBatchTypes,
} from "@/lib/library-health";
import { cn } from "@/lib/utils";
import {
    ApplyLibraryPlan,
    CancelLibraryApply,
    CancelLibraryScan,
    GetLastLibraryBatch,
    GetLibraryReport,
    LibraryProfiles,
    OpenFolder,
    PreviewLibraryFixes,
    SelectFolder,
    StartLibraryScan,
    UndoLastLibraryFix,
} from "../../wailsjs/go/main/App";
import { library, main } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime/runtime";
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
    TriangleAlert,
    Undo2,
    Wrench,
} from "lucide-react";

interface ScanProgressPayload {
    phase: string;
    current: number;
    total: number;
    path: string;
}

interface ApplyProgressPayload {
    current: number;
    total: number;
    path: string;
}

// A warning with a batch ID in it is worth reading; keep it up longer.
const LONG_TOAST_MS = 10000;

const getScoreColor = (score: number) => {
    if (score >= 90) return "text-emerald-500 border-emerald-500/30 bg-emerald-500/10";
    if (score >= 70) return "text-amber-500 border-amber-500/30 bg-amber-500/10";
    return "text-destructive border-destructive/30 bg-destructive/10";
};

const getRuleTitle = (ruleId: string): string => {
    // The backend emits upper-case IDs (ALBUMARTIST, MIXEDFORMAT, ...).
    switch (ruleId.toLowerCase().replace(/_/g, "")) {
        case "albumartist":
            return t("translation.libraryHealth.ruleAlbumArtist");
        case "compilation":
            return t("translation.libraryHealth.ruleCompilation");
        case "tracknumber":
            return t("translation.libraryHealth.ruleTrackNumber");
        case "discnumber":
            return t("translation.libraryHealth.ruleDiscNumber");
        case "cover":
            return t("translation.libraryHealth.ruleCover");
        case "path":
            return t("translation.libraryHealth.rulePath");
        case "mixedformat":
            return t("translation.libraryHealth.ruleMixedFormat");
        case "duplicate":
        case "duplicates":
            return t("translation.libraryHealth.ruleDuplicate");
        case "orphan":
            return t("translation.libraryHealth.ruleOrphan");
        case "unreadable":
            return t("translation.libraryHealth.ruleUnreadable");
        default:
            return ruleId;
    }
};

const getOperationTypeLabel = (type: string): string => {
    switch (type) {
        case "tags":
            return t("translation.libraryHealth.diffTags");
        case "move":
            return t("translation.libraryHealth.diffMove");
        case "rmdir":
            return t("translation.libraryHealth.diffRmdir");
        default:
            return type;
    }
};

interface IssueRowProps {
    issue: library.Issue;
    checked: boolean;
    canFix: boolean;
    root: string;
    onSelect: (id: string, checked: boolean) => void;
    onPreview: (id: string) => void;
    onOpenFolder: (folder: string) => void;
}

const IssueRow = memo(function IssueRow({
    issue,
    checked,
    canFix,
    root,
    onSelect,
    onPreview,
    onOpenFolder,
}: IssueRowProps) {
    // A row with a preview button has its own way forward. Every other row can
    // only be dealt with by hand, fixable ones in a report restored from an
    // earlier session included, so give it a way to reach the folder.
    const hasFixAction = issue.fixable && canFix;
    const folder = hasFixAction ? null : resolveIssueFolder(issue, root);

    return (
        <div className="px-3 py-2.5 flex items-start justify-between gap-3 text-xs">
            <div className="flex items-start gap-2.5 min-w-0">
                {issue.fixable && canFix ? (
                    <Checkbox
                        checked={checked}
                        onCheckedChange={(value) => onSelect(issue.id, !!value)}
                        className="mt-0.5"
                    />
                ) : (
                    <div className="size-4 shrink-0 mt-0.5 flex items-center justify-center text-muted-foreground">
                        <Info className="size-3.5" />
                    </div>
                )}
                <div className="flex flex-col gap-1 min-w-0">
                    <span className="text-foreground leading-snug">{issue.message}</span>
                    {issue.path && (
                        <span className="font-mono text-[11px] text-muted-foreground truncate">
                            {issue.path}
                        </span>
                    )}
                </div>
            </div>

            <div className="flex items-center gap-2 shrink-0">
                {issue.severity === "error" && (
                    <Badge variant="destructive" className="text-[10px] uppercase font-mono">
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
                {issue.fixable && canFix && (
                    <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => onPreview(issue.id)}
                        className="h-6 px-2 text-[11px] text-primary hover:text-primary"
                    >
                        {t("translation.libraryHealth.previewFixes")}
                    </Button>
                )}
                {folder && (
                    <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => onOpenFolder(folder)}
                        className="h-6 gap-1 px-2 text-[11px]"
                    >
                        <FolderOpen className="size-3" />
                        <span>{t("translation.common.openFolder")}</span>
                    </Button>
                )}
            </div>
        </div>
    );
});

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
    // How many rows the user has asked to see, per album key / per rule ID.
    // Missing means the first page.
    const [issueLimits, setIssueLimits] = useState<Record<string, number>>({});
    const [albumLimits, setAlbumLimits] = useState<Record<string, number>>({});
    const [previewOpLimit, setPreviewOpLimit] = useState<number | undefined>(undefined);
    const [selectedIssueIds, setSelectedIssueIds] = useState<Record<string, boolean>>({});
    const [previewPlan, setPreviewPlan] = useState<library.Plan | null>(null);
    const [isPreviewOpen, setIsPreviewOpen] = useState<boolean>(false);
    const [isApplying, setIsApplying] = useState<boolean>(false);
    const [isCancellingApply, setIsCancellingApply] = useState<boolean>(false);
    const [applyProgress, setApplyProgress] = useState<ApplyProgressPayload | null>(null);
    const [undoBatch, setUndoBatch] = useState<library.BatchInfo | null>(null);
    const [isUndoConfirmOpen, setIsUndoConfirmOpen] = useState<boolean>(false);
    const [isLoadingBatch, setIsLoadingBatch] = useState<boolean>(false);
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

    // Listen for scan and apply events
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
            setIssueLimits({});
            setAlbumLimits({});
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

        const onApplyProgress = (data: ApplyProgressPayload) => {
            setApplyProgress(data);
        };

        const unsubscribers = [
            EventsOn("library:scan-progress", onProgress),
            EventsOn("library:scan-complete", onComplete),
            EventsOn("library:scan-error", onError),
            EventsOn("library:scan-cancelled", onCancelled),
            EventsOn("library:apply-progress", onApplyProgress),
        ];

        return () => {
            unsubscribers.forEach((off) => off());
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

    const handleStartScan = async (root: string = rootFolder, profileId: string = selectedProfile) => {
        if (!root || root.trim() === "") {
            toast.error(t("translation.libraryHealth.noFolderSelected"));
            return;
        }
        setIsScanning(true);
        setIsCancelling(false);
        setScanProgress({ phase: "starting", current: 0, total: 1, path: root });
        try {
            await StartLibraryScan(root, profileId);
        } catch (err: unknown) {
            setIsScanning(false);
            toast.error(t("translation.libraryHealth.scanErrorToast", { error: errorMessage(err) }));
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

    const showMoreIssues = (albumKey: string) => {
        setIssueLimits((prev) => ({ ...prev, [albumKey]: nextPageLimit(prev[albumKey], ISSUES_PER_PAGE) }));
    };

    const showMoreAlbums = (ruleId: string) => {
        setAlbumLimits((prev) => ({ ...prev, [ruleId]: nextPageLimit(prev[ruleId], ALBUMS_PER_PAGE) }));
    };

    const handleSelectIssue = useCallback((id: string, checked: boolean) => {
        setSelectedIssueIds((prev) => ({ ...prev, [id]: checked }));
    }, []);

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

    // Failures of a preview or an undo. A stale preview gets a message in the
    // user's language; the backend's own text is English only.
    const reportFailure = useCallback((err: unknown) => {
        const failure = classifyLibraryError(err);
        if (failure.kind === "stale-preview") {
            toast.warning(t("translation.libraryHealth.previewStaleToast"));
        } else {
            toast.error(failure.message);
        }
    }, []);

    const openPreview = useCallback(
        async (ids: string[]) => {
            try {
                const plan = await PreviewLibraryFixes(ids);
                setPreviewOpLimit(undefined);
                setPreviewPlan(plan);
                setIsPreviewOpen(true);
            } catch (err) {
                reportFailure(err);
            }
        },
        [reportFailure]
    );

    const handlePreviewIssue = useCallback(
        (id: string) => {
            void openPreview([id]);
        },
        [openPreview]
    );

    const handlePreviewSelected = async () => {
        const ids = Object.keys(selectedIssueIds).filter((id) => selectedIssueIds[id]);
        // The backend treats an empty list as an empty plan.
        if (ids.length === 0) return;
        await openPreview(ids);
    };

    // "Safe" means tag edits and empty-folder removal. Path fixes move files
    // around the library, so they are only planned when picked explicitly.
    const handlePreviewAllSafe = async () => {
        const safeIds = (report?.issues ?? [])
            .filter((issue) => issue.fixable && issue.rule_id !== "PATH")
            .map((issue) => issue.id);
        if (safeIds.length === 0) {
            toast.info(t("translation.libraryHealth.noSafeFixes"));
            return;
        }
        await openPreview(safeIds);
    };

    const handleOpenFolder = useCallback(async (folder: string) => {
        try {
            await OpenFolder(folder);
        } catch (err) {
            toast.error(t("translation.libraryHealth.openFolderFailed", { error: errorMessage(err) }));
        }
    }, []);

    const handleApplyPlan = async () => {
        if (!previewPlan || isApplying) return;
        setIsApplying(true);
        setIsCancellingApply(false);
        setApplyProgress(null);
        try {
            const result = await ApplyLibraryPlan(previewPlan);
            setIsPreviewOpen(false);
            setPreviewPlan(null);
            switch (classifyApplyResult(result)) {
                case "cancelled":
                    toast.warning(
                        t("translation.libraryHealth.applyCancelledToast", {
                            applied: result.applied,
                        }),
                        { duration: LONG_TOAST_MS }
                    );
                    break;
                case "cancelled-empty":
                    toast.info(t("translation.libraryHealth.applyCancelledNothingToast"));
                    break;
                case "with-errors":
                    toast.warning(
                        t("translation.libraryHealth.fixAppliedToastErrors", {
                            applied: result.applied,
                            errors: result.errors.length,
                        }),
                        { description: result.errors[0] }
                    );
                    break;
                default:
                    toast.success(
                        t("translation.libraryHealth.fixAppliedToast", {
                            applied: result.applied,
                        })
                    );
            }
            if (shouldRescanAfterApply(result)) {
                // Refresh the report that was just fixed, not a folder picked since.
                await handleStartScan(report?.root || rootFolder, report?.profile_id || selectedProfile);
            }
        } catch (err) {
            const failure = classifyLibraryError(err);
            if (failure.kind === "stale-preview") {
                // The plan the dialog holds is gone. Nothing else changed, so
                // only ask for a new preview.
                setIsPreviewOpen(false);
                setPreviewPlan(null);
                toast.warning(t("translation.libraryHealth.previewStaleToast"));
            } else {
                toast.error(failure.message);
            }
        } finally {
            setIsApplying(false);
            setIsCancellingApply(false);
            setApplyProgress(null);
        }
    };

    const handleCancelApply = async () => {
        setIsCancellingApply(true);
        try {
            await CancelLibraryApply();
        } catch (err) {
            console.error("Failed to cancel apply", err);
            setIsCancellingApply(false);
        }
    };

    // First step of an undo: describe what it would revert and ask.
    const handleUndoLastFix = async () => {
        if (isLoadingBatch || isUndoing) return;
        setIsLoadingBatch(true);
        try {
            const batch = await GetLastLibraryBatch();
            if (!batch) {
                toast.info(t("translation.libraryHealth.undoNoBatch"));
                return;
            }
            setUndoBatch(batch);
            setIsUndoConfirmOpen(true);
        } catch (err) {
            reportFailure(err);
        } finally {
            setIsLoadingBatch(false);
        }
    };

    const handleConfirmUndo = async () => {
        if (isUndoing) return;
        setIsUndoing(true);
        try {
            const result = await UndoLastLibraryFix();
            setIsUndoConfirmOpen(false);
            setUndoBatch(null);
            const outcome = classifyUndoResult(result);
            if (outcome.kind === "partial") {
                toast.warning(
                    t("translation.libraryHealth.undoPartialToast", {
                        applied: outcome.applied,
                        errors: outcome.errorCount,
                    }),
                    { description: result?.errors?.[0], duration: LONG_TOAST_MS }
                );
            } else if (outcome.kind === "restored") {
                toast.success(
                    t("translation.libraryHealth.undoAppliedToast", {
                        applied: outcome.applied,
                    })
                );
            } else if (!outcome.abandonedBatchId) {
                toast.info(t("translation.libraryHealth.undoNoBatch"));
            }
            if (outcome.abandonedBatchId) {
                toast.warning(t("translation.libraryHealth.undoAbandonedToast"), {
                    description: outcome.abandonedBatchId,
                    duration: LONG_TOAST_MS,
                });
            }
            if (outcome.applied > 0) {
                await handleStartScan(report?.root || rootFolder, report?.profile_id || selectedProfile);
            }
        } catch (err) {
            reportFailure(err);
        } finally {
            setIsUndoing(false);
        }
    };

    // Group issues by rule: worst severity first, then the biggest group.
    const ruleGroups = useMemo(() => groupIssuesByRule<library.Issue>(report?.issues), [report]);

    // Operations the backend already marked as unsafe are skipped on apply.
    const runnableOps = previewPlan?.operations?.filter((op) => !op.error).length ?? 0;
    const previewOps = previewPlan?.operations ?? [];
    const previewWindow = pageWindow(previewOps.length, previewOpLimit, OPERATIONS_PER_PAGE);

    const totalIssuesCount = report?.issues ? report.issues.length : 0;
    const fixableIssuesCount = report?.issues ? report.issues.filter((i) => i.fixable).length : 0;
    const selectedCount = Object.values(selectedIssueIds).filter(Boolean).length;
    const healthScore = report ? Math.round(report.health_score) : 100;
    // A report restored from a previous session has no live scan behind it.
    const canFix = !!report && !report.persisted;
    const reportRoot = report?.root || rootFolder;

    // Folders the scan could not read are missing from every number above.
    const skippedDirs = report?.skipped_dirs ?? 0;
    const skippedSample = report?.skipped_sample ?? [];
    const skippedMore = Math.max(0, skippedDirs - skippedSample.length);

    const profileOptions = buildProfileOptions(profiles, selectedProfile, (key) => t(key));
    const undoTypes = summarizeBatchTypes(undoBatch?.types);

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
                                    {profileOptions.map((option) => (
                                        <SelectItem key={option.id} value={option.id}>
                                            {option.label}
                                        </SelectItem>
                                    ))}
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
                                    onClick={() => handleStartScan()}
                                    disabled={isApplying || isUndoing}
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
                                disabled={isScanning || isApplying || isLoadingBatch || isUndoing}
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

                        {report && report.issues && report.issues.some((i) => i.fixable) && canFix && !isScanning && (
                            <div className="flex items-center gap-2">
                                {selectedCount > 0 && (
                                    <Button
                                        type="button"
                                        variant="outline"
                                        onClick={handlePreviewSelected}
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

            {/* Folders the scan could not read */}
            {report && skippedDirs > 0 && (
                <Card className="border-amber-500/30 bg-amber-500/5" role="alert">
                    <CardContent className="py-4 flex items-start gap-3">
                        <TriangleAlert className="size-5 text-amber-500 shrink-0 mt-0.5" />
                        <div className="flex flex-col gap-1.5 min-w-0">
                            <p className="text-sm font-medium text-amber-500">
                                {t("translation.libraryHealth.skippedFoldersTitle", { count: skippedDirs })}
                            </p>
                            <p className="text-xs text-muted-foreground">
                                {t("translation.libraryHealth.skippedFoldersDescription")}
                            </p>
                            {skippedSample.length > 0 && (
                                <ul className="flex flex-col gap-0.5 font-mono text-[11px] text-muted-foreground">
                                    {skippedSample.map((sample) => (
                                        <li key={sample} className="truncate">
                                            {sample}
                                        </li>
                                    ))}
                                </ul>
                            )}
                            {skippedMore > 0 && (
                                <p className="text-[11px] text-muted-foreground">
                                    {t("translation.libraryHealth.skippedFoldersMore", { count: skippedMore })}
                                </p>
                            )}
                        </div>
                    </CardContent>
                </Card>
            )}

            {report?.persisted && (
                <p className="text-xs text-muted-foreground">
                    {t("translation.libraryHealth.persistedNotice")}
                </p>
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

            {/* Not claimed when part of the library was never read. */}
            {report && totalIssuesCount === 0 && skippedDirs === 0 && (
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
                    {ruleGroups.map(({ ruleId, issues: ruleIssues }) => {
                        const isExpanded = !!expandedRules[ruleId];
                        const fixableInRule = ruleIssues.filter((i) => i.fixable);
                        const hasFixable = fixableInRule.length > 0;
                        const allSelected =
                            hasFixable && fixableInRule.every((i) => selectedIssueIds[i.id]);

                        // Group per album within the rule, only once it is open.
                        const albumGroups = isExpanded ? groupIssuesByAlbum(ruleIssues) : [];
                        const albumWindow = pageWindow(albumGroups.length, albumLimits[ruleId], ALBUMS_PER_PAGE);

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

                                        {hasFixable && canFix && (
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
                                        {albumGroups.slice(0, albumWindow.shown).map(({ albumDir, issues: albIssues }) => {
                                            const albumKey = albumStateKey(ruleId, albumDir);
                                            const isAlbumExpanded =
                                                expandedAlbums[albumKey] !== false; // default open
                                            const issueWindow = pageWindow(
                                                albIssues.length,
                                                issueLimits[albumKey],
                                                ISSUES_PER_PAGE
                                            );

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
                                                                {albumDir || t("translation.libraryHealth.libraryRoot")}
                                                            </span>
                                                        </div>
                                                        <span className="text-muted-foreground shrink-0">
                                                            {t("translation.libraryHealth.albumIssueCount", {
                                                                count: albIssues.length,
                                                            })}
                                                        </span>
                                                    </div>

                                                    {isAlbumExpanded && (
                                                        <div className="divide-y divide-border/50">
                                                            {albIssues.slice(0, issueWindow.shown).map((issue) => (
                                                                <IssueRow
                                                                    key={issue.id}
                                                                    issue={issue}
                                                                    checked={!!selectedIssueIds[issue.id]}
                                                                    canFix={canFix}
                                                                    root={reportRoot}
                                                                    onSelect={handleSelectIssue}
                                                                    onPreview={handlePreviewIssue}
                                                                    onOpenFolder={handleOpenFolder}
                                                                />
                                                            ))}
                                                            {issueWindow.hidden > 0 && (
                                                                <div className="px-3 py-2 flex justify-center">
                                                                    <Button
                                                                        type="button"
                                                                        size="sm"
                                                                        variant="ghost"
                                                                        onClick={() => showMoreIssues(albumKey)}
                                                                        className="h-7 px-3 text-[11px]"
                                                                    >
                                                                        {t("translation.libraryHealth.showMore", {
                                                                            count: issueWindow.step,
                                                                        })}
                                                                    </Button>
                                                                </div>
                                                            )}
                                                        </div>
                                                    )}
                                                </div>
                                            );
                                        })}
                                        {albumWindow.hidden > 0 && (
                                            <div className="flex justify-center pt-1">
                                                <Button
                                                    type="button"
                                                    size="sm"
                                                    variant="outline"
                                                    onClick={() => showMoreAlbums(ruleId)}
                                                    className="h-8 px-4 text-xs"
                                                >
                                                    {t("translation.libraryHealth.showMoreAlbums", {
                                                        count: albumWindow.step,
                                                    })}
                                                </Button>
                                            </div>
                                        )}
                                    </CardContent>
                                )}
                            </Card>
                        );
                    })}
                </div>
            )}

            {/* Preview Plan Dialog */}
            <Dialog
                open={isPreviewOpen}
                onOpenChange={(open) => {
                    // Closing mid-apply would hide a batch that is still changing files.
                    if (!open && isApplying) return;
                    setIsPreviewOpen(open);
                }}
            >
                <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col" showCloseButton={!isApplying}>
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
                                        count: runnableOps,
                                    })}
                                </span>
                            </div>

                            {isApplying && (
                                <div className="flex flex-col gap-2 px-1" role="status" aria-live="polite">
                                    <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground font-mono">
                                        <span className="truncate min-w-0">
                                            {applyProgress?.path || t("translation.libraryHealth.applyingFixes")}
                                        </span>
                                        {applyProgress && (
                                            <span className="shrink-0">
                                                {t("translation.libraryHealth.applyProgress", {
                                                    current: applyProgress.current,
                                                    total: applyProgress.total,
                                                })}
                                            </span>
                                        )}
                                    </div>
                                    <Progress
                                        value={
                                            applyProgress
                                                ? progressPercent(applyProgress.current, applyProgress.total)
                                                : 0
                                        }
                                        className="h-2"
                                    />
                                </div>
                            )}

                            <ScrollArea className="flex-1 border border-border rounded-md p-3 bg-muted/20">
                                <div className="flex flex-col gap-2.5">
                                    {previewOps.length > 0 ? (
                                        previewOps.slice(0, previewWindow.shown).map((op, idx) => (
                                            <div
                                                key={idx}
                                                className="p-2.5 rounded border border-border bg-card text-xs flex flex-col gap-1.5"
                                            >
                                                <div className="flex items-center justify-between font-mono">
                                                    <Badge variant="outline" className="text-[10px] uppercase font-mono">
                                                        {getOperationTypeLabel(op.type)}
                                                    </Badge>
                                                    <span className="text-[11px] text-muted-foreground truncate max-w-xs">
                                                        {op.path}
                                                    </span>
                                                </div>

                                                {op.error && (
                                                    <div className="text-[11px] text-destructive">
                                                        {t("translation.libraryHealth.opSkipped", { error: op.error })}
                                                    </div>
                                                )}

                                                {op.type === "tags" && op.set && (
                                                    <div className="font-mono text-[11px] bg-background/60 p-2 rounded border border-border/50 divide-y divide-border/30">
                                                        {Object.entries(op.set).map(([tag, val]) => (
                                                            <div key={tag} className="py-1 flex items-center justify-between gap-2">
                                                                <span className="font-semibold text-muted-foreground">{tag}:</span>
                                                                <span className="flex items-center gap-1.5 min-w-0">
                                                                    {op.old && tag in op.old && (
                                                                        <span className="text-destructive line-through truncate max-w-[40%]">{op.old[tag] ? String(op.old[tag]) : t("translation.libraryHealth.diffUnset")}</span>
                                                                    )}
                                                                    <span aria-hidden="true" className="text-muted-foreground">→</span>
                                                                    <span className="text-emerald-500 truncate max-w-[40%]">{String(val)}</span>
                                                                </span>
                                                            </div>
                                                        ))}
                                                    </div>
                                                )}

                                                {op.type === "move" && (
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
                                            {t("translation.libraryHealth.previewEmpty")}
                                        </div>
                                    )}
                                    {previewWindow.hidden > 0 && (
                                        <div className="flex justify-center">
                                            <Button
                                                type="button"
                                                size="sm"
                                                variant="ghost"
                                                onClick={() =>
                                                    setPreviewOpLimit((prev) =>
                                                        nextPageLimit(prev, OPERATIONS_PER_PAGE)
                                                    )
                                                }
                                                className="h-7 px-3 text-[11px]"
                                            >
                                                {t("translation.libraryHealth.showMore", {
                                                    count: previewWindow.step,
                                                })}
                                            </Button>
                                        </div>
                                    )}
                                </div>
                            </ScrollArea>
                        </div>
                    )}

                    <DialogFooter className="gap-2 sm:gap-0">
                        {isApplying ? (
                            <Button
                                type="button"
                                variant="outline"
                                onClick={handleCancelApply}
                                disabled={isCancellingApply}
                                className="gap-2"
                            >
                                <Square className="size-4 fill-current" />
                                <span>
                                    {isCancellingApply
                                        ? t("translation.libraryHealth.cancelling")
                                        : t("translation.libraryHealth.cancelApply")}
                                </span>
                            </Button>
                        ) : (
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => setIsPreviewOpen(false)}
                            >
                                {t("translation.libraryHealth.close")}
                            </Button>
                        )}
                        <Button
                            type="button"
                            onClick={handleApplyPlan}
                            disabled={
                                isApplying || runnableOps === 0
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

            {/* Undo Confirmation Dialog */}
            <Dialog
                open={isUndoConfirmOpen}
                onOpenChange={(open) => {
                    if (!open && isUndoing) return;
                    setIsUndoConfirmOpen(open);
                }}
            >
                <DialogContent className="max-w-lg" showCloseButton={!isUndoing}>
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <Undo2 className="size-5 text-primary" />
                            <span>{t("translation.libraryHealth.undoConfirmTitle")}</span>
                        </DialogTitle>
                        <DialogDescription>
                            {t("translation.libraryHealth.undoConfirmDescription")}
                        </DialogDescription>
                    </DialogHeader>

                    {undoBatch && (
                        <div className="flex flex-col gap-3 py-1">
                            {undoBatch.abandoned_batch_id && (
                                <div
                                    role="alert"
                                    className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-500"
                                >
                                    <TriangleAlert className="size-4 shrink-0 mt-0.5" />
                                    <span className="leading-snug">
                                        {t("translation.libraryHealth.undoAbandonedNotice")}
                                    </span>
                                </div>
                            )}

                            <div className="rounded-md border border-border bg-muted/20 p-3 flex flex-col gap-2.5 text-xs">
                                <div className="flex flex-wrap items-center justify-between gap-2">
                                    <span className="text-muted-foreground">
                                        {t("translation.libraryHealth.undoBatchTime", {
                                            time: formatBatchTime(undoBatch.time, i18n.language),
                                        })}
                                    </span>
                                    <span className="font-mono font-medium text-foreground">
                                        {t("translation.libraryHealth.undoBatchCount", {
                                            count: undoBatch.count,
                                        })}
                                    </span>
                                </div>

                                {undoTypes.length > 0 && (
                                    <div className="flex flex-wrap gap-1.5">
                                        {undoTypes.map(({ type, count }) => (
                                            <Badge
                                                key={type}
                                                variant="outline"
                                                className="text-[10px] font-mono"
                                            >
                                                {getOperationTypeLabel(type)}: {count}
                                            </Badge>
                                        ))}
                                    </div>
                                )}

                                {undoBatch.paths && undoBatch.paths.length > 0 && (
                                    <div className="flex flex-col gap-1">
                                        <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                            {t("translation.libraryHealth.undoBatchSamples")}
                                        </span>
                                        <ul className="flex flex-col gap-0.5 font-mono text-[11px] text-muted-foreground">
                                            {undoBatch.paths.map((samplePath) => (
                                                <li key={samplePath} className="truncate" title={samplePath}>
                                                    {samplePath}
                                                </li>
                                            ))}
                                        </ul>
                                    </div>
                                )}
                            </div>
                        </div>
                    )}

                    <DialogFooter className="gap-2 sm:gap-0">
                        <Button
                            type="button"
                            variant="outline"
                            onClick={() => setIsUndoConfirmOpen(false)}
                            disabled={isUndoing}
                        >
                            {t("translation.common.cancel")}
                        </Button>
                        <Button
                            type="button"
                            onClick={handleConfirmUndo}
                            disabled={isUndoing}
                            className="bg-primary text-primary-foreground gap-2"
                        >
                            {isUndoing ? (
                                <>
                                    <RefreshCw className="size-4 animate-spin" />
                                    <span>{t("translation.libraryHealth.undoing")}</span>
                                </>
                            ) : (
                                <>
                                    <Undo2 className="size-4" />
                                    <span>{t("translation.libraryHealth.undoConfirm")}</span>
                                </>
                            )}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
