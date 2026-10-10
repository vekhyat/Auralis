import assert from "node:assert/strict";
import { test } from "node:test";
import { DEFAULT_AUTO_ORDER, DEFAULT_AUTO_SERVICES, extendAutoServices, isSupportedAutoService, rankDownloadServices, sanitizeAutoServices, } from "./source-priority.ts";

type BridgeWindow = {
    window: {
        go?: { main?: { App?: { RankDownloadServices?: (services: string[], quality: string) => Promise<string[]> } } };
    };
};

function setBridge(bridge?: (services: string[], quality: string) => Promise<string[]>): void {
    (globalThis as unknown as BridgeWindow).window = bridge ? { go: { main: { App: { RankDownloadServices: bridge } } } } : {};
}

test("legacy saved orders cannot reintroduce retired services", () => {
    assert.deepEqual(sanitizeAutoServices(["tidal", "qobuz", "amazon", "deezer", "apple", "jiosaavn"]), ["qobuz", "deezer", "apple", "jiosaavn"]);
    assert.deepEqual(sanitizeAutoServices("tidal-qobuz-amazon"), ["qobuz"]);
    assert.deepEqual(sanitizeAutoServices("amazon-tidal"), []);
    assert.deepEqual(extendAutoServices("tidal-qobuz-amazon"), [...DEFAULT_AUTO_SERVICES]);
    assert.deepEqual(extendAutoServices("qobuz-tidal"), [...DEFAULT_AUTO_SERVICES]);
    assert.equal(DEFAULT_AUTO_ORDER, "qobuz-deezer-apple-jiosaavn");
});

test("extended orders keep every verified route exactly once", () => {
    assert.deepEqual(extendAutoServices("apple-qobuz"), ["apple", "qobuz", "deezer", "jiosaavn"]);
    assert.deepEqual(extendAutoServices(["jiosaavn", "deezer"]), ["jiosaavn", "deezer", "qobuz", "apple"]);
});

test("an unavailable bridge falls back to the vetted order with AAC last", async () => {
    setBridge(undefined);
    assert.deepEqual(await rankDownloadServices(["tidal", "qobuz", "amazon", "deezer", "apple", "jiosaavn"], "16"), ["qobuz", "deezer", "apple", "jiosaavn"]);
    assert.deepEqual(await rankDownloadServices(["tidal", "amazon"], "24"), [...DEFAULT_AUTO_SERVICES]);
    assert.deepEqual(await rankDownloadServices(["jiosaavn", "qobuz"], "atmos"), ["qobuz", "jiosaavn"]);
    assert.deepEqual(await rankDownloadServices(["jiosaavn", "qobuz"], "lossy"), ["jiosaavn", "qobuz"]);
});

test("a rejecting bridge cannot restore retired services", async () => {
    setBridge(async () => { throw new Error("bridge unavailable"); });
    assert.deepEqual(await rankDownloadServices(["tidal", "qobuz", "amazon"], "16"), ["qobuz"]);
});

test("a stale bridge response containing retired services is rejected", async () => {
    setBridge(async (services) => [...services, "tidal", "amazon"]);
    assert.deepEqual(await rankDownloadServices(["qobuz", "deezer"], "16"), ["qobuz", "deezer"]);
});

test("a valid bridge permutation of vetted services is honored", async () => {
    setBridge(async (services) => [...services].reverse());
    assert.deepEqual(await rankDownloadServices(["qobuz", "deezer", "apple", "jiosaavn"], "16"), ["apple", "deezer", "qobuz", "jiosaavn"]);
});

test("a saved single store is honoured only while it passes full-audio checks", () => {
    for (const service of ["qobuz", "deezer", "apple", "jiosaavn"]) {
        assert.ok(isSupportedAutoService(service), service);
    }
    for (const service of ["tidal", "amazon", "auto", ""]) {
        assert.equal(isSupportedAutoService(service), false, service);
    }
});
