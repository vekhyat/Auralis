import type { TrackAvailability } from "@/types/api";

export function hasAvailabilityLinks(availability?: TrackAvailability): boolean {
    return Boolean(availability && [availability.tidal_url, availability.qobuz_url, availability.amazon_url]
        .some((url) => typeof url === "string" && url.trim() !== ""));
}
