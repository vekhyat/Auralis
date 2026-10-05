import assert from "node:assert/strict";
import { test } from "node:test";
import {
    cleanPosixPath,
    isAbsolutePath,
    isVolumeRoot,
    validateRecentDays,
    validateTargetFolder,
} from "./validation.ts";

test("cleanPosixPath resolves dots, double slashes, and relative traversals", () => {
    assert.equal(cleanPosixPath("/sdcard/Music/../Audio"), "/sdcard/Audio");
    assert.equal(cleanPosixPath("/storage/emulated/0//Music/"), "/storage/emulated/0/Music");
    assert.equal(cleanPosixPath("/"), "/");
    assert.equal(cleanPosixPath(""), "");
    assert.equal(cleanPosixPath("Music/Rock"), "Music/Rock");
});

test("isVolumeRoot identifies drive letters, Unix root, and UNC roots", () => {
    // Windows volume roots
    assert.equal(isVolumeRoot("C:\\"), true);
    assert.equal(isVolumeRoot("C:/"), true);
    assert.equal(isVolumeRoot("c:"), true);
    assert.equal(isVolumeRoot("E:\\"), true);

    // Non-roots
    assert.equal(isVolumeRoot("C:\\Music"), false);
    assert.equal(isVolumeRoot("D:/Audio/Sync"), false);
    assert.equal(isVolumeRoot("/sdcard/Music"), false);

    // POSIX root
    assert.equal(isVolumeRoot("/"), true);
    assert.equal(isVolumeRoot("///"), true);

    // UNC root vs subfolder
    assert.equal(isVolumeRoot("\\\\nas\\music"), true);
    assert.equal(isVolumeRoot("\\\\nas\\music\\sync"), false);
});

test("isAbsolutePath recognizes Windows drive, UNC, and Unix absolute paths", () => {
    assert.equal(isAbsolutePath("C:\\Music"), true);
    assert.equal(isAbsolutePath("d:/music"), true);
    assert.equal(isAbsolutePath("\\\\nas\\share"), true);
    assert.equal(isAbsolutePath("/sdcard/Music"), true);

    // Relative paths
    assert.equal(isAbsolutePath("relative/path"), false);
    assert.equal(isAbsolutePath("./relative"), false);
    assert.equal(isAbsolutePath(""), false);
});

test("validateTargetFolder enforces managed root containment for ADB devices", () => {
    // Allowed dedicated subfolders
    assert.deepEqual(validateTargetFolder("/sdcard/Music", true), { valid: true });
    assert.deepEqual(validateTargetFolder("/storage/emulated/0/Music", true), { valid: true });
    assert.deepEqual(validateTargetFolder("/sdcard/Audio/Playlists", true), { valid: true });

    // Disallowed dangerous root paths
    assert.equal(validateTargetFolder("/", true).valid, false);
    assert.equal(validateTargetFolder("/sdcard", true).valid, false);
    assert.equal(validateTargetFolder("/sdcard/", true).valid, false);
    assert.equal(validateTargetFolder("/storage", true).valid, false);
    assert.equal(validateTargetFolder("/storage/", true).valid, false);
    assert.equal(validateTargetFolder("Music", true).valid, false); // Not absolute

    // Invalid control characters
    assert.equal(validateTargetFolder("/sdcard/Music\n", true).valid, false);
    assert.equal(validateTargetFolder("/sdcard/Music\x00", true).valid, false);
});

test("validateTargetFolder enforces managed root containment for drives and folder targets", () => {
    // Allowed subfolders
    assert.deepEqual(validateTargetFolder("C:\\Music", false), { valid: true });
    assert.deepEqual(validateTargetFolder("E:\\Audio\\Sync", false), { valid: true });
    assert.deepEqual(validateTargetFolder("/Users/alice/Music", false), { valid: true });

    // Disallowed volume root (preventing accidental wipe of entire drive)
    assert.equal(validateTargetFolder("C:\\", false).valid, false);
    assert.equal(validateTargetFolder("C:", false).valid, false);
    assert.equal(validateTargetFolder("E:/", false).valid, false);
    assert.equal(validateTargetFolder("/", false).valid, false);

    // Disallowed relative paths
    assert.equal(validateTargetFolder("relative/folder", false).valid, false);
    assert.equal(validateTargetFolder("", false).valid, false);
});

test("validateRecentDays checks range between 0 and 36500", () => {
    assert.deepEqual(validateRecentDays(0), { valid: true });
    assert.deepEqual(validateRecentDays(30), { valid: true });
    assert.deepEqual(validateRecentDays(36500), { valid: true });

    assert.equal(validateRecentDays(-1).valid, false);
    assert.equal(validateRecentDays(36501).valid, false);
    assert.equal(validateRecentDays(NaN).valid, false);
});
