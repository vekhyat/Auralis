type SourcePriorityWindow = Window & {
    go?: { main?: { App?: { RankDownloadServices?: (services: string[], quality: string) => Promise<string[]> } } };
};

// Only services with authenticated full-audio proof take part in automatic
// selection. Retired built-in routes (tidal, amazon, community/zarz mirrors,
// saavn.dev) are stripped here so old saved orders can never reintroduce them,
// even when the backend bridge is unavailable or rejects the call.
export const SUPPORTED_AUTO_SERVICES = ["qobuz", "deezer", "apple", "jiosaavn"] as const;

export const DEFAULT_AUTO_ORDER = "qobuz-deezer-apple-jiosaavn";

export const DEFAULT_AUTO_SERVICES: string[] = [...SUPPORTED_AUTO_SERVICES];

export function isSupportedAutoService(service: string): boolean {
    return (SUPPORTED_AUTO_SERVICES as readonly string[]).includes(service);
}

function readAutoServiceList(order: unknown): string[] {
    const list = Array.isArray(order) ? order : typeof order === "string" ? order.split("-") : [];
    const seen = new Set<string>();
    const out: string[] = [];
    for (const entry of list) {
        const normalized = String(entry ?? "").trim().toLowerCase();
        if (normalized !== "" && isSupportedAutoService(normalized) && !seen.has(normalized)) {
            seen.add(normalized);
            out.push(normalized);
        }
    }
    return out;
}

export function sanitizeAutoServices(order: unknown): string[] {
    return readAutoServiceList(order);
}

export function extendAutoServices(order: unknown): string[] {
    const parts = readAutoServiceList(order);
    if (parts.length < 2) return [...DEFAULT_AUTO_SERVICES];
    for (const extra of SUPPORTED_AUTO_SERVICES) {
        if (!parts.includes(extra)) parts.push(extra);
    }
    return parts;
}

function withLossyLast(order: string[], quality: string): string[] {
    if (String(quality ?? "").trim().toLowerCase() === "lossy") return order;
    return [...order.filter((service) => service !== "jiosaavn"), ...order.filter((service) => service === "jiosaavn")];
}

export async function rankDownloadServices(services: string[], quality: string): Promise<string[]> {
    const vetted = sanitizeAutoServices(services);
    const requested = vetted.length > 0 ? vetted : [...DEFAULT_AUTO_SERVICES];
    const fallback = withLossyLast(requested, quality);
    const rank = (window as SourcePriorityWindow).go?.main?.App?.RankDownloadServices;
    if (!rank) return fallback;
    try {
        const ranked = await rank(requested, quality);
        const clean = sanitizeAutoServices(ranked);
        if (clean.length === requested.length && clean.every((service) => requested.includes(service))) {
            return withLossyLast(clean, quality);
        }
    } catch { /* Retain the vetted fallback order if the bridge is unavailable. */ }
    return fallback;
}
