import assert from "node:assert/strict";
import { test } from "node:test";
import { existsSync } from "node:fs";
import { registerHooks } from "node:module";

const hooks = registerHooks({
  resolve(specifier, context, next) {
    if (specifier.startsWith(".") && context.parentURL) {
      for (const extension of [".ts", ".js"]) {
        const candidate = new URL(`${specifier}${extension}`, context.parentURL);
        if (existsSync(candidate)) return next(candidate.href, context);
      }
    }
    return next(specifier, context);
  },
});

let instance = 0;
async function storageFixture(t, payload, legacy, loadError) {
  const values = new Map(legacy === undefined ? [] : [["auralis_download_queue", legacy]]);
  const writes = [];
  const previousWindow = globalThis.window;
  const previousStorage = globalThis.localStorage;
  globalThis.localStorage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
  globalThis.window = { go: { main: { App: {
    LoadPersistentDownloadQueue: async () => { if (loadError) throw loadError; return payload; },
    ReplacePersistentDownloadQueue: async (value) => { writes.push(value); },
    ApplyPersistentDownloadQueueChanges: async (...args) => { writes.push(args); },
  } } } };
  t.after(() => { globalThis.window = previousWindow; globalThis.localStorage = previousStorage; });
  const queue = await import(`../src/lib/queue.ts?storage-test=${++instance}`);
  await queue.initializeQueuePersistence();
  return { queue, values, writes };
}

test("a failed database read never replaces saved queue data", async (t) => {
  const fixture = await storageFixture(t, "", "[]", new Error("database unavailable"));
  assert.deepEqual(fixture.writes, []);
  assert.equal(fixture.values.get("auralis_download_queue"), "[]");
  assert.equal(fixture.queue.getQueuePersistenceState().sealed, true);
});

test("corrupt legacy queue data survives an empty database migration", async (t) => {
  const fixture = await storageFixture(t, "", "broken legacy payload");
  assert.deepEqual(fixture.writes, []);
  assert.equal(fixture.values.get("auralis_download_queue"), "broken legacy payload");
  assert.equal(fixture.queue.getQueuePersistenceState().sealed, true);
});

test("a valid legacy queue is removed only after its database save succeeds", async (t) => {
  const fixture = await storageFixture(t, "", "[]");
  assert.deepEqual(fixture.writes, ["[]"]);
  assert.equal(fixture.values.has("auralis_download_queue"), false);
  assert.equal(await fixture.queue.flushQueuePersistence(), true);
});

test.after(() => hooks.deregister());
