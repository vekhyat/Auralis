import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { isSourceVerification, sourceVerificationBridge, type SourceVerification } from "@/lib/source-verification";

// The native child browser occupies only this viewport. Provider pages are never
// navigated into the privileged Wails frontend or embedded in an iframe.
export function SourceVerificationPane() {
    const { t } = useTranslation();
    const [verification, setVerification] = useState<SourceVerification | null>(null);
    const [error, setError] = useState("");
    const [confirming, setConfirming] = useState(false);
    const [cancelling, setCancelling] = useState(false);
    const viewport = useRef<HTMLDivElement>(null);
    const cancelButton = useRef<HTMLButtonElement>(null);
    const activeId = verification?.active ? verification.id : null;
    const currentId = useRef<number | null>(null);

    useEffect(() => {
        let live = true;
        let events = 0;
        let latestId = -1;
        const unsubscribe = EventsOn("source-verification", (value: unknown) => {
            if (live && isSourceVerification(value) && value.id >= latestId) {
                events++; latestId = value.id; currentId.current = value.active ? value.id : null;
                setVerification(value); setError("");
                if (!value.active) { setConfirming(false); setCancelling(false); }
            }
        });
        try {
            void sourceVerificationBridge().GetSourceVerification().then(value => {
                if (live && events === 0 && isSourceVerification(value)) {
                    latestId = value.id; currentId.current = value.active ? value.id : null; setVerification(value);
                }
            }).catch(() => { /* Older builds retain their own verification flow. */ });
        } catch { /* Browser previews have no native host. */ }
        return () => { live = false; unsubscribe(); };
    }, []);

    useEffect(() => {
        if (activeId === null) return;
        const previousFocus = document.activeElement;
        cancelButton.current?.focus();
        let frame = 0;
        let live = true;
        const sync = () => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => {
                const rect = viewport.current?.getBoundingClientRect();
                if (!live || !rect) return;
                const scale = window.devicePixelRatio || 1;
                const hidden = document.hidden || rect.width <= 0 || rect.height <= 0;
                void sourceVerificationBridge().SetSourceVerificationViewport(activeId,
                    Math.round(rect.left * scale), Math.round(rect.top * scale),
                    hidden ? 0 : Math.round(rect.width * scale), hidden ? 0 : Math.round(rect.height * scale),
                ).catch(err => { if (live) setError(String(err)); });
            });
        };
        const observer = new ResizeObserver(sync);
        if (viewport.current) observer.observe(viewport.current);
        window.addEventListener("resize", sync);
        document.addEventListener("visibilitychange", sync);
        sync();
        return () => {
            live = false; cancelAnimationFrame(frame); observer.disconnect();
            window.removeEventListener("resize", sync);
            document.removeEventListener("visibilitychange", sync);
            void sourceVerificationBridge().SetSourceVerificationViewport(activeId, 0, 0, 0, 0).catch(() => {});
            if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
        };
    }, [activeId]);

    if (activeId === null || !verification) return null;
    const cancel = async () => {
        setCancelling(true); setError("");
        try { await sourceVerificationBridge().CancelSourceVerification(activeId); }
        catch (err) { if (currentId.current === activeId) setError(String(err)); }
        finally { if (currentId.current === activeId) setCancelling(false); }
    };
    const confirm = async () => {
        setConfirming(true); setError("");
        try { await sourceVerificationBridge().ConfirmSourceVerification(); }
        catch (err) { if (currentId.current === activeId) setError(String(err)); }
        finally { if (currentId.current === activeId) setConfirming(false); }
    };

    return <section className="fixed bottom-[76px] left-[184px] right-0 top-16 z-50 flex flex-col bg-background px-8 py-6" aria-labelledby="source-verification-title">
        <div className="flex items-start justify-between gap-6 border-b border-border pb-4">
            <div>
                <h2 id="source-verification-title" className="text-lg font-semibold">{t("translation.sourceVerification.title")}</h2>
                <p className="mt-1 text-sm text-muted-foreground">{verification.host}</p>
                <p className="mt-2 max-w-[70ch] text-sm">{t(verification.can_confirm ? "translation.sourceVerification.manualHelp" : "translation.sourceVerification.automaticHelp")}</p>
            </div>
            <Button ref={cancelButton} type="button" variant="outline" onClick={() => void cancel()} disabled={cancelling}>{t("translation.sourceVerification.cancel")}</Button>
        </div>
        <div ref={viewport} className="relative my-4 min-h-0 flex-1 border border-border bg-muted/30" aria-label={t("translation.sourceVerification.browserLabel")}>
            {!verification.ready && <p className="p-4 text-sm text-muted-foreground" role="status">{t("translation.sourceVerification.opening")}</p>}
        </div>
        <div className="flex min-h-9 items-center justify-between gap-4">
            <p className="max-w-[70ch] text-sm text-destructive" role={error || verification.error ? "alert" : undefined}>{error || verification.error || ""}</p>
            {verification.can_confirm && <Button type="button" disabled={confirming || cancelling || !verification.ready} onClick={() => void confirm()}>{t(confirming ? "translation.sourceVerification.checking" : "translation.sourceVerification.confirm")}</Button>}
        </div>
    </section>;
}
