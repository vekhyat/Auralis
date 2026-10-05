import assert from "node:assert/strict";
import { test } from "node:test";
import { normalizeSelection } from "./validation.ts";

test("normalizeSelection defaults whole_library to true and empty lists", () => {
    const sel = normalizeSelection();
    assert.equal(sel.whole_library, true);
    assert.deepEqual(sel.artists, []);
    assert.deepEqual(sel.albums, []);
    assert.deepEqual(sel.playlists, []);
    assert.equal(sel.recent_days, 0);
});

test("normalizeSelection trims strings, filters empties, and clamps days", () => {
    const raw = {
        whole_library: false,
        artists: ["  Radiohead  ", "", "Daft Punk"],
        albums: ["Discovery", " "],
        playlists: ["/music/fav.m3u8"],
        recent_days: 99999,
    };
    const norm = normalizeSelection(raw);
    assert.equal(norm.whole_library, false);
    assert.deepEqual(norm.artists, ["Radiohead", "Daft Punk"]);
    assert.deepEqual(norm.albums, ["Discovery"]);
    assert.deepEqual(norm.playlists, ["/music/fav.m3u8"]);
    assert.equal(norm.recent_days, 36500); // Clamped
});
