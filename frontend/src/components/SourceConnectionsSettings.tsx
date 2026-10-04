import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { downloadExecution } from "@/lib/download-execution";
import { sourceVerificationBridge, type SourceConnection } from "@/lib/source-verification";

export function SourceConnectionsSettings() {
    const { t } = useTranslation();
    const [rows, setRows] = useState<SourceConnection[]>([]);
    const [busy, setBusy] = useState<string | null>(null);
    const [error, setError] = useState("");
    useEffect(() => {
        let live = true;
        void Promise.resolve().then(() => sourceVerificationBridge().GetSourceConnections()).then(value => { if (live) setRows(value ?? []); }).catch(err => { if (live) setError(String(err)); });
        return () => { live = false; };
    }, []);
    const check = async (id: string, verify: boolean) => {
        setBusy(id); setError("");
        try {
            const bridge = sourceVerificationBridge();
            let result: SourceConnection;
            if (verify) {
                const lease = await downloadExecution.acquireWhenUnblocked("direct");
                try { result = await bridge.VerifySourceConnection(id); }
                finally { downloadExecution.release(lease); }
            } else result = await bridge.CheckSourceConnection(id);
            setRows(current => current.map(row => row.id === result.id ? result : row));
        } catch (err) { setError(String(err)); }
        finally { setBusy(null); }
    };
    const stateLabel = (state: string) => {
        switch (state) {
            case "available": return t("translation.communitySources.responding");
            case "authentication_required": return t("translation.communitySources.verification");
            case "cancelled": return t("translation.communitySources.cancelled");
            case "failed": return t("translation.communitySources.unavailable");
            default: return t("translation.communitySources.unchecked");
        }
    };
    return <section className="max-w-3xl space-y-4" aria-labelledby="source-connections-heading">
        <h2 id="source-connections-heading" className="border-b border-border pb-1.5 text-sm font-semibold">{t("translation.sourceVerification.connections")}</h2>
        <p className="max-w-[70ch] text-sm text-muted-foreground">{t("translation.sourceVerification.connectionsHelp")}</p>
        {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
        {!rows.length && !error && <p className="text-sm text-muted-foreground" role="status">{t("translation.communitySources.loading")}</p>}
        {(["download", "resources"] as const).map(group => <div key={group}>
            <h3 className="mb-2 text-sm font-medium">{t(group === "download" ? "translation.sourceVerification.downloadSources" : "translation.sourceVerification.resources")}</h3>
            <div className="divide-y divide-border border-y border-border">
                {rows.filter(row => group === "download" ? row.role === "download" : row.role !== "download").map(row => <div key={row.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                    <div className="min-w-0 flex-1">
                        <p className="text-sm font-medium">{row.name}<span className="ml-3 text-xs font-normal text-muted-foreground">{stateLabel(row.state)}</span></p>
                        {row.message && <p className="mt-1 max-w-[70ch] text-xs text-muted-foreground">{row.message}</p>}
                    </div>
                    <div className="flex gap-2">
                        <Button type="button" variant="outline" size="sm" disabled={busy !== null} onClick={() => void check(row.id, false)}>{t(busy === row.id ? "translation.communitySources.checking" : "translation.communitySources.checkApi")}</Button>
                        {row.can_verify && <Button type="button" variant="outline" size="sm" disabled={busy !== null} onClick={() => void check(row.id, true)}>{t("translation.sourceVerification.verify")}</Button>}
                    </div>
                </div>)}
            </div>
        </div>)}
    </section>;
}
