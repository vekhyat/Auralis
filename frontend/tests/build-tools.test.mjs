import assert from "node:assert/strict";
import { test } from "node:test";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

function fixture(testContext) {
  const temporaryRoot = realpathSync(tmpdir());
  const directory = realpathSync(mkdtempSync(join(temporaryRoot, "auralis-build-test-")));
  assert.equal(dirname(directory), temporaryRoot);
  testContext.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

test("release version updates preserve configuration and reject invalid tags", (t) => {
  const directory = fixture(t);
  const scriptDirectory = join(directory, "frontend", "scripts");
  mkdirSync(scriptDirectory, { recursive: true });
  const script = join(scriptDirectory, "set-release-version.mjs");
  copyFileSync(fileURLToPath(new URL("../scripts/set-release-version.mjs", import.meta.url)), script);
  const configPath = join(directory, "wails.json");
  writeFileSync(configPath, JSON.stringify({ name: "Auralis", info: { productVersion: "1.0.0", productName: "Auralis" } }));
  const accepted = spawnSync(process.execPath, [script, "v2.4.0-beta.1"], { encoding: "utf8" });
  assert.equal(accepted.status, 0, accepted.stderr);
  assert.deepEqual(JSON.parse(readFileSync(configPath, "utf8")), { name: "Auralis", info: { productVersion: "2.4.0-beta.1", productName: "Auralis" } });
  const rejected = spawnSync(process.execPath, [script, "v2.invalid"], { encoding: "utf8" });
  assert.equal(rejected.status, 1);
  assert.equal(JSON.parse(readFileSync(configPath, "utf8")).info.productVersion, "2.4.0-beta.1");
});

test("bundle report counts entry and shared preloads exactly once", (t) => {
  const directory = fixture(t);
  mkdirSync(join(directory, "assets"));
  writeFileSync(join(directory, "index.html"), '<script src="/assets/entry.js"></script><link rel="modulepreload" href="/assets/shared.js"><link href="/assets/shared.js"><link href="/assets/style.css">');
  writeFileSync(join(directory, "assets", "entry.js"), "one");
  writeFileSync(join(directory, "assets", "shared.js"), "two");
  const script = fileURLToPath(new URL("../scripts/bundle-size.mjs", import.meta.url));
  const result = spawnSync(process.execPath, [script, directory], { encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { initialJavaScriptFiles: 2, bytes: 6, gzipBytes: 46 });
});
