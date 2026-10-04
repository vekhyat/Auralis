/**
 * One clock for metadata fetches. A claim at action start owns success,
 * error, and finally. Navigation retires that claim. Stream payloads apply
 * only after a begin event for the same claim. Once a claim has a client
 * request id, a begin must carry that same id.
 */
export interface RequestClock {
    current: number;
    acceptingStream: boolean;
    streamId: number | null;
    streamGeneration: number | null;
    requestId: string | null;
}

export type RequestOutcome = "success" | "error" | "finally" | "stream";

export function createRequestClock(): RequestClock {
    return {
        current: 0,
        acceptingStream: false,
        streamId: null,
        streamGeneration: null,
        requestId: null,
    };
}

export function claimRequest(clock: RequestClock): { clock: RequestClock; generation: number } {
    const generation = clock.current + 1;
    return {
        generation,
        clock: {
            current: generation,
            acceptingStream: true,
            streamId: null,
            streamGeneration: null,
            requestId: null,
        },
    };
}

/** Attach the id that this claim will send with the metadata request. */
export function bindRequestId(clock: RequestClock, generation: number, requestId: string): RequestClock {
    if (clock.current !== generation) {
        return clock;
    }
    const trimmed = requestId.trim();
    if (!trimmed) {
        return clock;
    }
    return { ...clock, requestId: trimmed };
}

/**
 * The claim is still the latest action, but it will not take more stream
 * payloads. Success and finally for this generation stay allowed.
 */
export function releaseRequest(clock: RequestClock, generation: number): RequestClock {
    if (clock.current !== generation) {
        return clock;
    }
    return {
        ...clock,
        acceptingStream: false,
        streamId: null,
        streamGeneration: null,
        requestId: null,
    };
}

/** Back, forward, cache load, and clear. In-flight claims no longer apply. */
export function retireRequest(clock: RequestClock): RequestClock {
    return {
        current: clock.current + 1,
        acceptingStream: false,
        streamId: null,
        streamGeneration: null,
        requestId: null,
    };
}

/** Drop a stream adopted while a URL was still being resolved. */
export function armRequestStream(clock: RequestClock, generation: number): RequestClock {
    if (clock.current !== generation) {
        return clock;
    }
    return {
        ...clock,
        acceptingStream: true,
        streamId: null,
        streamGeneration: null,
    };
}

export function adoptStreamBegin(clock: RequestClock, streamId: number): RequestClock {
    if (!clock.acceptingStream || !Number.isFinite(streamId)) {
        return clock;
    }
    return {
        ...clock,
        streamId,
        streamGeneration: clock.current,
    };
}

export interface StreamBegin {
    streamId: number;
    requestId: string | null;
}

function readFiniteId(value: unknown): number | null {
    const streamId = typeof value === "number" ? value : typeof value === "string" && value.trim() !== "" ? Number(value) : Number.NaN;
    return Number.isFinite(streamId) ? streamId : null;
}

function readRequestId(value: unknown): string | null {
    return typeof value === "string" && value.trim() !== "" ? value.trim() : null;
}

/** Begin events are a bare stream id, or `{ id, request_id }` once the backend echoes the client id. */
export function readStreamBegin(event: unknown): StreamBegin | null {
    const bareId = readFiniteId(event);
    if (bareId !== null) {
        return { streamId: bareId, requestId: null };
    }
    if (!event || typeof event !== "object") {
        return null;
    }
    const record = event as Record<string, unknown>;
    const streamId = readFiniteId(record.id ?? record.stream_id);
    if (streamId === null) {
        return null;
    }
    return {
        streamId,
        requestId: readRequestId(record.request_id ?? record.requestId),
    };
}

/**
 * A claim that sent a request id ignores every begin that does not echo it,
 * including a late bare id from an older read.
 */
export function adoptCorrelatedStreamBegin(clock: RequestClock, event: unknown): RequestClock {
    const begin = readStreamBegin(event);
    if (!begin) {
        return clock;
    }
    if (clock.requestId) {
        if (begin.requestId !== clock.requestId) {
            return clock;
        }
    }
    else if (begin.requestId) {
        return clock;
    }
    return adoptStreamBegin(clock, begin.streamId);
}

export interface StreamChunk {
    streamId: number;
    requestId: string | null;
    payload: unknown;
}

export function readStreamChunk(event: unknown): StreamChunk | null {
    if (!event || typeof event !== "object") {
        return null;
    }
    const record = event as Record<string, unknown>;
    if (!("payload" in record) || record.payload == null) {
        return null;
    }
    const streamId = readFiniteId(record.id ?? record.stream_id);
    if (streamId === null) {
        return null;
    }
    return {
        streamId,
        requestId: readRequestId(record.request_id ?? record.requestId),
        payload: record.payload,
    };
}

export function streamChunkBelongs(clock: RequestClock, generation: number, event: unknown): StreamChunk | null {
    const chunk = readStreamChunk(event);
    if (!chunk) {
        return null;
    }
    if (chunk.requestId && chunk.requestId !== clock.requestId) {
        return null;
    }
    if (!allowsRequestOutcome(clock, generation, "stream", chunk.streamId)) {
        return null;
    }
    return chunk;
}

export function allowsRequestOutcome(clock: RequestClock, generation: number, outcome: RequestOutcome, streamId?: number): boolean {
    if (clock.current !== generation) {
        return false;
    }
    if (outcome !== "stream") {
        return true;
    }
    return clock.acceptingStream
        && clock.streamGeneration === generation
        && clock.streamId !== null
        && streamId !== undefined
        && Number.isFinite(streamId)
        && clock.streamId === streamId;
}
