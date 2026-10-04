import { readdirSync, existsSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
function collect(directory) {
  if (!existsSync(new URL(`../${directory}`, import.meta.url))) return [];
  return readdirSync(new URL(`../${directory}`, import.meta.url), { withFileTypes: true })
    .flatMap((entry) => entry.isDirectory()
      ? collect(`${directory}/${entry.name}`)
      : /\.test\.(?:ts|mjs|js)$/.test(entry.name) ? [`${directory}/${entry.name}`] : []);
}
const files = [...collect("tests"), ...collect("src")].sort();
if (files.length === 0) {
  throw new Error("No frontend behavior tests were found.");
}
const result = spawnSync(process.execPath, ["--test", ...files], {
  cwd: root,
  stdio: "inherit",
});
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;
