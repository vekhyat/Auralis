import { forwardRef, useCallback, useEffect, useImperativeHandle, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { communitySourceBridge, type CommunitySource, type CommunitySourceCheck } from "@/lib/community-sources";
import { downloadExecution } from "@/lib/download-execution";

export interface CommunitySourcesHandle { save: () => Promise<boolean>; reset: () => void; resetToDefaults: () => Promise<boolean> }

export const CommunitySourcesSettings = forwardRef<CommunitySourcesHandle, { onDirtyChange: (dirty: boolean) => void }>(function CommunitySourcesSettings({ onDirtyChange }, ref) {
    const { t } = useTranslation();
    const [saved, setSaved] = useState<CommunitySource[]>([]);
    const [sources, setSources] = useState<CommunitySource[]>([]);
    const [checks, setChecks] = useState<Record<string, CommunitySourceCheck>>({});
    const [loaded, setLoaded] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState<Record<string, boolean>>({});
    const [saving, setSaving] = useState(false);
    const dirty = JSON.stringify(saved) !== JSON.stringify(sources);

    useEffect(() => { onDirtyChange(dirty); }, [dirty, onDirtyChange]);
    useEffect(() => {
        let live = true;
        const load = async () => {
            try {
                const bridge = communitySourceBridge();
                const [rows, results] = await Promise.all([bridge.ListCommunitySources(), bridge.GetCommunitySourceChecks()]);
                if (live) { setSources(rows); setSaved(rows); setChecks(Object.fromEntries((results ?? []).map(result => [result.id, result]))); setLoaded(true); }
            } catch (err) { if (live) setError(String(err)); }
        };
        void load();
        return () => { live = false; };
    }, []);

    const save = useCallback(async () => {
        if (!loaded || !dirty) return true;
        setSaving(true); setError("");
        try {
            const bridge = communitySourceBridge();
            await bridge.SaveCommunitySources(sources);
            setSaved(sources);
            const results = await bridge.GetCommunitySourceChecks();
            setChecks(Object.fromEntries((results ?? []).map(result => [result.id, result])));
            return true;
        }
        catch (err) { setError(String(err)); return false; }
        finally { setSaving(false); }
    }, [dirty, loaded, sources]);
    // Saving no overrides restores the built-in profiles; the backend also drops
    // checks and timings for every source whose server configuration changed.
    const resetToDefaults = useCallback(async () => {
        setError("");
        try {
            const bridge = communitySourceBridge();
            await bridge.SaveCommunitySources([]);
            const [rows, results] = await Promise.all([bridge.ListCommunitySources(), bridge.GetCommunitySourceChecks()]);
            setSources(rows); setSaved(rows); setChecks(Object.fromEntries((results ?? []).map(result => [result.id, result]))); setLoaded(true);
            return true;
        } catch (err) { setError(String(err)); return false; }
    }, []);
    useImperativeHandle(ref, () => ({ save, reset: () => setSources(saved), resetToDefaults }), [save, saved, resetToDefaults]);

    const change = (id: string, patch: Partial<CommunitySource>) => setSources(current => current.map(source => source.id === id ? { ...source, ...patch } : source));
    const check = async (id: string, download = false) => {
        setBusy(current => ({ ...current, [id]: true })); setError("");
        try {
            const bridge = communitySourceBridge();
            let result: CommunitySourceCheck;
            if (download) {
                const lease = await downloadExecution.acquireWhenUnblocked("direct");
                try { result = await bridge.TestCommunitySourceDownload(id); }
                finally { downloadExecution.release(lease); }
            } else result = await bridge.CheckCommunitySource(id);
            setChecks(current => ({ ...current, [id]: result }));
        } catch (err) { setError(String(err)); }
        finally { setBusy(current => ({ ...current, [id]: false })); }
    };
    const checkAll = async () => {
        const pending = sources.filter(source => source.base_url);
        const workers = Array.from({ length: Math.min(4, pending.length) }, async () => {
            while (pending.length) { const source = pending.shift(); if (source) await check(source.id); }
        });
        await Promise.all(workers);
    };
    const verify = async (id: string) => {
        setBusy(current => ({ ...current, [id]: true })); setError("");
        try {
            const lease = await downloadExecution.acquireWhenUnblocked("direct");
            try {
                const result = await communitySourceBridge().VerifyCommunitySource(id);
                setChecks(current => ({ ...current, [id]: result }));
            } finally { downloadExecution.release(lease); }
        } catch (err) { setError(String(err)); }
        finally { setBusy(current => ({ ...current, [id]: false })); }
    };
    const anyBusy = Object.values(busy).some(Boolean);
    const stateWord = (result?: CommunitySourceCheck) => {
        if (!result) return t("translation.communitySources.unchecked");
        if (result.audio_verified) return t("translation.communitySources.verified");
        if (result.state === "available") return t("translation.communitySources.responding");
        if (result.state === "authentication_required") return t("translation.communitySources.verification");
        if (result.state === "cancelled") return t("translation.communitySources.cancelled");
        return t("translation.communitySources.unavailable");
    };

    return <section className="max-w-3xl space-y-4" aria-labelledby="community-sources-heading">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-1.5">
            <h2 id="community-sources-heading" className="text-sm font-semibold tracking-tight">{t("translation.communitySources.title")}</h2>
            <div className="flex flex-wrap gap-2">
                <Button type="button" variant="outline" size="sm" disabled={!loaded || dirty || anyBusy} onClick={() => void checkAll()}>{t("translation.communitySources.checkAll")}</Button>
                <Button type="button" variant="outline" size="sm" disabled={!dirty || saving || anyBusy} onClick={() => void save()}>{saving ? t("translation.communitySources.saving") : t("translation.communitySources.save")}</Button>
            </div>
        </div>
        <p className="max-w-[70ch] text-sm text-muted-foreground">{t("translation.communitySources.description")}</p>
        {dirty && <p className="text-xs text-muted-foreground" role="status">{t("translation.communitySources.saveBeforeCheck")}</p>}
        {error && <p className="break-words text-sm text-destructive" role="alert">{error}</p>}
        {!loaded && !error && <p className="text-sm text-muted-foreground" role="status">{t("translation.communitySources.loading")}</p>}
        <div className="divide-y divide-border border-y border-border">
            {sources.map(source => {
                const result = checks[source.id];
                return <details key={source.id} className="py-2.5">
                    <summary className="cursor-pointer text-sm focus-visible:outline-2 focus-visible:outline-ring">
                        <span className="ml-1 font-medium">{source.name}</span>
                        <span className="ml-2 text-xs text-muted-foreground">{source.service === "apple" ? "Apple Music" : source.service.toUpperCase()}</span>
                        <span className="ml-2 font-mono text-[11px] text-muted-foreground">{source.enabled ? stateWord(result) : t("translation.communitySources.disabled")}</span>
                        {result && source.enabled && <span className="ml-2 font-mono text-[11px] text-muted-foreground">{Math.round(result.latency_ms)} ms</span>}
                    </summary>
                    <div className="ml-4 mt-4 space-y-3">
                        <div className="flex items-center gap-3">
                            <Switch id={`source-enabled-${source.id}`} checked={source.enabled} disabled={saving || busy[source.id] || !source.base_url} onCheckedChange={enabled => change(source.id, { enabled })} />
                            <Label htmlFor={`source-enabled-${source.id}`}>{t("translation.communitySources.useSource")}</Label>
                        </div>
                        {source.protocol === "subsonic" && <div className="space-y-1.5">
                            <Label htmlFor={`source-service-${source.id}`}>{t("translation.communitySources.musicService")}</Label>
                            <select id={`source-service-${source.id}`} value={source.service} className="h-9 w-full rounded-[2px] border border-input bg-background px-3 text-sm focus-visible:outline-2 focus-visible:outline-ring" onChange={event => change(source.id, { service: event.target.value })}>
                                {["tidal", "qobuz", "amazon", "deezer", "apple"].map(service => <option key={service} value={service}>{service.toUpperCase()}</option>)}
                            </select>
                        </div>}
                        <div className="grid gap-3 sm:grid-cols-2">
                            <div className="space-y-1.5 sm:col-span-2">
                                <Label htmlFor={`source-url-${source.id}`}>{t("translation.communitySources.serverUrl")}</Label>
                                <Input id={`source-url-${source.id}`} type="url" value={source.base_url} disabled={saving || busy[source.id]} onChange={event => change(source.id, { base_url: event.target.value })} placeholder="https://your-server.example" />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor={`source-env-${source.id}`}>{t("translation.communitySources.credentialEnv")}</Label>
                                <Input id={`source-env-${source.id}`} value={source.credential_env ?? ""} disabled={saving || busy[source.id]} onChange={event => change(source.id, { credential_env: event.target.value })} placeholder="AURALIS_SOURCE_KEY" />
                            </div>
                            <div className="space-y-1.5">
                                <Label htmlFor={`source-auth-${source.id}`}>{t("translation.communitySources.credentialType")}</Label>
                                <select id={`source-auth-${source.id}`} className="h-9 w-full rounded-[2px] border border-input bg-background px-3 text-sm focus-visible:outline-2 focus-visible:outline-ring" value={source.credential_type ?? ""} disabled={saving || busy[source.id]} onChange={event => change(source.id, { credential_type: event.target.value as CommunitySource["credential_type"] })}>
                                    <option value="">{t("translation.communitySources.noCredential")}</option><option value="api_key">API key</option><option value="bearer">Bearer token</option><option value="cookie">{t("translation.communitySources.sessionCookie")}</option>{source.protocol === "subsonic" && <option value="subsonic">Subsonic user:password</option>}
                                </select>
                            </div>
                        </div>
                        <p className="text-xs text-muted-foreground">{t("translation.communitySources.credentialHelp")}</p>
                        {result && <p className="break-words text-xs text-muted-foreground" role="status">{result.message}</p>}
                        <div className="flex flex-wrap gap-2">
                            <Button type="button" variant="outline" size="sm" disabled={dirty || saving || anyBusy || !source.base_url} onClick={() => void check(source.id)}>{busy[source.id] ? t("translation.communitySources.checking") : t("translation.communitySources.checkApi")}</Button>
                            <Button type="button" variant="outline" size="sm" disabled={dirty || saving || anyBusy || !source.base_url} onClick={() => void check(source.id, true)}>{t("translation.communitySources.testDownload")}</Button>
                            {(source.credential_type === "cookie" || (!source.credential_type && ["hifi", "dab", "lucida"].includes(source.protocol))) && <Button type="button" variant="outline" size="sm" disabled={dirty || saving || anyBusy || !source.base_url} onClick={() => void verify(source.id)}>{t("translation.sourceVerification.verify")}</Button>}
                        </div>
                    </div>
                </details>;
            })}
        </div>
    </section>;
});
