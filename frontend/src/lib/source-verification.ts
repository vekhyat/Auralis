export interface SourceVerification {
    id: number;
    active: boolean;
    title: string;
    host: string;
    ready: boolean;
    error?: string;
    can_confirm: boolean;
}

export interface SourceConnection {
    id: string;
    name: string;
    service: string;
    role: string;
    state: string;
    message: string;
    can_verify: boolean;
    checked_at?: string;
}

interface SourceVerificationBridge {
    GetSourceVerification: () => Promise<SourceVerification>;
    SetSourceVerificationViewport: (id: number, left: number, top: number, width: number, height: number) => Promise<void>;
    CancelSourceVerification: (id: number) => Promise<void>;
    ConfirmSourceVerification: () => Promise<boolean>;
    GetSourceConnections: () => Promise<SourceConnection[]>;
    CheckSourceConnection: (id: string) => Promise<SourceConnection>;
    VerifySourceConnection: (id: string) => Promise<SourceConnection>;
}

export function sourceVerificationBridge(): SourceVerificationBridge {
    const bridge = (window as Window & { go?: { main?: { App?: Partial<SourceVerificationBridge> } } }).go?.main?.App;
    if (!bridge?.GetSourceVerification || !bridge.SetSourceVerificationViewport || !bridge.CancelSourceVerification || !bridge.ConfirmSourceVerification || !bridge.GetSourceConnections || !bridge.CheckSourceConnection || !bridge.VerifySourceConnection) {
        throw new Error("Source verification is unavailable. Open the latest Auralis desktop build.");
    }
    return bridge as SourceVerificationBridge;
}

export function isSourceVerification(value: unknown): value is SourceVerification {
    if (!value || typeof value !== "object") return false;
    const row = value as Partial<SourceVerification>;
    return Number.isSafeInteger(row.id) && (row.id ?? -1) >= 0 && typeof row.active === "boolean" && typeof row.host === "string" && typeof row.ready === "boolean" && typeof row.can_confirm === "boolean";
}
