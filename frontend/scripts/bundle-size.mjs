import { readFileSync } from "node:fs";
import { resolve, sep } from "node:path";
import { gzipSync } from "node:zlib";

const directory = resolve(process.argv[2] ?? "dist");
const html = readFileSync(resolve(directory, "index.html"), "utf8");
const files = [...new Set([...html.matchAll(/(?:src|href)="([^"?]+\.js)"/g)]
  .map((match) => match[1]))];
let bytes = 0;
let gzipBytes = 0;
for (const file of files) {
  const path = resolve(directory, file.replace(/^\//, ""));
  if (!path.startsWith(`${directory}${sep}`)) throw new Error("Asset is outside the build directory.");
  const data = readFileSync(path);
  bytes += data.length;
  gzipBytes += gzipSync(data).length;
}
console.log(JSON.stringify({ initialJavaScriptFiles: files.length, bytes, gzipBytes }, null, 2));
