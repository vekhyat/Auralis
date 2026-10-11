import assert from "node:assert/strict";
import { test } from "node:test";
import { LOSSY_QUALITIES, isDownloadQuality, isLossyQuality, lossyConversionTarget, lossyKbps } from "./quality.ts";

test("stored quality values are accepted only when the picker offers them", () => {
    for (const value of ["16", "24", "atmos", ...LOSSY_QUALITIES]) {
        assert.ok(isDownloadQuality(value), value);
    }
    for (const value of ["", "27", "lossy-64", "LOSSLESS", 16, null, undefined]) {
        assert.equal(isDownloadQuality(value), false, String(value));
    }
});

test("lossless tiers never trigger an encode", () => {
    for (const quality of ["16", "24", "atmos"] as const) {
        assert.equal(isLossyQuality(quality), false);
        assert.equal(lossyConversionTarget(quality), null);
    }
});

test("each smaller-file tier encodes at its own bitrate into a format ffmpeg can write", () => {
    assert.deepEqual(LOSSY_QUALITIES.map(lossyKbps), [320, 256, 192, 128]);
    for (const quality of LOSSY_QUALITIES) {
        const target = lossyConversionTarget(quality);
        assert.ok(target, quality);
        assert.equal(target.bitrate, `${lossyKbps(quality)}k`);
        assert.ok(["mp3", "m4a-aac", "opus"].includes(target.format), `${quality} -> ${target.format}`);
    }
});
