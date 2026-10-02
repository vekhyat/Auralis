import { useEffect, useState } from "react";
import { t } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { cn } from "@/lib/utils";
import { CancelIPod, DoctorIPod, EjectIPod, ListIPods, RemoveFromIPod, SelectAudioFiles, SelectFolder, SendToIPod } from "../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";

interface DeviceTrack {
    id: number;
    title: string;
    artist: string;
    album: string;
    path: string;
    size: number;
}

interface IPod {
    id: string;
    name: string;
    mount: string;
    mode: string;
    canSend: boolean;
    warning: string;
    hasRockbox: boolean;
    generation: string;
    checksum: string;
    firewireId: string;
    totalBytes: number;
    freeBytes: number;
    tracks: DeviceTrack[] | null;
    trackCount: number;
}

interface ProgressEvent {
    phase: string;
    current: number;
    total: number;
    name: string;
}

interface DoctorReport {
    orphans: DeviceTrack[] | null;
    missing: DeviceTrack[] | null;
    changed: boolean;
}

const WARNINGS: Record<string, string> = {
    "no-drive": "translation.devices.warnNoDrive",
    unsupported: "translation.devices.warnUnsupported",
    unverified: "translation.devices.warnUnverified",
    database: "translation.devices.warnDatabase",
    rockbox: "translation.devices.warnRockbox",
    "no-firewire": "translation.devices.warnNoFirewire",
};

const MODES: Record<string, string> = {
    stock: "translation.devices.modeStock",
    rockbox: "translation.devices.modeRockbox",
    unreadable: "translation.devices.modeUnreadable",
    unsupported: "translation.devices.modeUnsupported",
};

function formatBytes(value: number): string {
    if (!value || value < 0) return "0 MB";
    const gb = value / (1024 * 1024 * 1024);
    if (gb >= 10) return `${Math.round(gb)} GB`;
    if (gb >= 1) return `${gb.toFixed(1)} GB`;
    return `${Math.max(1, Math.round(value / (1024 * 1024)))} MB`;
}

function errorText(error: unknown): string {
    if (typeof error === "string") return error;
    if (error && typeof error === "object" && "message" in error) return String((error as { message: unknown }).message);
    return String(error ?? "");
}

export function DevicesPage() {
    const [devices, setDevices] = useState<IPod[]>([]);
    const [selectedId, setSelectedId] = useState("");
    const [format, setFormat] = useState<"alac" | "aac">("alac");
    const [picked, setPicked] = useState<number[]>([]);
    const [busy, setBusy] = useState(false);
    const [progress, setProgress] = useState<ProgressEvent | null>(null);
    const [doctor, setDoctor] = useState<DoctorReport | null>(null);
    const [confirm, setConfirm] = useState<"remove" | "rebuild" | null>(null);

    const load = async () => {
        try {
            const list = ((await ListIPods()) || []) as IPod[];
            setDevices(list);
            setSelectedId((current) => (list.some((device) => device.id === current) ? current : list[0]?.id || ""));
        }
        catch (error) {
            console.error(error);
        }
    };

    useEffect(() => {
        void load();
        const timer = window.setInterval(() => void load(), 2000);
        return () => window.clearInterval(timer);
    }, []);

    useEffect(() => {
        EventsOn("ipod:progress", (event: ProgressEvent) => {
            setProgress(event);
            if (event?.phase === "done") {
                window.setTimeout(() => setProgress(null), 1200);
            }
        });
        return () => {
            EventsOff("ipod:progress");
        };
    }, []);

    const device = devices.find((item) => item.id === selectedId) || null;

    useEffect(() => {
        setPicked([]);
        if (!device || device.mode !== "stock") {
            setDoctor(null);
            return;
        }
        let cancel = false;
        DoctorIPod(device.id, "report").then((report) => {
            if (!cancel) setDoctor(report as DoctorReport);
        }).catch(() => {
            if (!cancel) setDoctor(null);
        });
        return () => {
            cancel = true;
        };
    }, [device?.id, device?.trackCount, device?.mode]);

    const refreshDoctor = async (id: string) => {
        try {
            setDoctor((await DoctorIPod(id, "report")) as DoctorReport);
        }
        catch {
            setDoctor(null);
        }
        await load();
    };

    const send = async (paths: string[]) => {
        if (!device || paths.length === 0) return;
        setBusy(true);
        setProgress({ phase: "copy", current: 0, total: paths.length, name: "" });
        try {
            const result = await SendToIPod(device.id, paths, format);
            const sent = Number(result?.sent || 0);
            const skipped = Number(result?.skipped || 0);
            toast.success(`${t("translation.devices.sent", { count: sent })}${skipped ? ` · ${t("translation.devices.skipped", { count: skipped })}` : ""}`);
            await refreshDoctor(device.id);
        }
        catch (error) {
            setProgress(null);
            toast.error(errorText(error));
        }
        finally {
            setBusy(false);
        }
    };

    const onSendFiles = async () => {
        try {
            const paths = await SelectAudioFiles();
            await send(paths || []);
        }
        catch (error) {
            if (errorText(error)) toast.error(errorText(error));
        }
    };

    const onSendFolder = async () => {
        try {
            const folder = await SelectFolder("");
            if (folder) await send([folder]);
        }
        catch (error) {
            if (errorText(error)) toast.error(errorText(error));
        }
    };

    const onEject = async () => {
        if (!device) return;
        try {
            await EjectIPod(device.id);
        }
        catch (error) {
            toast.error(t("translation.devices.ejectFailed"));
            console.error(error);
        }
    };

    const onRemove = async () => {
        if (!device || picked.length === 0) return;
        setConfirm(null);
        setBusy(true);
        try {
            await RemoveFromIPod(device.id, picked);
            setPicked([]);
            await refreshDoctor(device.id);
        }
        catch (error) {
            toast.error(errorText(error));
        }
        finally {
            setBusy(false);
        }
    };

    const onDoctor = async (action: "add-orphans" | "drop-missing" | "rebuild") => {
        if (!device) return;
        setConfirm(null);
        setBusy(true);
        try {
            await DoctorIPod(device.id, action);
            toast.success(t("translation.devices.doctorOk"));
            await refreshDoctor(device.id);
        }
        catch (error) {
            toast.error(errorText(error));
        }
        finally {
            setBusy(false);
        }
    };

    const tracks = device?.tracks || [];
    const used = device && device.totalBytes > 0 ? Math.max(0, Math.min(100, ((device.totalBytes - device.freeBytes) / device.totalBytes) * 100)) : 0;
    const warningKey = device?.warning ? WARNINGS[device.warning] : "";
    const modeKey = device ? MODES[device.mode] : "";

    return (
        <div className="mx-auto flex max-w-3xl flex-col gap-6">
            <header className="flex flex-col gap-1">
                <h1 className="text-lg font-semibold tracking-tight">{t("translation.devices.title")}</h1>
                <p className="max-w-2xl text-sm text-muted-foreground">{t("translation.devices.intro")}</p>
            </header>

            {devices.length === 0 ? (
                <div className="border-t py-8">
                    <p className="text-sm font-medium">{t("translation.devices.empty")}</p>
                    <p className="mt-1 max-w-xl text-sm text-muted-foreground">{t("translation.devices.emptyHint")}</p>
                </div>
            ) : (
                <>
                    {devices.length > 1 ? (
                        <div className="flex flex-wrap gap-3 border-b">
                            {devices.map((item) => (
                                <button
                                    key={item.id}
                                    type="button"
                                    onClick={() => setSelectedId(item.id)}
                                    className={cn(
                                        "cursor-pointer pb-2 text-sm",
                                        item.id === device?.id ? "font-semibold text-primary" : "text-muted-foreground hover:text-foreground",
                                    )}
                                >
                                    {item.name}
                                </button>
                            ))}
                        </div>
                    ) : null}

                    {device ? (
                        <section className="flex flex-col gap-4">
                            <div>
                                <h2 className="text-base font-semibold">{device.name}</h2>
                                <p className="mt-1 text-sm text-muted-foreground">
                                    {modeKey ? t(modeKey) : device.mode}
                                    {device.checksum && device.checksum !== "unknown" ? ` · ${device.checksum}` : ""}
                                </p>
                                {device.totalBytes > 0 ? (
                                    <>
                                        <p className="mt-3 font-mono text-xs tabular-nums text-muted-foreground">
                                            {t("translation.devices.free", { free: formatBytes(device.freeBytes), total: formatBytes(device.totalBytes) })}
                                        </p>
                                        <div className="mt-2 h-px w-full max-w-sm bg-border">
                                            <div className="h-px bg-primary" style={{ width: `${used}%` }} />
                                        </div>
                                    </>
                                ) : null}
                                {warningKey ? <p className="mt-3 max-w-2xl text-sm">{t(warningKey)}</p> : null}
                                {device.hasRockbox && device.mode === "stock" ? (
                                    <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{t("translation.devices.dualBoot")}</p>
                                ) : null}
                                {device.mode === "stock" || device.canSend ? (
                                    <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{t("translation.devices.covers")}</p>
                                ) : null}
                            </div>

                            <div className="flex flex-wrap items-center gap-2">
                                <button
                                    type="button"
                                    disabled={!device.canSend || busy}
                                    onClick={() => setFormat("alac")}
                                    className={cn("cursor-pointer px-1 text-sm disabled:cursor-default disabled:opacity-40", format === "alac" ? "font-semibold text-primary underline decoration-1 underline-offset-4" : "text-muted-foreground")}
                                >
                                    {t("translation.devices.alac")}
                                </button>
                                <button
                                    type="button"
                                    disabled={!device.canSend || busy}
                                    onClick={() => setFormat("aac")}
                                    className={cn("cursor-pointer px-1 text-sm disabled:cursor-default disabled:opacity-40", format === "aac" ? "font-semibold text-primary underline decoration-1 underline-offset-4" : "text-muted-foreground")}
                                >
                                    {t("translation.devices.aac")}
                                </button>
                                <Button type="button" size="sm" variant="outline" disabled={!device.canSend || busy} onClick={() => void onSendFiles()}>
                                    {t("translation.devices.sendFiles")}
                                </Button>
                                <Button type="button" size="sm" variant="outline" disabled={!device.canSend || busy} onClick={() => void onSendFolder()}>
                                    {t("translation.devices.sendFolder")}
                                </Button>
                                <Button type="button" size="sm" variant="outline" disabled={picked.length === 0 || busy || device.mode !== "stock"} onClick={() => setConfirm("remove")}>
                                    {t("translation.devices.remove")}
                                </Button>
                                <Button type="button" size="sm" variant="ghost" disabled={!device.mount || busy} onClick={() => void onEject()}>
                                    {t("translation.devices.eject")}
                                </Button>
                                {busy ? (
                                    <button type="button" className="cursor-pointer text-sm text-muted-foreground underline-offset-4 hover:underline" onClick={() => void CancelIPod()}>
                                        {t("translation.common.cancel")}
                                    </button>
                                ) : null}
                            </div>
                            <p className="text-sm text-muted-foreground">{t("translation.devices.copyNote")}</p>
                            {progress ? (
                                <p className="font-mono text-xs tabular-nums text-muted-foreground">
                                    {progress.phase === "database" ? t("translation.devices.writing") : t("translation.devices.sending")}
                                    {progress.total ? ` ${progress.current}/${progress.total}` : ""}
                                    {progress.name ? ` · ${progress.name}` : ""}
                                </p>
                            ) : null}

                            <div>
                                <div className="flex items-baseline justify-between border-b py-2">
                                    <h3 className="text-sm font-medium">{t("translation.devices.tracks")}</h3>
                                    <span className="font-mono text-xs tabular-nums text-muted-foreground">{device.trackCount || tracks.length}</span>
                                </div>
                                {tracks.length === 0 ? (
                                    <p className="py-4 text-sm text-muted-foreground">{t("translation.devices.noTracks")}</p>
                                ) : (
                                    <ul>
                                        {tracks.map((track) => {
                                            const checked = picked.includes(track.id);
                                            return (
                                                <li key={`${track.id}-${track.path}`} className="flex items-center gap-3 border-b py-2">
                                                    {device.mode === "stock" && track.id ? (
                                                        <Checkbox
                                                            checked={checked}
                                                            onCheckedChange={(value) => {
                                                                setPicked((current) => value === true ? [...current, track.id] : current.filter((id) => id !== track.id));
                                                            }}
                                                            aria-label={track.title}
                                                        />
                                                    ) : <span className="size-4" />}
                                                    <span className="min-w-0 flex-1 truncate text-sm">{track.title || track.path}</span>
                                                    <span className="hidden min-w-0 flex-1 truncate text-sm text-muted-foreground sm:block">{track.artist}</span>
                                                    <span className="hidden min-w-0 flex-1 truncate text-sm text-muted-foreground md:block">{track.album}</span>
                                                </li>
                                            );
                                        })}
                                    </ul>
                                )}
                            </div>

                            {device.mode === "stock" ? (
                                <div className="border-t pt-4">
                                    <h3 className="text-sm font-medium">{t("translation.devices.doctor")}</h3>
                                    <p className="mt-1 text-sm text-muted-foreground">{t("translation.devices.backup")}</p>
                                    <p className="mt-3 text-sm">
                                        {t("translation.devices.orphans")}: <span className="font-mono tabular-nums">{doctor?.orphans?.length || 0}</span>
                                    </p>
                                    <p className="text-sm">
                                        {t("translation.devices.missing")}: <span className="font-mono tabular-nums">{doctor?.missing?.length || 0}</span>
                                    </p>
                                    <div className="mt-3 flex flex-wrap gap-2">
                                        <Button type="button" size="sm" variant="outline" disabled={busy || !doctor?.orphans?.length} onClick={() => void onDoctor("add-orphans")}>
                                            {t("translation.devices.addOrphans")}
                                        </Button>
                                        <Button type="button" size="sm" variant="outline" disabled={busy || !doctor?.missing?.length} onClick={() => void onDoctor("drop-missing")}>
                                            {t("translation.devices.dropMissing")}
                                        </Button>
                                        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setConfirm("rebuild")}>
                                            {t("translation.devices.rebuild")}
                                        </Button>
                                    </div>
                                </div>
                            ) : null}
                        </section>
                    ) : null}
                </>
            )}

            <Dialog open={confirm !== null} onOpenChange={(open) => { if (!open) setConfirm(null); }}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>
                            {confirm === "rebuild" ? t("translation.devices.rebuild") : t("translation.devices.remove")}
                        </DialogTitle>
                        <DialogDescription>
                            {confirm === "rebuild" ? t("translation.devices.confirmRebuild") : t("translation.devices.confirmRemove")}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button type="button" variant="outline" onClick={() => setConfirm(null)}>{t("translation.common.cancel")}</Button>
                        <Button type="button" onClick={() => void (confirm === "rebuild" ? onDoctor("rebuild") : onRemove())}>
                            {confirm === "rebuild" ? t("translation.devices.rebuild") : t("translation.devices.remove")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
