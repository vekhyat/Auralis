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
const eventListeners = new Map<string, Set<(...args: unknown[]) => void>>();
const emit = (name: string, payload: unknown) => eventListeners.get(name)?.forEach((listener) => listener(payload));
const tastePreview = { enabled: false, empty: false, failShelves: false, failDownloads: false, spotify: false, lastfm: false, clientId: "", username: "", lastSync: "", count: 24, dismissed: new Set<string>(), banned: new Set<string>(), pinned: new Set<string>() };
let syncTimers: ReturnType<typeof setTimeout>[] = [];
const tasteItems = records.slice(0, 5).map((record, index) => ({
    id: `taste${index}`, kind: index === 0 ? "track" : "album", title: record.name, artist: record.artist,
    image: record.image, spotify_id: index === 0 ? tracks(index)[0].spotify_id : "", album_id: index === 0 ? "" : record.id,
    reason: "Saved on Spotify", reason_code: index === 0 ? "saved_spotify" : "liked_tracks", reason_arg: index === 0 ? "" : "3",
}));
const runtime = new Proxy({}, { get: (_, name) => {
    if (name === "EventsOnMultiple") return (event: string, listener: (...args: unknown[]) => void) => {
        const listeners = eventListeners.get(event) ?? new Set();
        listeners.add(listener); eventListeners.set(event, listeners);
        return () => listeners.delete(listener);
    };
    if (name === "ClipboardGetText") return async () => "";
    return () => {};
} });
const backend = new Proxy({}, { get: (_, name: string) => async (...args: unknown[]) => {
    calls.push(name);
    if (name === "GetTasteSettings") return { for_you_enabled: tastePreview.enabled, spotify_client_id: tastePreview.clientId, spotify_redirect_uri: "http://127.0.0.1", spotify_connected: tastePreview.spotify, lastfm_username: tastePreview.username, lastfm_configured: tastePreview.lastfm, last_sync: tastePreview.lastSync, event_count: tastePreview.count };
    if (name === "SetForYouEnabled") { tastePreview.enabled = Boolean(args[0]); return; }
    if (name === "GetTasteShelves") {
        if (tastePreview.failShelves) throw new Error("Simulated shelf load failure");
        if (tastePreview.empty) return [];
        const items = tasteItems.filter((item) => !tastePreview.dismissed.has(item.id) && !tastePreview.banned.has(item.artist.toLowerCase()));
        return [{ id: "liked-not-downloaded", seed: "", items }, { id: "similar-artists", seed: records[0].artist, items: tastePreview.banned.has(records[4].artist.toLowerCase()) || tastePreview.dismissed.has("artist-preview") ? [] : [{ id: "artist-preview", kind: "artist", title: records[4].artist, artist: records[4].artist, image: records[4].image, reason: "Similar artist", reason_code: "similar_to", reason_arg: records[0].artist }] }];
    }
    if (name === "GetTasteSummary") return { top_artists: tastePreview.empty ? [] : records.slice(0, 4).map((r) => r.artist), top_genres: tastePreview.empty ? [] : ["rock", "jazz", "pop"], pinned_artists: [...tastePreview.pinned], banned_artists: [...tastePreview.banned], event_count: tastePreview.count, last_sync: tastePreview.lastSync };
    if (name === "DismissTasteItem") { tastePreview.dismissed.add(String(args[0])); return; }
    if (name === "BanTasteArtist") { tastePreview.banned.add(String(args[0]).toLowerCase()); return; }
    if (name === "PinTasteArtist") { tastePreview.pinned.add(String(args[0])); return; }
    if (name === "UnpinTasteArtist") { tastePreview.pinned.delete(String(args[0])); return; }
    if (name === "SetSpotifyClientID") { tastePreview.clientId = String(args[0]); return; }
    if (name === "ConnectSpotify" || name === "DisconnectSpotify") { tastePreview.spotify = name === "ConnectSpotify"; return; }
    if (name === "SetLastFMCredentials") { tastePreview.lastfm = true; tastePreview.username = String(args[1]); return; }
    if (name === "DisconnectLastFM") { tastePreview.lastfm = false; tastePreview.username = ""; return; }
    if (name === "SelectSpotifyExportFile") return "C:\\Sample\\StreamingHistory.json";
    if (name === "SelectFolder") return "C:\\Sample\\SpotifyExport";
    if (name === "ImportSpotifyExport") { emit("taste:import-progress", { phase: "reading", count: 24 }); tastePreview.count += 24; return 24; }
    if (name === "SyncTasteNow") {
        emit("taste:sync-progress", { phase: "spotify_api", message: "", current: 0, total: 3, done: false });
        syncTimers = [setTimeout(() => emit("taste:sync-progress", { phase: "profile", message: "", current: 1, total: 3, done: false }), 10000), setTimeout(() => {
            tastePreview.lastSync = new Date().toISOString();
            emit("taste:sync-progress", { phase: "done", message: "", current: 3, total: 3, done: true });
        }, 20000)];
        return;
    }
    if (name === "CancelTasteSync") { syncTimers.forEach(clearTimeout); emit("taste:sync-progress", { phase: "cancelled", message: "", current: 0, total: 3, done: true }); return; }
    if (name === "LoadPersistentDownloadQueue") return storedQueue;
    if (name === "ReplacePersistentDownloadQueue") { storedQueue = String(args[0]); return; }
    if (name === "GetRecentFetches") return JSON.stringify(records);
    if (name === "CheckFFmpegInstalled") return true;
    if (name === "LoadSettings" || name === "GetDefaults") return { downloadPath: "C:\\Music\\Auralis", language: "en", themeMode: "light", operatingSystem: "windows", checkForUpdates: false };
    if (name === "LoadFonts") return [];
    if (name === "ListCommunitySources" || name === "GetCommunitySourceChecks") return [];
    if (name === "GetDownloadProgress") return { is_downloading: transferring, mb_downloaded: transferring ? (Date.now() - transferStart) / 200 : 0, speed_mbps: transferring ? 5 : 0 };
    if (name === "GetSpotifyMetadata") {
        if (tastePreview.failDownloads) throw new Error("Simulated metadata failure");
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
Object.assign(window, { runtime, go: { main: { App: backend } }, __previewCalls: calls, __tastePreview: tastePreview, __previewEmit: emit });
const { initializeQueuePersistence } = await import("./lib/queue");
await initializeQueuePersistence();
const { default: App } = await import("./App");
const { Toaster } = await import("./components/ui/sonner");
createRoot(document.getElementById("root")!).render(<StrictMode><App /><Toaster position="bottom-left" offset={{ bottom: 92, left: 20 }} duration={1000} /></StrictMode>);
const notice = document.createElement("div");
notice.textContent = "UI preview · sample listening data & simulated transfers";
notice.style.cssText = "position:fixed;top:48px;right:150px;font-size:9px;z-index:100;color:var(--muted-foreground);pointer-events:none";
document.body.append(notice);
