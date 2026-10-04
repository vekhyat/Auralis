export interface CommunitySource {
    id: string;
    name: string;
    service: string;
    protocol: "hifi" | "qobuz-rest" | "qobuz-dl" | "dab" | "lucida" | "subsonic";
    base_url: string;
    enabled: boolean;
    credential_env?: string;
    credential_type?: "" | "api_key" | "bearer" | "cookie" | "subsonic";
}

export interface CommunitySourceCheck {
    id: string;
    state: string;
    message: string;
    latency_ms: number;
    audio_verified: boolean;
    checked_at: string;
}

interface SourceBridge {
    ListCommunitySources: () => Promise<CommunitySource[]>;
    SaveCommunitySources: (sources: CommunitySource[]) => Promise<void>;
    GetCommunitySourceChecks: () => Promise<CommunitySourceCheck[]>;
    CheckCommunitySource: (id: string) => Promise<CommunitySourceCheck>;
    TestCommunitySourceDownload: (id: string) => Promise<CommunitySourceCheck>;
    VerifyCommunitySource: (id: string) => Promise<CommunitySourceCheck>;
}

export function communitySourceBridge(): SourceBridge {
    const bridge = (window as Window & { go?: { main?: { App?: Partial<SourceBridge> } } }).go?.main?.App;
    if (!bridge?.ListCommunitySources || !bridge.SaveCommunitySources || !bridge.GetCommunitySourceChecks || !bridge.CheckCommunitySource || !bridge.TestCommunitySourceDownload || !bridge.VerifyCommunitySource) {
        throw new Error("Community source controls are unavailable. Open the latest Auralis desktop build.");
    }
    return bridge as SourceBridge;
}
