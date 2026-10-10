export type LosslessQuality = "16" | "24" | "atmos";
export type LossyQuality = "lossy-320" | "lossy-256" | "lossy-192" | "lossy-128";
export type DownloadQuality = LosslessQuality | LossyQuality;
export type LossyFormat = "mp3" | "m4a-aac" | "opus";
export type LossyBitrate = "320k" | "256k" | "192k" | "128k";

// Highest bitrate first; the picker reverses this to list lowest first.
export const LOSSY_QUALITIES: readonly LossyQuality[] = ["lossy-320", "lossy-256", "lossy-192", "lossy-128"];
const DOWNLOAD_QUALITIES: readonly DownloadQuality[] = ["atmos", "24", "16", ...LOSSY_QUALITIES];

export function isDownloadQuality(value: unknown): value is DownloadQuality {
    return typeof value === "string" && (DOWNLOAD_QUALITIES as readonly string[]).includes(value);
}

export function isLossyQuality(value: unknown): value is LossyQuality {
    return typeof value === "string" && (LOSSY_QUALITIES as readonly string[]).includes(value);
}

export function lossyKbps(quality: LossyQuality): number {
    return Number(quality.slice("lossy-".length));
}

// Lossy tiers are not fetched as lossy streams. Sources are asked for the
// smallest lossless file (16-bit) and ffmpeg encodes it, so every verified
// source can serve every tier and the result is checked like any other file.
export function lossyConversionTarget(quality: DownloadQuality): { format: LossyFormat; bitrate: LossyBitrate } | null {
    if (!isLossyQuality(quality)) {
        return null;
    }
    const bitrate = `${lossyKbps(quality)}k` as LossyBitrate;
    // At 320 kbps MP3 is already transparent and plays on every device. Below
    // that AAC keeps noticeably more detail per bit and still plays natively
    // on phones, so the smaller tiers use it.
    return { format: lossyKbps(quality) >= 320 ? "mp3" : "m4a-aac", bitrate };
}
