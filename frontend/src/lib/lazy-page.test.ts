import assert from "node:assert/strict";
import { test } from "node:test";
import { createLazyPage } from "./lazy-page.ts";

test("retry constructs a new lazy page instead of reusing a rejected one", () => {
    let calls = 0;
    const load = () => {
        calls += 1;
        return Promise.resolve({ default: function Page() { return null; } });
    };
    const first = createLazyPage(load);
    const second = createLazyPage(load);
    assert.notEqual(first, second);
    assert.equal(calls, 0);
});
