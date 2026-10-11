// Pure logic behind the Library health page. It has no React and no Wails
// imports, so the node:test suite can load it directly.

/** The fields of a library.Issue that the page reads. */
export interface HealthIssue {
    id: string;
    rule_id: string;
    severity: string;
    path: string;
    album_dir: string;
    message: string;
    fixable: boolean;
}

// --- Grouping and ordering -------------------------------------------------------

/** Rule ID used for an issue the backend sent without one. */
export const FALLBACK_RULE_ID = "other";

export interface RuleGroup<T extends HealthIssue = HealthIssue> {
    ruleId: string;
    issues: T[];
    /** severityRank of the worst issue in the group; lower is worse. */
    worst: number;
}

export interface AlbumGroup<T extends Pick<HealthIssue, "album_dir"> = HealthIssue> {
    /** Slash-relative album folder; "" is the library root itself. */
    albumDir: string;
    issues: T[];
}

/** Lower sorts first: errors, then warnings, then everything else. */
export function severityRank(severity: string): number {
    if (severity === "error") return 0;
    if (severity === "warning") return 1;
    return 2;
}

export function worstSeverityRank(issues: ReadonlyArray<Pick<HealthIssue, "severity">>): number {
    let worst = 2;
    for (const issue of issues) {
        const rank = severityRank(issue.severity);
        if (rank < worst) worst = rank;
    }
    return worst;
}

/**
 * Groups issues by rule. Groups with the worst severity come first, the larger
 * group wins a tie, and the rule ID makes the order deterministic after that.
 * Issues keep their incoming order inside a group.
 */
export function groupIssuesByRule<T extends HealthIssue>(issues: readonly T[] | null | undefined): RuleGroup<T>[] {
    const byRule = new Map<string, T[]>();
    for (const issue of issues ?? []) {
        const ruleId = issue.rule_id || FALLBACK_RULE_ID;
        const group = byRule.get(ruleId);
        if (group) {
            group.push(issue);
        } else {
            byRule.set(ruleId, [issue]);
        }
    }
    const groups: RuleGroup<T>[] = [];
    for (const [ruleId, list] of byRule) {
        groups.push({ ruleId, issues: list, worst: worstSeverityRank(list) });
    }
    groups.sort((a, b) => {
        if (a.worst !== b.worst) return a.worst - b.worst;
        if (a.issues.length !== b.issues.length) return b.issues.length - a.issues.length;
        return a.ruleId < b.ruleId ? -1 : a.ruleId > b.ruleId ? 1 : 0;
    });
    return groups;
}

/** Groups one rule's issues per album folder, in order of first appearance. */
export function groupIssuesByAlbum<T extends Pick<HealthIssue, "album_dir">>(issues: readonly T[]): AlbumGroup<T>[] {
    const byAlbum = new Map<string, AlbumGroup<T>>();
    for (const issue of issues) {
        const albumDir = issue.album_dir || "";
        const group = byAlbum.get(albumDir);
        if (group) {
            group.issues.push(issue);
        } else {
            byAlbum.set(albumDir, { albumDir, issues: [issue] });
        }
    }
    return [...byAlbum.values()];
}

/** Key for per-album UI state (collapsed, rows shown). */
export function albumStateKey(ruleId: string, albumDir: string): string {
    return `${ruleId}\u0000${albumDir}`;
}

// --- Chunked rendering -----------------------------------------------------------

/** Issues rendered per album before "Show more". */
export const ISSUES_PER_PAGE = 100;
/** Albums rendered per rule before "Show more albums". */
export const ALBUMS_PER_PAGE = 50;
/** Planned operations rendered in the preview dialog before "Show more". */
export const OPERATIONS_PER_PAGE = 100;

export interface PageWindow {
    /** Items to render. */
    shown: number;
    /** Items still hidden. */
    hidden: number;
    /** How many items the next "Show more" reveals. */
    step: number;
}

/**
 * Works out how much of a list to render. limit is how many the user asked for
 * so far (undefined until they ask for more); it never drops below one page.
 */
export function pageWindow(total: number, limit: number | undefined, pageSize: number): PageWindow {
    const size = Math.max(1, Math.floor(pageSize));
    const requested = limit !== undefined && limit >= size ? Math.floor(limit) : size;
    const count = Number.isFinite(total) ? Math.max(0, Math.floor(total)) : 0;
    const shown = Math.min(count, requested);
    const hidden = count - shown;
    return { shown, hidden, step: Math.min(hidden, size) };
}

/** The limit to store after one "Show more" click. */
export function nextPageLimit(limit: number | undefined, pageSize: number): number {
    const size = Math.max(1, Math.floor(pageSize));
    return (limit !== undefined && limit >= size ? Math.floor(limit) : size) + size;
}

// --- Paths -----------------------------------------------------------------------

const DRIVE_PREFIX = /^[A-Za-z]:/;

/** True for "C:\x", "C:/x", "\\server\share" and "/x"; album-level paths are relative. */
export function isAbsolutePath(path: string): boolean {
    return /^[A-Za-z]:[\\/]/.test(path) || path.startsWith("\\\\") || path.startsWith("/");
}

function separatorFor(root: string): "\\" | "/" {
    return root.includes("\\") || DRIVE_PREFIX.test(root) ? "\\" : "/";
}

function relativeSegments(relative: string): string[] | null {
    const segments = relative.split(/[\\/]+/).filter((segment) => segment !== "" && segment !== ".");
    return segments.includes("..") ? null : segments;
}

/**
 * Joins a slash-relative path onto the library root, using the root's own
 * separator. It returns null without a root, or when the path climbs out of it.
 */
export function joinRootPath(root: string, relative: string): string | null {
    if (root.trim() === "") return null;
    const segments = relativeSegments(relative);
    if (segments === null) return null;
    if (segments.length === 0) return root;
    const separator = separatorFor(root);
    return root.replace(/[\\/]+$/, "") + separator + segments.join(separator);
}

/** The folder holding an absolute file path, or null when there is none. */
export function parentPath(path: string): string | null {
    const trimmed = path.replace(/[\\/]+$/, "");
    const cut = Math.max(trimmed.lastIndexOf("\\"), trimmed.lastIndexOf("/"));
    if (cut < 0) return null;
    const parent = trimmed.slice(0, cut);
    if (parent === "") return trimmed.startsWith("/") ? "/" : null;
    if (/^[A-Za-z]:$/.test(parent)) return parent + trimmed[cut];
    return parent;
}

function comparablePath(path: string): string {
    const slashed = path.replace(/\\/g, "/").replace(/\/+/g, "/").replace(/\/$/, "");
    return DRIVE_PREFIX.test(path) || path.includes("\\") ? slashed.toLowerCase() : slashed;
}

/**
 * The folder to open for an issue. Album-level issues carry a slash-relative
 * path that equals album_dir and names the folder itself. Track-level issues
 * carry an absolute file path, so their folder is its parent. The one absolute
 * path that is a folder (an empty one) is recognised by matching album_dir.
 */
export function resolveIssueFolder(issue: Pick<HealthIssue, "path" | "album_dir">, root: string): string | null {
    const path = issue.path ?? "";
    const albumDir = issue.album_dir ?? "";
    const libraryRoot = root ?? "";

    if (isAbsolutePath(path)) {
        const albumPath = joinRootPath(libraryRoot, albumDir);
        if (albumPath !== null && comparablePath(path) === comparablePath(albumPath)) return path;
        return parentPath(path);
    }

    const pathSegments = relativeSegments(path);
    const albumSegments = relativeSegments(albumDir);
    if (pathSegments === null || albumSegments === null) return null;
    const isFolder = pathSegments.join("/") === albumSegments.join("/");
    const folderSegments = isFolder ? pathSegments : pathSegments.slice(0, -1);
    return joinRootPath(libraryRoot, folderSegments.join("/"));
}

// --- Errors and results ----------------------------------------------------------

export function errorMessage(err: unknown): string {
    if (err instanceof Error) return err.message;
    if (typeof err === "string") return err;
    return String(err);
}

export type LibraryErrorKind = "stale-preview" | "other";

const STALE_PREVIEW = /preview is out of date|preview the fixes again/i;

/**
 * Sorts a backend error into what the page should do about it. A stale preview
 * means the plan the dialog holds is gone, so the page asks for a new one.
 */
export function classifyLibraryError(err: unknown): { kind: LibraryErrorKind; message: string } {
    const message = errorMessage(err);
    return { kind: STALE_PREVIEW.test(message) ? "stale-preview" : "other", message };
}

export interface ApplyResultLike {
    applied: number;
    errors?: readonly string[] | null;
    cancelled?: boolean;
}

export type ApplyOutcome = "cancelled" | "cancelled-empty" | "with-errors" | "success";

export function classifyApplyResult(result: ApplyResultLike): ApplyOutcome {
    if (result.cancelled) return result.applied > 0 ? "cancelled" : "cancelled-empty";
    return (result.errors?.length ?? 0) > 0 ? "with-errors" : "success";
}

/** A batch cancelled before its first change left the files as the scan saw them. */
export function shouldRescanAfterApply(result: ApplyResultLike): boolean {
    return !(result.cancelled && result.applied === 0);
}

export interface UndoResultLike {
    applied: number;
    errors?: readonly string[] | null;
    abandoned_batch_id?: string;
}

export interface UndoOutcome {
    kind: "nothing" | "restored" | "partial";
    applied: number;
    errorCount: number;
    /** A newer batch that failed to undo too often and was skipped, if any. */
    abandonedBatchId: string;
}

export function classifyUndoResult(result: UndoResultLike | null | undefined): UndoOutcome {
    const applied = result?.applied ?? 0;
    const errorCount = result?.errors?.length ?? 0;
    const abandonedBatchId = result?.abandoned_batch_id ?? "";
    if (errorCount > 0) return { kind: "partial", applied, errorCount, abandonedBatchId };
    if (applied === 0) return { kind: "nothing", applied, errorCount, abandonedBatchId };
    return { kind: "restored", applied, errorCount, abandonedBatchId };
}

// --- Undo confirmation -----------------------------------------------------------

/** Operation types in the order the confirmation lists them. */
export const BATCH_TYPE_ORDER = ["tags", "move", "rmdir"] as const;

export interface BatchTypeCount {
    type: string;
    count: number;
}

/** Per-type counts with the known types first and empty ones dropped. */
export function summarizeBatchTypes(types: Record<string, number> | null | undefined): BatchTypeCount[] {
    const counts = types ?? {};
    const known: string[] = [...BATCH_TYPE_ORDER];
    const extra = Object.keys(counts)
        .filter((type) => !known.includes(type))
        .sort();
    const summary: BatchTypeCount[] = [];
    for (const type of [...known, ...extra]) {
        const count = counts[type];
        if (typeof count === "number" && Number.isFinite(count) && count > 0) {
            summary.push({ type, count });
        }
    }
    return summary;
}

/** Formats the journal's RFC 3339 stamp for display; unparseable input is returned unchanged. */
export function formatBatchTime(value: string, locale?: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    try {
        return date.toLocaleString(locale, { dateStyle: "medium", timeStyle: "short" });
    } catch {
        return date.toLocaleString();
    }
}

/** 0 to 100, for a progress bar. */
export function progressPercent(current: number, total: number): number {
    if (!(total > 0) || !Number.isFinite(current)) return 0;
    return Math.min(100, Math.max(0, Math.round((current / total) * 100)));
}

// --- Profiles --------------------------------------------------------------------

/** Locale key per backend profile ID. The backend's own labels are English only. */
export const PROFILE_LABEL_KEYS: Readonly<Record<string, string>> = {
    poweramp: "translation.libraryHealth.profilePoweramp",
    mediastore: "translation.libraryHealth.profileMediastore",
    rockbox: "translation.libraryHealth.profileRockbox",
    mediaserver: "translation.libraryHealth.profileMediaserver",
};

/** Profiles listed before the backend answers. */
export const KNOWN_PROFILE_IDS: readonly string[] = Object.keys(PROFILE_LABEL_KEYS);

/** Localised name for a profile, falling back to the backend label, then the ID. */
export function resolveProfileLabel(id: string, backendLabel: string | undefined, translate: (key: string) => string): string {
    const key = Object.prototype.hasOwnProperty.call(PROFILE_LABEL_KEYS, id) ? PROFILE_LABEL_KEYS[id] : undefined;
    if (key) {
        const text = translate(key);
        if (text && text !== key) return text;
    }
    return backendLabel?.trim() || id;
}

export interface ProfileOption {
    id: string;
    label: string;
}

/**
 * Options for the profile picker: the backend's profiles once they arrive, the
 * known ones until then. A selected ID that is in neither list is kept, so the
 * picker never shows an empty value.
 */
export function buildProfileOptions(
    profiles: ReadonlyArray<{ id: string; label?: string }>,
    selectedId: string,
    translate: (key: string) => string
): ProfileOption[] {
    const source =
        profiles.length > 0
            ? profiles.map((profile) => ({ id: profile.id, label: profile.label }))
            : KNOWN_PROFILE_IDS.map((id) => ({ id, label: undefined as string | undefined }));
    const options = source.map(({ id, label }) => ({ id, label: resolveProfileLabel(id, label, translate) }));
    if (selectedId && !options.some((option) => option.id === selectedId)) {
        options.push({ id: selectedId, label: resolveProfileLabel(selectedId, undefined, translate) });
    }
    return options;
}
