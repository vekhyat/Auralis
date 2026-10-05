import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
    Sparkles,
    Check,
    X,
    ExternalLink,
    RefreshCw,
    FileUp,
    FolderOpen,
    Key,
    User,
    Trash2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { TasteSyncStatus } from "@/components/TasteSyncStatus";
import { useTasteSync, startTasteSync, cancelTasteSync, type TasteSyncProgress } from "@/hooks/useTasteSync";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { openExternal } from "@/lib/utils";
import {
    GetTasteSettings,
    SetForYouEnabled,
    SetSpotifyClientID,
    ConnectSpotify,
    DisconnectSpotify,
    SetLastFMCredentials,
    DisconnectLastFM,
    SelectSpotifyExportFile,
    SelectFolder,
    ImportSpotifyExport,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { taste } from "../../wailsjs/go/models";

/** Mirrors taste.ImportProgress. */
interface ImportProgressEvent {
    phase: string;
    message: string;
    count: number;
}

interface ListeningConnectionsSettingsProps {
    onForYouToggle?: (enabled: boolean) => void;
}

export function ListeningConnectionsSettings({ onForYouToggle }: ListeningConnectionsSettingsProps) {
    const { t } = useTranslation();
    const [settings, setSettings] = useState<taste.Settings | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState(false);
    const [savingToggle, setSavingToggle] = useState(false);
    const [savingClientId, setSavingClientId] = useState(false);

    // Form inputs
    const [spotifyClientId, setSpotifyClientId] = useState("");
    const [isConnectingSpotify, setIsConnectingSpotify] = useState(false);

    const [lastfmApiKey, setLastfmApiKey] = useState("");
    const [lastfmUser, setLastfmUser] = useState("");
    const [isSavingLastfm, setIsSavingLastfm] = useState(false);

    // Sync state
    const { syncing, syncProgress } = useTasteSync();

    // Import state
    const [importing, setImporting] = useState(false);
    const [importProgress, setImportProgress] = useState<ImportProgressEvent | null>(null);

    const refreshSettings = useCallback(async () => {
        try {
            const s = await GetTasteSettings();
            setSettings(s);
            setSpotifyClientId(s.spotify_client_id || "");
            setLastfmUser(s.lastfm_username || "");
            setLoadError(false);
        } catch (err) {
            console.error("Failed to load taste settings:", err);
            setLoadError(true);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        const timer = window.setTimeout(() => {
            void refreshSettings();
        }, 0);

        const unsubSync = EventsOn("taste:sync-progress", (p: TasteSyncProgress) => {
            if (p.done) {
                void refreshSettings();
                if (p.phase === "cancelled") {
                    toast.info(t("translation.connections.syncCancelled"));
                    return;
                }
                if (p.error) {
                    toast.error(t("translation.connections.syncError", { error: p.error }));
                } else {
                    toast.success(t("translation.connections.syncSuccess"));
                }
            }
        });

        // The import binding returns the result; events only drive progress.
        const unsubImport = EventsOn("taste:import-progress", (p: ImportProgressEvent) => {
            setImportProgress(p);
        });

        return () => {
            window.clearTimeout(timer);
            unsubSync();
            unsubImport();
        };
    }, [refreshSettings, t]);

    const handleToggleForYou = async (checked: boolean) => {
        setSavingToggle(true);
        try {
            await SetForYouEnabled(checked);
            setSettings((prev) => (prev ? { ...prev, for_you_enabled: checked } : null));
            onForYouToggle?.(checked);
            toast.success(
                checked
                    ? t("translation.connections.forYouEnabledToast")
                    : t("translation.connections.forYouDisabledToast")
            );
        } catch (err) {
            toast.error(String(err));
        } finally {
            setSavingToggle(false);
        }
    };

    const handleSaveSpotifyClientId = async () => {
        setSavingClientId(true);
        try {
            await SetSpotifyClientID(spotifyClientId.trim());
            toast.success(t("translation.connections.clientIdSaved"));
            await refreshSettings();
        } catch (err) {
            toast.error(String(err));
        } finally {
            setSavingClientId(false);
        }
    };

    const handleConnectSpotify = async () => {
        setIsConnectingSpotify(true);
        try {
            if (spotifyClientId.trim()) {
                await SetSpotifyClientID(spotifyClientId.trim());
            }
            await ConnectSpotify();
            toast.success(t("translation.connections.spotifyConnectedToast"));
            await refreshSettings();
        } catch (err) {
            toast.error(t("translation.connections.spotifyConnectError", { error: String(err) }));
        } finally {
            setIsConnectingSpotify(false);
        }
    };

    const handleDisconnectSpotify = async () => {
        try {
            await DisconnectSpotify();
            toast.success(t("translation.connections.spotifyDisconnectedToast"));
            await refreshSettings();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleSaveLastFM = async () => {
        setIsSavingLastfm(true);
        try {
            await SetLastFMCredentials(lastfmApiKey.trim(), lastfmUser.trim());
            setLastfmApiKey("");
            toast.success(t("translation.connections.lastfmSavedToast"));
            await refreshSettings();
        } catch (err) {
            toast.error(String(err));
        } finally {
            setIsSavingLastfm(false);
        }
    };

    const handleDisconnectLastFM = async () => {
        try {
            await DisconnectLastFM();
            toast.success(t("translation.connections.lastfmDisconnectedToast"));
            setLastfmApiKey("");
            await refreshSettings();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleImportExport = async (folder = false) => {
        setImportProgress(null);
        setImporting(true);
        try {
            const path = folder ? await SelectFolder("") : await SelectSpotifyExportFile();
            if (!path) return;
            const count = await ImportSpotifyExport(path);
            toast.success(t("translation.connections.importSuccess", { count }));
            await refreshSettings();
        } catch (err) {
            toast.error(t("translation.connections.importError", { error: String(err) }));
        } finally {
            setImporting(false);
        }
    };

    const handleSyncNow = async () => {
        try {
            await startTasteSync();
        } catch (err) {
            toast.error(String(err));
        }
    };

    const handleCancelSync = async () => {
        try { await cancelTasteSync(); } catch (err) { toast.error(String(err)); }
    };

    if (loading) {
        return <p role="status">{t("translation.connections.loading")}</p>;
    }

    if (loadError) return <section className="space-y-3" role="alert">
        <h2 className="text-sm font-semibold">{t("translation.connections.title")}</h2>
        <p className="text-sm text-destructive">{t("translation.connections.loadError")}</p>
        <Button variant="outline" size="sm" onClick={() => void refreshSettings()}>{t("translation.connections.retry")}</Button>
    </section>;

    return (
        <section className="max-w-4xl space-y-6">
            <div className="flex items-center justify-between border-b border-border pb-1.5">
                <div>
                    <h2 className="text-sm font-semibold tracking-tight">
                        {t("translation.connections.title")}
                    </h2>
                    <p className="text-xs text-muted-foreground mt-0.5">
                        {t("translation.connections.subtitle")}
                    </p>
                </div>
            </div>

            {/* Enable For You Toggle */}
            <div className="flex items-center justify-between rounded-lg border border-border p-4 bg-card">
                <div className="space-y-0.5 pr-4">
                    <div className="flex items-center gap-2">
                        <Sparkles className="size-4 text-primary" />
                        <Label htmlFor="toggle-for-you" className="text-sm font-medium cursor-pointer">
                            {t("translation.connections.enableForYou")}
                        </Label>
                    </div>
                    <p className="text-xs text-muted-foreground">
                        {t("translation.connections.enableForYouDesc")}
                    </p>
                </div>
                <Switch
                    id="toggle-for-you"
                    checked={settings?.for_you_enabled ?? false}
                    disabled={savingToggle}
                    onCheckedChange={handleToggleForYou}
                />
            </div>

            {/* Spotify BYO Client ID */}
            <div className="rounded-lg border border-border p-4 bg-card space-y-4">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                        <div className="size-2 rounded-full bg-[#1db954]" />
                        <h3 className="text-sm font-semibold">Spotify</h3>
                    </div>
                    {settings?.spotify_connected ? (
                        <span className="inline-flex items-center gap-1 text-xs text-emerald-500 font-medium">
                            <Check className="size-3.5" />
                            {t("translation.connections.connected")}
                        </span>
                    ) : (
                        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <X className="size-3.5" />
                            {t("translation.connections.notConnected")}
                        </span>
                    )}
                </div>

                <div className="text-xs text-muted-foreground space-y-1.5 bg-muted/40 p-3 rounded-md border border-border/50">
                    <p className="font-medium text-foreground">
                        {t("translation.connections.spotifyGuideTitle")}
                    </p>
                    <ol className="list-decimal pl-4 space-y-1">
                        <li>
                            {t("translation.connections.spotifyGuideStep1")}{" "}
                            <button
                                type="button"
                                onClick={() => openExternal("https://developer.spotify.com/dashboard")}
                                className="inline-flex items-center gap-0.5 text-primary underline hover:opacity-80"
                            >
                                developer.spotify.com <ExternalLink className="size-3" />
                            </button>
                        </li>
                        <li>{t("translation.connections.spotifyGuideStep2")}</li>
                        <li>
                            {t("translation.connections.spotifyGuideStep3")}: {" "}
                            <code className="bg-background px-1.5 py-0.5 rounded text-[11px] font-mono select-all">
                                {settings?.spotify_redirect_uri || "http://127.0.0.1"}
                            </code>
                        </li>
                        <li>{t("translation.connections.spotifyGuideStep4")}</li>
                    </ol>
                    <p>{t("translation.connections.spotifyLoopbackHint")}</p>
                </div>

                <div className="space-y-2">
                    <Label htmlFor="spotify-client-id" className="text-xs">
                        {t("translation.connections.spotifyClientId")}
                    </Label>
                    <div className="flex gap-2">
                        <Input
                            id="spotify-client-id"
                            type="text"
                            placeholder={t("translation.connections.spotifyClientIdPlaceholder")}
                            value={spotifyClientId}
                            onChange={(e) => setSpotifyClientId(e.target.value)}
                            disabled={isConnectingSpotify || savingClientId}
                            className="font-mono text-xs"
                        />
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={handleSaveSpotifyClientId}
                            disabled={!spotifyClientId.trim() || savingClientId || isConnectingSpotify}
                        >
                            {t("translation.connections.save")}
                        </Button>
                    </div>
                </div>

                <div className="flex items-center gap-2 pt-1">
                    {settings?.spotify_connected ? (
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="text-destructive hover:bg-destructive/10"
                            onClick={handleDisconnectSpotify}
                        >
                            <Trash2 className="size-3.5 mr-1.5" />
                            {t("translation.connections.disconnectSpotify")}
                        </Button>
                    ) : (
                        <Button
                            type="button"
                            size="sm"
                            disabled={!spotifyClientId.trim() || isConnectingSpotify || savingClientId}
                            onClick={handleConnectSpotify}
                        >
                            {isConnectingSpotify ? (
                                <RefreshCw className="size-3.5 mr-1.5 animate-spin" />
                            ) : null}
                            {t("translation.connections.connectSpotify")}
                        </Button>
                    )}
                </div>
            </div>

            {/* Last.fm Scrobble Integration */}
            <div className="rounded-lg border border-border p-4 bg-card space-y-4">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                        <div className="size-2 rounded-full bg-[#d51007]" />
                        <h3 className="text-sm font-semibold">Last.fm</h3>
                    </div>
                    {settings?.lastfm_configured ? (
                        <span className="inline-flex items-center gap-1 text-xs text-emerald-500 font-medium">
                            <Check className="size-3.5" />
                            {t("translation.connections.connected")}
                        </span>
                    ) : (
                        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <X className="size-3.5" />
                            {t("translation.connections.notConnected")}
                        </span>
                    )}
                </div>

                <p className="text-xs text-muted-foreground">
                    {t("translation.connections.lastfmDesc")}{" "}
                    <button
                        type="button"
                        onClick={() => openExternal("https://www.last.fm/api/account/create")}
                        className="inline-flex items-center gap-0.5 text-primary underline hover:opacity-80"
                    >
                        {t("translation.connections.getLastfmKey")} <ExternalLink className="size-3" />
                    </button>
                </p>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                        <Label htmlFor="lastfm-username" className="text-xs flex items-center gap-1.5">
                            <User className="size-3" />
                            {t("translation.connections.lastfmUsername")}
                        </Label>
                        <Input
                            id="lastfm-username"
                            type="text"
                            placeholder={t("translation.connections.lastfmUsernamePlaceholder")}
                            value={lastfmUser}
                            onChange={(e) => setLastfmUser(e.target.value)}
                            className="text-xs"
                        />
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="lastfm-api-key" className="text-xs flex items-center gap-1.5">
                            <Key className="size-3" />
                            {t("translation.connections.lastfmApiKey")}
                        </Label>
                        <Input
                            id="lastfm-api-key"
                            type="password"
                            placeholder={t("translation.connections.lastfmApiKeyPlaceholder")}
                            value={lastfmApiKey}
                            onChange={(e) => setLastfmApiKey(e.target.value)}
                            className="font-mono text-xs"
                        />
                    </div>
                </div>

                <div className="flex items-center gap-2 pt-1">
                    <Button
                        type="button"
                        size="sm"
                        disabled={!lastfmUser.trim() || !lastfmApiKey.trim() || isSavingLastfm}
                        onClick={handleSaveLastFM}
                    >
                        {isSavingLastfm ? (
                            <RefreshCw className="size-3.5 mr-1.5 animate-spin" />
                        ) : null}
                        {t("translation.connections.saveLastfm")}
                    </Button>
                    {settings?.lastfm_configured ? (
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="text-destructive hover:bg-destructive/10"
                            onClick={handleDisconnectLastFM}
                        >
                            <Trash2 className="size-3.5 mr-1.5" />
                            {t("translation.connections.disconnectLastfm")}
                        </Button>
                    ) : null}
                </div>
            </div>

            {/* Spotify Export Import */}
            <div className="rounded-lg border border-border p-4 bg-card space-y-3">
                <div className="flex items-center gap-2.5">
                    <FileUp className="size-4 text-muted-foreground" />
                    <div>
                        <h3 className="text-sm font-semibold">
                            {t("translation.connections.importExportTitle")}
                        </h3>
                        <p className="text-xs text-muted-foreground">
                            {t("translation.connections.importExportDesc")}
                        </p>
                    </div>
                </div>

                {importing ? (
                    <div className="flex items-center gap-2 bg-muted/40 p-3 rounded border border-border text-xs text-muted-foreground">
                        <RefreshCw className="size-3.5 animate-spin" />
                        {t("translation.connections.importingCount", { count: importProgress?.count ?? 0 })}
                    </div>
                ) : null}

                <div className="flex flex-wrap gap-2">
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={importing}
                        onClick={() => void handleImportExport()}
                    >
                        <FileUp className="size-3.5 mr-1.5" />
                        {t("translation.connections.selectExportFile")}
                    </Button>
                    <Button type="button" variant="outline" size="sm" disabled={importing} onClick={() => void handleImportExport(true)}>
                        <FolderOpen className="size-3.5 mr-1.5" />
                        {t("translation.connections.selectExportFolder")}
                    </Button>
                </div>
            </div>

            {/* Sync & Taste Stats */}
            <div className="rounded-lg border border-border p-4 bg-card space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h3 className="text-sm font-semibold">{t("translation.connections.syncTitle")}</h3>
                        <p className="text-xs text-muted-foreground">
                            {settings?.last_sync
                                ? t("translation.connections.lastSyncedAt", { time: new Date(settings.last_sync).toLocaleString() })
                                : t("translation.connections.neverSynced")}{" "}
                            • {t("translation.connections.eventsCount", { count: settings?.event_count ?? 0 })}
                        </p>
                    </div>
                    <div className="flex items-center gap-2">
                        {syncing ? (
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={handleCancelSync}
                            >
                                {t("translation.connections.cancelSync")}
                            </Button>
                        ) : (
                            <Button
                                type="button"
                                size="sm"
                                onClick={handleSyncNow}
                                disabled={!settings?.spotify_connected && !settings?.lastfm_configured && (settings?.event_count ?? 0) === 0}
                            >
                                <RefreshCw className="size-3.5 mr-1.5" />
                                {t("translation.connections.syncNow")}
                            </Button>
                        )}
                    </div>
                </div>

                {syncing && syncProgress ? (
                    <TasteSyncStatus progress={syncProgress} />
                ) : null}
            </div>
        </section>
    );
}
