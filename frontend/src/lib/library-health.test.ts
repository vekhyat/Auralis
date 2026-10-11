import assert from "node:assert/strict";
import { test } from "node:test";
import {
    ALBUMS_PER_PAGE,
    FALLBACK_RULE_ID,
    ISSUES_PER_PAGE,
    PROFILE_LABEL_KEYS,
    albumStateKey,
    buildProfileOptions,
    classifyApplyResult,
    classifyLibraryError,
    classifyUndoResult,
    formatBatchTime,
    groupIssuesByAlbum,
    groupIssuesByRule,
    isAbsolutePath,
    joinRootPath,
    nextPageLimit,
    pageWindow,
    parentPath,
    progressPercent,
    resolveIssueFolder,
    resolveProfileLabel,
    severityRank,
    shouldRescanAfterApply,
    summarizeBatchTypes,
    worstSeverityRank,
    type HealthIssue,
} from "./library-health.ts";

function issue(overrides: Partial<HealthIssue> & { id: string }): HealthIssue {
    return {
        rule_id: "COVER",
        severity: "warning",
        path: "",
        album_dir: "",
        message: "",
        fixable: false,
        ...overrides,
    };
}

test("severity ranks put errors before warnings before everything else", () => {
    assert.equal(severityRank("error"), 0);
    assert.equal(severityRank("warning"), 1);
    assert.equal(severityRank("info"), 2);
    assert.equal(severityRank(""), 2);
    assert.equal(worstSeverityRank([]), 2);
    assert.equal(worstSeverityRank([{ severity: "info" }, { severity: "warning" }]), 1);
    assert.equal(worstSeverityRank([{ severity: "warning" }, { severity: "error" }, { severity: "info" }]), 0);
});

test("rules are grouped by ID, worst severity first, then larger groups, then ID", () => {
    const issues: HealthIssue[] = [
        issue({ id: "1", rule_id: "ORPHAN", severity: "info" }),
        issue({ id: "2", rule_id: "ORPHAN", severity: "info" }),
        issue({ id: "3", rule_id: "ORPHAN", severity: "info" }),
        issue({ id: "4", rule_id: "COVER", severity: "warning" }),
        issue({ id: "5", rule_id: "TRACKNUMBER", severity: "warning" }),
        issue({ id: "6", rule_id: "TRACKNUMBER", severity: "warning" }),
        issue({ id: "7", rule_id: "PATH", severity: "error" }),
        issue({ id: "8", rule_id: "ALBUMARTIST", severity: "warning" }),
        issue({ id: "9", rule_id: "COVER", severity: "info" }),
        issue({ id: "10", rule_id: "TRACKNUMBER", severity: "info" }),
        issue({ id: "11", rule_id: "COMPILATION", severity: "warning" }),
    ];
    const groups = groupIssuesByRule(issues);
    assert.deepEqual(
        groups.map((group) => group.ruleId),
        // The error group first; then warning groups, the 3-issue one ahead of
        // the 2-issue one, and the two 1-issue ones in ID order; the info-only
        // group last even though it is as large as the biggest warning group.
        ["PATH", "TRACKNUMBER", "COVER", "ALBUMARTIST", "COMPILATION", "ORPHAN"]
    );
    assert.deepEqual(
        groups.find((group) => group.ruleId === "COVER")?.issues.map((i) => i.id),
        ["4", "9"]
    );
    assert.equal(groups.find((group) => group.ruleId === "ORPHAN")?.worst, 2);
});

test("a group's worst severity beats its size", () => {
    const issues: HealthIssue[] = [
        ...Array.from({ length: 5 }, (_, n) => issue({ id: `w${n}`, rule_id: "COVER", severity: "warning" })),
        issue({ id: "e", rule_id: "UNREADABLE", severity: "error" }),
    ];
    assert.deepEqual(
        groupIssuesByRule(issues).map((group) => group.ruleId),
        ["UNREADABLE", "COVER"]
    );
});

test("issues without a rule ID land in the fallback group and empty input gives no groups", () => {
    const groups = groupIssuesByRule([issue({ id: "1", rule_id: "" })]);
    assert.equal(groups.length, 1);
    assert.equal(groups[0].ruleId, FALLBACK_RULE_ID);
    assert.deepEqual(groupIssuesByRule(null), []);
    assert.deepEqual(groupIssuesByRule(undefined), []);
    assert.deepEqual(groupIssuesByRule([]), []);
});

test("albums are grouped in order of first appearance and the root has an empty key", () => {
    const issues = [
        issue({ id: "1", album_dir: "B/Two" }),
        issue({ id: "2", album_dir: "" }),
        issue({ id: "3", album_dir: "A/One" }),
        issue({ id: "4", album_dir: "B/Two" }),
    ];
    const groups = groupIssuesByAlbum(issues);
    assert.deepEqual(
        groups.map((group) => [group.albumDir, group.issues.map((i) => i.id)]),
        [
            ["B/Two", ["1", "4"]],
            ["", ["2"]],
            ["A/One", ["3"]],
        ]
    );
});

test("album state keys differ per rule and per folder", () => {
    assert.notEqual(albumStateKey("COVER", "A"), albumStateKey("PATH", "A"));
    assert.notEqual(albumStateKey("COVER", ""), albumStateKey("COVER", "A"));
    assert.notEqual(albumStateKey("A:B", "C"), albumStateKey("A", "B:C"));
});

test("page windows show one page first and reveal the rest a page at a time", () => {
    assert.deepEqual(pageWindow(0, undefined, ISSUES_PER_PAGE), { shown: 0, hidden: 0, step: 0 });
    assert.deepEqual(pageWindow(30, undefined, ISSUES_PER_PAGE), { shown: 30, hidden: 0, step: 0 });
    assert.deepEqual(pageWindow(100, undefined, ISSUES_PER_PAGE), { shown: 100, hidden: 0, step: 0 });
    assert.deepEqual(pageWindow(250, undefined, ISSUES_PER_PAGE), { shown: 100, hidden: 150, step: 100 });

    let limit = nextPageLimit(undefined, ISSUES_PER_PAGE);
    assert.equal(limit, 200);
    assert.deepEqual(pageWindow(250, limit, ISSUES_PER_PAGE), { shown: 200, hidden: 50, step: 50 });
    limit = nextPageLimit(limit, ISSUES_PER_PAGE);
    assert.deepEqual(pageWindow(250, limit, ISSUES_PER_PAGE), { shown: 250, hidden: 0, step: 0 });
});

test("page windows ignore a limit below one page and a nonsense total", () => {
    assert.deepEqual(pageWindow(120, 5, ALBUMS_PER_PAGE), { shown: 50, hidden: 70, step: 50 });
    assert.deepEqual(pageWindow(120, Number.NaN, ALBUMS_PER_PAGE), { shown: 50, hidden: 70, step: 50 });
    assert.deepEqual(pageWindow(Number.NaN, undefined, ALBUMS_PER_PAGE), { shown: 0, hidden: 0, step: 0 });
    assert.equal(nextPageLimit(3, ALBUMS_PER_PAGE), 100);
});

test("absolute paths are recognised for Windows drives, UNC shares and POSIX roots", () => {
    assert.equal(isAbsolutePath("C:\\Music\\a.flac"), true);
    assert.equal(isAbsolutePath("d:/Music/a.flac"), true);
    assert.equal(isAbsolutePath("\\\\nas\\music\\a.flac"), true);
    assert.equal(isAbsolutePath("/home/me/Music/a.flac"), true);
    assert.equal(isAbsolutePath("Artist/Album"), false);
    assert.equal(isAbsolutePath(""), false);
});

test("relative paths join onto a Windows or POSIX root with the root's separator", () => {
    assert.equal(joinRootPath("C:\\Music", "Artist/Album"), "C:\\Music\\Artist\\Album");
    assert.equal(joinRootPath("C:\\Music\\", "Artist/Album"), "C:\\Music\\Artist\\Album");
    assert.equal(joinRootPath("C:\\", "Artist"), "C:\\Artist");
    assert.equal(joinRootPath("\\\\nas\\music", "Artist/Album"), "\\\\nas\\music\\Artist\\Album");
    assert.equal(joinRootPath("/home/me/Music", "Artist/Album"), "/home/me/Music/Artist/Album");
    assert.equal(joinRootPath("/home/me/Music/", "Artist"), "/home/me/Music/Artist");
    assert.equal(joinRootPath("/", "Artist"), "/Artist");
    assert.equal(joinRootPath("C:\\Music", ""), "C:\\Music");
    assert.equal(joinRootPath("", "Artist"), null);
    assert.equal(joinRootPath("C:\\Music", "../Elsewhere"), null);
});

test("parent folders are found for Windows, UNC and POSIX file paths", () => {
    assert.equal(parentPath("C:\\Music\\Artist\\a.flac"), "C:\\Music\\Artist");
    assert.equal(parentPath("C:\\a.flac"), "C:\\");
    assert.equal(parentPath("\\\\nas\\music\\Artist\\a.flac"), "\\\\nas\\music\\Artist");
    assert.equal(parentPath("/home/me/a.flac"), "/home/me");
    assert.equal(parentPath("/a.flac"), "/");
    assert.equal(parentPath("a.flac"), null);
});

test("an album-level issue opens its own folder under the root", () => {
    const mixed = { path: "Artist/Album", album_dir: "Artist/Album" };
    assert.equal(resolveIssueFolder(mixed, "C:\\Music"), "C:\\Music\\Artist\\Album");
    assert.equal(resolveIssueFolder(mixed, "/home/me/Music"), "/home/me/Music/Artist/Album");
    assert.equal(resolveIssueFolder({ path: "", album_dir: "" }, "C:\\Music"), "C:\\Music");
    assert.equal(resolveIssueFolder(mixed, ""), null);
});

test("a track-level issue opens the folder that holds its file", () => {
    const cover = { path: "C:\\Music\\Artist\\Album\\01.flac", album_dir: "Artist/Album" };
    assert.equal(resolveIssueFolder(cover, "C:\\Music"), "C:\\Music\\Artist\\Album");
    const posix = { path: "/home/me/Music/Artist/Album/01.flac", album_dir: "Artist/Album" };
    assert.equal(resolveIssueFolder(posix, "/home/me/Music"), "/home/me/Music/Artist/Album");
    const rootLevel = { path: "C:\\Music\\01.flac", album_dir: "" };
    assert.equal(resolveIssueFolder(rootLevel, "C:\\Music"), "C:\\Music");
    // Without a root the file's own parent still works.
    assert.equal(resolveIssueFolder(cover, ""), "C:\\Music\\Artist\\Album");
});

test("an absolute path that is the album folder itself is not trimmed to its parent", () => {
    const empty = { path: "C:\\Music\\Artist\\Empty", album_dir: "Artist/Empty" };
    assert.equal(resolveIssueFolder(empty, "C:\\Music"), "C:\\Music\\Artist\\Empty");
    const sameCase = { path: "c:\\music\\Artist\\Empty", album_dir: "Artist/Empty" };
    assert.equal(resolveIssueFolder(sameCase, "C:\\Music"), "c:\\music\\Artist\\Empty");
});

test("a relative file path falls back to its parent folder", () => {
    assert.equal(resolveIssueFolder({ path: "Artist/Album/01.flac", album_dir: "Artist/Album" }, "C:\\Music"), "C:\\Music\\Artist\\Album");
    assert.equal(resolveIssueFolder({ path: "01.flac", album_dir: "" }, "/music"), "/music");
});

test("a path that climbs out of the root is refused", () => {
    assert.equal(resolveIssueFolder({ path: "../Other", album_dir: "../Other" }, "C:\\Music"), null);
});

test("a stale preview is told apart from other backend errors", () => {
    const stale = classifyLibraryError("this preview is out of date; preview the fixes again");
    assert.equal(stale.kind, "stale-preview");
    assert.equal(stale.message, "this preview is out of date; preview the fixes again");
    assert.equal(classifyLibraryError(new Error("This preview is out of date; preview the fixes again")).kind, "stale-preview");
    assert.equal(classifyLibraryError("fixes are being applied or undone; wait for them to finish").kind, "other");
    assert.equal(classifyLibraryError("scan the library again before previewing or applying fixes").kind, "other");
    assert.equal(classifyLibraryError(undefined).kind, "other");
    assert.equal(classifyLibraryError(new Error("disk full")).message, "disk full");
});

test("apply results are classified and a cancelled-before-anything batch skips the rescan", () => {
    assert.equal(classifyApplyResult({ applied: 4, errors: [] }), "success");
    assert.equal(classifyApplyResult({ applied: 4 }), "success");
    assert.equal(classifyApplyResult({ applied: 4, errors: ["a: file changed"] }), "with-errors");
    assert.equal(classifyApplyResult({ applied: 2, errors: [], cancelled: true }), "cancelled");
    assert.equal(classifyApplyResult({ applied: 0, errors: [], cancelled: true }), "cancelled-empty");

    assert.equal(shouldRescanAfterApply({ applied: 0, cancelled: true }), false);
    assert.equal(shouldRescanAfterApply({ applied: 3, cancelled: true }), true);
    assert.equal(shouldRescanAfterApply({ applied: 0, errors: ["x"] }), true);
    assert.equal(shouldRescanAfterApply({ applied: 5 }), true);
});

test("undo results report restored, partial and nothing, and carry the abandoned batch", () => {
    assert.deepEqual(classifyUndoResult({ applied: 3, errors: [] }), {
        kind: "restored",
        applied: 3,
        errorCount: 0,
        abandonedBatchId: "",
    });
    assert.deepEqual(classifyUndoResult({ applied: 2, errors: ["a: locked", "b: locked"] }), {
        kind: "partial",
        applied: 2,
        errorCount: 2,
        abandonedBatchId: "",
    });
    assert.equal(classifyUndoResult({ applied: 0, errors: ["a: locked"] }).kind, "partial");
    assert.equal(classifyUndoResult({ applied: 0, errors: [] }).kind, "nothing");
    assert.equal(classifyUndoResult(null).kind, "nothing");
    assert.equal(classifyUndoResult(undefined).kind, "nothing");
    const skipped = classifyUndoResult({ applied: 5, errors: [], abandoned_batch_id: "batch-9" });
    assert.equal(skipped.kind, "restored");
    assert.equal(skipped.abandonedBatchId, "batch-9");
    assert.equal(classifyUndoResult({ applied: 0, abandoned_batch_id: "batch-9" }).abandonedBatchId, "batch-9");
});

test("batch types list known operations first and drop empty counts", () => {
    assert.deepEqual(summarizeBatchTypes({ rmdir: 1, tags: 12, move: 3 }), [
        { type: "tags", count: 12 },
        { type: "move", count: 3 },
        { type: "rmdir", count: 1 },
    ]);
    assert.deepEqual(summarizeBatchTypes({ tags: 0, move: 2, zebra: 1, alpha: 4 }), [
        { type: "move", count: 2 },
        { type: "alpha", count: 4 },
        { type: "zebra", count: 1 },
    ]);
    assert.deepEqual(summarizeBatchTypes(null), []);
    assert.deepEqual(summarizeBatchTypes({}), []);
});

test("batch times are formatted, and anything unparseable is shown as sent", () => {
    const formatted = formatBatchTime("2026-06-15T12:00:00Z", "en-US");
    assert.ok(formatted.includes("2026"), formatted);
    assert.notEqual(formatted, "2026-06-15T12:00:00Z");
    assert.equal(formatBatchTime("not a time"), "not a time");
    assert.equal(formatBatchTime(""), "");
});

test("progress percent is clamped and safe for an unknown total", () => {
    assert.equal(progressPercent(0, 10), 0);
    assert.equal(progressPercent(3, 10), 30);
    assert.equal(progressPercent(10, 10), 100);
    assert.equal(progressPercent(15, 10), 100);
    assert.equal(progressPercent(-2, 10), 0);
    assert.equal(progressPercent(5, 0), 0);
    assert.equal(progressPercent(Number.NaN, 10), 0);
});

test("profile names come from the locale by ID, then the backend label, then the ID", () => {
    const messages: Record<string, string> = {
        [PROFILE_LABEL_KEYS.poweramp]: "Poweramp (Android)",
        [PROFILE_LABEL_KEYS.rockbox]: "Rockbox",
    };
    const translate = (key: string) => messages[key] ?? key;
    assert.equal(resolveProfileLabel("poweramp", "Poweramp", translate), "Poweramp (Android)");
    // A key the locale does not have comes back as the key itself.
    assert.equal(resolveProfileLabel("mediastore", "Android (Samsung Music, Files)", translate), "Android (Samsung Music, Files)");
    assert.equal(resolveProfileLabel("custom", "My player", translate), "My player");
    assert.equal(resolveProfileLabel("custom", "  ", translate), "custom");
    assert.equal(resolveProfileLabel("custom", undefined, translate), "custom");
    assert.equal(resolveProfileLabel("toString", undefined, translate), "toString");
});

test("every built-in profile has a locale key", () => {
    assert.deepEqual(Object.keys(PROFILE_LABEL_KEYS).sort(), ["mediaserver", "mediastore", "poweramp", "rockbox"]);
});

test("profile options use the backend list once loaded and the known IDs before that", () => {
    const translate = (key: string) => `t:${key}`;
    const before = buildProfileOptions([], "poweramp", translate);
    assert.deepEqual(
        before.map((option) => option.id),
        ["poweramp", "mediastore", "rockbox", "mediaserver"]
    );
    assert.equal(before[0].label, `t:${PROFILE_LABEL_KEYS.poweramp}`);

    const after = buildProfileOptions(
        [
            { id: "rockbox", label: "Rockbox / iPod" },
            { id: "future", label: "Future player" },
        ],
        "rockbox",
        translate
    );
    assert.deepEqual(after, [
        { id: "rockbox", label: `t:${PROFILE_LABEL_KEYS.rockbox}` },
        { id: "future", label: "Future player" },
    ]);

    const kept = buildProfileOptions([{ id: "rockbox", label: "Rockbox / iPod" }], "old-profile", translate);
    assert.deepEqual(kept.at(-1), { id: "old-profile", label: "old-profile" });
});
