import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const version = process.argv[2]?.replace(/^v/, "");
if (!version || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
  throw new Error("Expected a release version such as v1.2.3 or v1.2.3-beta.1.");
}
const configPath = fileURLToPath(new URL("../../wails.json", import.meta.url));
const config = JSON.parse(readFileSync(configPath, "utf8"));
config.info.productVersion = version;
writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
console.log(`Building Auralis ${version}`);
