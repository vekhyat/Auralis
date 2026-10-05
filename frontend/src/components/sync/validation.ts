import type { ValidationResult } from "./types";

/**
 * Normalizes POSIX paths similarly to Go's path.Clean for ADB storage paths.
 */
export function cleanPosixPath(raw: string): string {
    if (!raw) return "";
    const normalized = raw.replace(/\\/g, "/");
    const isAbsolute = normalized.startsWith("/");
    const segments = normalized.split("/").filter(Boolean);
    const resolved: string[] = [];

    for (const seg of segments) {
        if (seg === ".") continue;
        if (seg === "..") {
            if (resolved.length > 0) resolved.pop();
        } else {
            resolved.push(seg);
        }
    }

    const result = (isAbsolute ? "/" : "") + resolved.join("/");
    return result || (isAbsolute ? "/" : ".");
}

/**
 * Checks if a Windows or POSIX path points directly to a volume root
 * (e.g. "C:\", "D:", "/", "\\server\share\").
 */
export function isVolumeRoot(raw: string): boolean {
    const trimmed = raw.trim();
    if (!trimmed) return false;

    // Windows drive root (e.g., "C:\", "c:/", "D:", "e:\")
    if (/^[a-zA-Z]:[\\/]*$/.test(trimmed)) {
        return true;
    }

    // Unix root ("/" or multiple slashes "///")
    if (/^\/+$/.test(trimmed)) {
        return true;
    }

    // UNC root e.g. \\server\share or \\server\share\
    const uncMatch = trimmed.match(/^\\\\[^\\/]+[\\/]+[^\\/]+[\\/]*$/);
    if (uncMatch) {
        return true;
    }

    return false;
}

/**
 * Checks if a path is absolute on Windows or Unix.
 */
export function isAbsolutePath(raw: string): boolean {
    const trimmed = raw.trim();
    if (!trimmed) return false;

    // Windows drive: e.g. C:\ or C:/
    if (/^[a-zA-Z]:[\\/]/.test(trimmed)) {
        return true;
    }

    // UNC path: \\server\share
    if (/^\\\\[^\\/]+[\\/]+[^\\/]+/.test(trimmed)) {
        return true;
    }

    // Unix absolute path
    if (trimmed.startsWith("/")) {
        return true;
    }

    return false;
}

/**
 * Validates target folder per backend/devices/validation.go rules:
 * - No control characters (\x00, \r, \n)
 * - ADB targets: must be absolute (start with /), cleaned path cannot be "/", "/sdcard", or "/storage"
 * - Drive/Folder targets: must be absolute path, and cannot be the volume root itself
 */
export function validateTargetFolder(folder: string, isAdb: boolean): ValidationResult {
    // Check for control characters on raw input before trimming
    if (/[\r\n]/.test(folder || "") || (folder || "").includes("\0")) {
        return {
            valid: false,
            errorKey: "translation.sync.validationInvalidChars",
        };
    }

    const trimmed = (folder || "").trim();
    if (!trimmed) {
        return {
            valid: false,
            errorKey: isAdb
                ? "translation.sync.validationAdbSubfolder"
                : "translation.sync.validationFolderAbsolute",
        };
    }

    if (isAdb) {
        const cleaned = cleanPosixPath(trimmed);
        if (!cleaned.startsWith("/") || cleaned === "/" || cleaned === "/sdcard" || cleaned === "/storage") {
            return {
                valid: false,
                errorKey: "translation.sync.validationAdbSubfolder",
            };
        }
    } else {
        if (!isAbsolutePath(trimmed)) {
            return {
                valid: false,
                errorKey: "translation.sync.validationFolderAbsolute",
            };
        }
        if (isVolumeRoot(trimmed)) {
            return {
                valid: false,
                errorKey: "translation.sync.validationFolderVolumeRoot",
            };
        }
    }

    return { valid: true };
}

/**
 * Validates recent days selection: must be an integer between 0 and 36500.
 */
export function validateRecentDays(days: number): ValidationResult {
    if (Number.isNaN(days) || days < 0 || days > 36500) {
        return {
            valid: false,
            errorKey: "translation.sync.validationRecentDays",
        };
    }
    return { valid: true };
}

export interface SelectionRulesData {
    whole_library: boolean;
    artists: string[];
    albums: string[];
    playlists: string[];
    recent_days: number;
}

/**
 * Normalizes selection rules into a safe representation.
 */
export function normalizeSelection(raw?: Partial<SelectionRulesData> | null): SelectionRulesData {
    return {
        whole_library: raw?.whole_library ?? true,
        artists: (raw?.artists ?? []).map((s) => s.trim()).filter(Boolean),
        albums: (raw?.albums ?? []).map((s) => s.trim()).filter(Boolean),
        playlists: (raw?.playlists ?? []).map((s) => s.trim()).filter(Boolean),
        recent_days: Math.max(0, Math.min(36500, Number(raw?.recent_days || 0))),
    };
}
