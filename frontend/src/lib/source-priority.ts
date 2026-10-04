type SourcePriorityWindow = Window & {
    go?: { main?: { App?: { RankDownloadServices?: (services: string[], quality: string) => Promise<string[]> } } };
};

export async function rankDownloadServices(services: string[], quality: string): Promise<string[]> {
    const rank = (window as SourcePriorityWindow).go?.main?.App?.RankDownloadServices;
    if (!rank) return services;
    try {
        const ranked = await rank(services, quality);
        if (ranked.length === services.length && new Set(ranked).size === services.length && ranked.every(service => services.includes(service))) return ranked;
    } catch { /* Retain the configured fallback order if the bridge is unavailable. */ }
    return services;
}
