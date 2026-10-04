import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dir = join(dirname(fileURLToPath(import.meta.url)), "../src/locales");
const downloadEntries = [
    ["quality", "Quality"],
    ["qualityHint", "Choose the file for new downloads. If that quality is not available, the next best file is saved."],
    ["couldntGet", "Couldn't save this"],
    ["couldntSave", "This one did not save. Try again in a moment."],
    ["tryAgain", "Try again"],
    ["settingUp", "Setting up"],
    ["ffmpegNeed", "Auralis needs an audio tool before it can save files."],
];

for (const file of readdirSync(dir).filter((name) => name.endsWith(".json"))) {
    const path = join(dir, file);
    let text = readFileSync(path, "utf8");
    const nl = text.includes("\r\n") ? "\r\n" : "\n";
    if (!text.includes('"chooseTracks"')) {
        const next = text.replace(
            /("discography": "[^"\\]*")(\r?\n)(    \},\r?\n    "history":)/,
            `$1,${nl}      "chooseTracks": "Choose tracks"$2$3`,
        );
        if (next === text) {
            throw new Error(`chooseTracks anchor missing in ${file}`);
        }
        text = next;
    }
    if (!text.includes('"couldntGet"')) {
        const block = downloadEntries.map(([key, value]) => `      "${key}": ${JSON.stringify(value)}`).join(`,${nl}`);
        const next = text.replace(
            /("waitingCount": "\{\{count\}\} requests waiting")(\r?\n)(    \})/,
            `$1,${nl}${block}$2$3`,
        );
        if (next === text) {
            throw new Error(`downloads anchor missing in ${file}`);
        }
        text = next;
    }
    JSON.parse(text);
    writeFileSync(path, text);
    console.log(file);
}
