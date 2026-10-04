import assert from "node:assert/strict";
import { test } from "node:test";
import { PRIMARY_DESTINATIONS, isShellPage } from "./pages.ts";
import type { PageType } from "./pages.ts";

const HIDDEN_TOOL_PAGES = [
    "tools",
    "audio-analysis",
    "tempo-key-analyzer",
    "replaygain",
    "audio-converter",
    "audio-resampler",
    "file-manager",
    "lyrics-manager",
    "enrich",
] as const satisfies readonly PageType[];

test("titlebar destinations are library, queue, history, devices, and settings", () => {
    assert.deepEqual([...PRIMARY_DESTINATIONS], ["main", "queue", "history", "devices", "settings"]);
});

test("tool pages are not shell routes", () => {
    for (const page of HIDDEN_TOOL_PAGES) {
        assert.equal(isShellPage(page), false);
    }
    for (const page of ["main", "queue", "history", "devices", "settings", "debug"] as const) {
        assert.equal(isShellPage(page), true);
        assert.equal(PRIMARY_DESTINATIONS.includes(page as typeof PRIMARY_DESTINATIONS[number]) || page === "debug", true);
    }
});
