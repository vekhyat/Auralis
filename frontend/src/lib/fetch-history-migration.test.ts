import assert from "node:assert/strict";
import { test } from "node:test";
import { planLegacyHistoryMigration, shouldDiscardLegacyHistory, } from "./fetch-history-migration.ts";

function ids(items: unknown[]): string[] {
    return items.map((item) => {
        const record = item as { id?: string };
        return record.id ?? "";
    });
}

test("legacy storage is removed only after a confirmed save of a parsed merge", () => {
    const seen: unknown[][] = [];
    const plan = planLegacyHistoryMigration({
        legacyRaw: JSON.stringify([{ id: "legacy", timestamp: 2 }]),
        persistedRaw: JSON.stringify([{ id: "backend", timestamp: 1 }]),
        persistedReadOk: true,
        normalize: (items) => {
            seen.push(items);
            return ids(items);
        },
    });
    assert.deepEqual(seen, [[{ id: "backend", timestamp: 1 }, { id: "legacy", timestamp: 2 }]]);
    assert.deepEqual(plan.items, ["backend", "legacy"]);
    assert.deepEqual(plan.saveItems, ["backend", "legacy"]);
    assert.equal(shouldDiscardLegacyHistory({ ...plan, saveConfirmed: false }), false);
    assert.equal(shouldDiscardLegacyHistory({ ...plan, saveConfirmed: true }), true);
});

test("a failed backend read keeps legacy data and does not save", () => {
    const plan = planLegacyHistoryMigration({
        legacyRaw: JSON.stringify([{ id: "legacy" }]),
        persistedRaw: null,
        persistedReadOk: false,
        normalize: ids,
    });
    assert.deepEqual(plan.items, ["legacy"]);
    assert.equal(plan.saveItems, null);
    assert.equal(shouldDiscardLegacyHistory({ ...plan, saveConfirmed: true }), false);
});

test("corrupt legacy or backend payloads are not deleted or overwritten", () => {
    const corruptLegacy = planLegacyHistoryMigration({
        legacyRaw: "{",
        persistedRaw: JSON.stringify([{ id: "backend" }]),
        persistedReadOk: true,
        normalize: ids,
    });
    assert.deepEqual(corruptLegacy.items, ["backend"]);
    assert.equal(corruptLegacy.saveItems, null);
    assert.equal(shouldDiscardLegacyHistory({ ...corruptLegacy, saveConfirmed: true }), false);

    const corruptBackend = planLegacyHistoryMigration({
        legacyRaw: JSON.stringify([{ id: "legacy" }]),
        persistedRaw: "{\"id\":1}",
        persistedReadOk: true,
        normalize: ids,
    });
    assert.deepEqual(corruptBackend.items, ["legacy"]);
    assert.equal(corruptBackend.saveItems, null);
    assert.equal(shouldDiscardLegacyHistory({ ...corruptBackend, saveConfirmed: true }), false);
});

test("missing legacy storage can be normalized without a removal", () => {
    const plan = planLegacyHistoryMigration({
        legacyRaw: null,
        persistedRaw: "[]",
        persistedReadOk: true,
        normalize: ids,
    });
    assert.deepEqual(plan.items, []);
    assert.deepEqual(plan.saveItems, []);
    assert.equal(plan.hadLegacy, false);
    assert.equal(shouldDiscardLegacyHistory({ ...plan, saveConfirmed: true }), false);
});
