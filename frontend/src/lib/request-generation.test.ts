import assert from "node:assert/strict";
import { test } from "node:test";
import { adoptCorrelatedStreamBegin, adoptStreamBegin, allowsRequestOutcome, armRequestStream, bindRequestId, claimRequest, createRequestClock, releaseRequest, retireRequest, streamChunkBelongs, } from "./request-generation.ts";

test("a later claim retires album confirmation and artist search on every settle phase", () => {
    let clock = createRequestClock();
    const album = claimRequest(clock);
    clock = album.clock;
    const artist = claimRequest(clock);
    clock = artist.clock;
    for (const outcome of ["success", "error", "finally"] as const) {
        assert.equal(allowsRequestOutcome(clock, album.generation, outcome), false);
        assert.equal(allowsRequestOutcome(clock, artist.generation, outcome), true);
    }
    const fetch = claimRequest(clock);
    clock = fetch.clock;
    assert.equal(allowsRequestOutcome(clock, artist.generation, "finally"), false);
    assert.equal(allowsRequestOutcome(clock, artist.generation, "error"), false);
    assert.equal(allowsRequestOutcome(clock, fetch.generation, "success"), true);
    assert.equal(allowsRequestOutcome(clock, fetch.generation, "finally"), true);
});

test("navigation retires success, error, finally, and any stream already begun", () => {
    let clock = createRequestClock();
    const claimed = claimRequest(clock);
    clock = claimed.clock;
    clock = adoptStreamBegin(clock, 4);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "stream", 4), true);
    clock = retireRequest(clock);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "success"), false);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "error"), false);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "finally"), false);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "stream", 4), false);
    const ignored = adoptStreamBegin(clock, 4);
    assert.equal(ignored, clock);
    assert.equal(allowsRequestOutcome(ignored, claimed.generation, "stream", 4), false);
});

test("stream payloads apply only after begin for the claim that is still current", () => {
    let clock = createRequestClock();
    const first = claimRequest(clock);
    clock = first.clock;
    assert.equal(allowsRequestOutcome(clock, first.generation, "stream", 7), false);
    clock = adoptStreamBegin(clock, Number.NaN);
    assert.equal(allowsRequestOutcome(clock, first.generation, "stream", 7), false);
    clock = adoptStreamBegin(clock, 7);
    assert.equal(allowsRequestOutcome(clock, first.generation, "stream", 6), false);
    assert.equal(allowsRequestOutcome(clock, first.generation, "stream", 7), true);
    const second = claimRequest(clock);
    clock = second.clock;
    assert.equal(allowsRequestOutcome(clock, first.generation, "stream", 7), false);
    assert.equal(allowsRequestOutcome(clock, second.generation, "stream", 7), false);
    clock = adoptStreamBegin(clock, 8);
    assert.equal(allowsRequestOutcome(clock, second.generation, "stream", 8), true);
    assert.equal(allowsRequestOutcome(clock, second.generation, "success"), true);
    const released = releaseRequest(clock, second.generation);
    assert.equal(allowsRequestOutcome(released, second.generation, "stream", 8), false);
    assert.equal(allowsRequestOutcome(released, second.generation, "finally"), true);
    assert.equal(releaseRequest(released, first.generation), released);
});

test("arming a fetch drops a stream adopted while the url was still unresolved", () => {
    let clock = createRequestClock();
    const search = claimRequest(clock);
    clock = search.clock;
    clock = adoptStreamBegin(clock, 4);
    assert.equal(allowsRequestOutcome(clock, search.generation, "stream", 4), true);
    clock = armRequestStream(clock, search.generation);
    assert.equal(allowsRequestOutcome(clock, search.generation, "stream", 4), false);
    assert.equal(allowsRequestOutcome(clock, search.generation, "success"), true);
    assert.equal(allowsRequestOutcome(clock, search.generation, "finally"), true);
    clock = adoptStreamBegin(clock, 9);
    assert.equal(allowsRequestOutcome(clock, search.generation, "stream", 4), false);
    assert.equal(allowsRequestOutcome(clock, search.generation, "stream", 9), true);
    assert.equal(armRequestStream(clock, search.generation - 1), clock);
});

test("a labeled claim ignores bare and foreign begins, then takes only its own stream", () => {
    let clock = createRequestClock();
    const claimed = claimRequest(clock);
    clock = bindRequestId(claimed.clock, claimed.generation, "req-current");
    clock = adoptCorrelatedStreamBegin(clock, 4);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "stream", 4), false);
    clock = adoptCorrelatedStreamBegin(clock, { id: 4, request_id: "req-old" });
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "stream", 4), false);
    assert.equal(streamChunkBelongs(clock, claimed.generation, { id: 4, payload: ["stale"] }), null);
    clock = adoptCorrelatedStreamBegin(clock, { id: 9, requestId: "req-current" });
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "stream", 9), true);
    const chunk = streamChunkBelongs(clock, claimed.generation, { id: 9, request_id: "req-current", payload: { name: "live" } });
    assert.equal(chunk?.payload && typeof chunk.payload === "object" && "name" in chunk.payload, true);
    assert.equal(streamChunkBelongs(clock, claimed.generation, { id: 9, request_id: "req-old", payload: { name: "other" } }), null);
    const replaced = adoptCorrelatedStreamBegin(clock, { id: 10, request_id: "req-other" });
    assert.equal(replaced, clock);
    assert.equal(allowsRequestOutcome(clock, claimed.generation, "success"), true);
    const unlabeled = createRequestClock();
    const plain = claimRequest(unlabeled);
    const rejectedLabel = adoptCorrelatedStreamBegin(plain.clock, { id: 3, request_id: "req-current" });
    assert.equal(rejectedLabel, plain.clock);
    assert.equal(bindRequestId(clock, claimed.generation - 1, "req-other"), clock);
});
