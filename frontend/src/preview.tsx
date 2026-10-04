/** Development-only browser harness. All transfers and persistence are simulated.
 * Artwork and album metadata originate from Apple's public catalog API.
 * This entry is separate from index.html and is excluded from production builds.
 */
import { createRoot } from "react-dom/client";
import { StrictMode } from "react";
import albums from "./preview-albums.json";
import "./index.css";

if (!import.meta.env.DEV) throw new Error("UI preview requires the development server");

const records = albums.map((album, index) => ({
    id: `preview${index}`, url: `https://open.spotify.com/album/preview${index}`, type: "album",
    name: album.collectionName, artist: album.artistName,
    image: album.artworkUrl100.replace('100x100bb', '600x600bb'), timestamp: Date.now() - index * 3600000,
}));
const tracks = (index: number) => Array.from({ length: 6 }, (_, n) => ({
    spotify_id: `preview${index}track${n}`, name: `Preview track ${n + 1}`, artists: records[index].artist,
    album_name: records[index].name, images: records[index].image, duration_ms: 210000 + n * 17000,
    track_number: n + 1, release_date: albums[index].releaseDate.slice(0, 10),
    external_urls: `https://open.spotify.com/track/preview${index}track${n}`,
}));
let storedQueue = "[]";
let transferring = false;
let cancelled = false;
let transferStart = 0;
const calls: string[] = [];
const runtime = new Proxy({}, { get: (_, name) => {
    if (name === "EventsOnMultiple") return () => () => {};
    if (name === "ClipboardGetText") return async () => "";
    return () => {};
} });
const backend = new Proxy({}, { get: (_, name: string) => async (...args: unknown[]) => {
    calls.push(name);
    if (name === "LoadPersistentDownloadQueue") return storedQueue;
    if (name === "ReplacePersistentDownloadQueue") { storedQueue = String(args[0]); return; }
    if (name === "GetRecentFetches") return JSON.stringify(records);
    if (name === "CheckFFmpegInstalled") return true;
    if (name === "LoadSettings" || name === "GetDefaults") return { downloadPath: "C:\\Music\\Auralis", language: "en", themeMode: "light", operatingSystem: "windows", checkForUpdates: false };
    if (name === "LoadFonts") return [];
    if (name === "GetDownloadProgress") return { is_downloading: transferring, mb_downloaded: transferring ? (Date.now() - transferStart) / 200 : 0, speed_mbps: transferring ? 5 : 0 };
    if (name === "GetSpotifyMetadata") {
        const request = args[0] as { url: string };
        const index = Number(request.url.match(/preview(\d)/)?.[1] ?? 0);
        if (request.url.includes("/track/")) return JSON.stringify({ track: tracks(index)[0] });
        return JSON.stringify({ album_info: { name: records[index].name, artists: records[index].artist, images: records[index].image, release_date: albums[index].releaseDate.slice(0, 10), total_tracks: 6 }, track_list: tracks(index) });
    }
    if (name === "SearchSpotify") return { tracks: [], albums: records.map((r) => ({ ...r, images: r.image, external_urls: r.url, artists: r.artist, total_tracks: 6 })), artists: [], playlists: [] };
    if (name === "CheckFilesExistence") return [];
    if (name === "GetStreamingURLs") return "{}";
    if (name === "GetHistory" || name === "GetDownloadHistory" || name === "GetFetchHistory") return [];
    if (name === "DownloadTrack") {
        transferring = true; cancelled = false; transferStart = Date.now();
        await new Promise((resolve) => setTimeout(resolve, 8000));
        transferring = false;
        return cancelled ? { success: false, cancelled: true, error: "cancelled" } : { success: true, message: "Preview transfer complete", file: "C:\\Music\\Auralis\\preview.flac" };
    }
    if (name === "ForceStopDownloads") { cancelled = true; return; }
    if (name === "AddToDownloadQueue") return crypto.randomUUID();
    if (name === "GetPlatform") return "windows";
    // Reads not needed for this UI harness return empty data. Nothing touches
    // the user's Wails app, database, files, or provider sessions.
    return undefined;
} });
Object.assign(window, { runtime, go: { main: { App: backend } }, __previewCalls: calls });
const { initializeQueuePersistence } = await import("./lib/queue");
await initializeQueuePersistence();
const { default: App } = await import("./App");
const { Toaster } = await import("./components/ui/sonner");
createRoot(document.getElementById("root")!).render(<StrictMode><App /><Toaster position="bottom-left" offset={{ bottom: 92, left: 20 }} duration={1000} /></StrictMode>);
const notice = document.createElement("div");
notice.textContent = "UI preview · sample track lists & simulated transfers";
notice.style.cssText = "position:fixed;top:48px;right:150px;font-size:9px;z-index:100;color:var(--muted-foreground);pointer-events:none";
document.body.append(notice);
