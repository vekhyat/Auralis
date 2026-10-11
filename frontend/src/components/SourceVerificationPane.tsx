import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { isSourceVerification, sourceVerificationBridge, type SourceVerification } from "@/lib/source-verification";

// The native child view occupies only this viewport. Provider pages are never
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

    // A compact dialog over the app. The native view is clipped to the page, so
    // no browser title, address, or window controls appear. Sign-in pages get a
    // taller viewport than a single challenge widget.
    const size = verification.can_confirm ? "h-[420px] w-[480px]" : "h-[300px] w-[380px]";
    const message = error || verification.error || "";
    return <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
        onKeyDown={event => { if (event.key === "Escape" && !cancelling) { event.preventDefault(); void cancel(); } }}>
        <section role="dialog" aria-modal="true" aria-labelledby="source-verification-title" aria-describedby="source-verification-help"
            className="flex max-h-full max-w-full flex-col gap-3 rounded-lg border border-border bg-background p-4 shadow-2xl">
            <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                    <h2 id="source-verification-title" className="text-sm font-semibold">{t("translation.sourceVerification.title")}</h2>
                    <p id="source-verification-help" className="mt-1 max-w-[46ch] text-xs text-muted-foreground">{t(verification.can_confirm ? "translation.sourceVerification.manualHelp" : "translation.sourceVerification.automaticHelp")}</p>
                </div>
                <Button ref={cancelButton} type="button" variant="ghost" size="sm" onClick={() => void cancel()} disabled={cancelling}>{t("translation.sourceVerification.cancel")}</Button>
            </div>
            <div ref={viewport} className={`relative max-h-[calc(100vh-12rem)] max-w-[calc(100vw-4rem)] shrink overflow-hidden rounded-md bg-muted/30 ${size}`} aria-label={t("translation.sourceVerification.browserLabel")}>
                {!verification.ready && <div className="flex h-full items-center justify-center gap-2 text-xs text-muted-foreground" role="status"><Spinner role="presentation" aria-hidden="true" />{t("translation.sourceVerification.opening")}</div>}
            </div>
            {(message || verification.can_confirm) && <div className="flex items-center justify-between gap-4">
                <p className="max-w-[46ch] text-xs text-destructive" role={message ? "alert" : undefined}>{message}</p>
                {verification.can_confirm && <Button type="button" size="sm" disabled={confirming || cancelling || !verification.ready} onClick={() => void confirm()}>{t(confirming ? "translation.sourceVerification.checking" : "translation.sourceVerification.confirm")}</Button>}
            </div>}
        </section>
    </div>;
}
