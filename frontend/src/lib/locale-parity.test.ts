import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

function flatten(value: unknown, prefix = ""): Array<[string, string]> {
    if (typeof value === "string") {
        return [[prefix, value]];
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) {
        return [];
    }
    return Object.entries(value).flatMap(([key, child]) => flatten(child, prefix ? `${prefix}.${key}` : key));
}

function placeholders(value: string): string[] {
    return [...value.matchAll(/{{\s*([^{}]+?)\s*}}/g)].map((match) => match[1]).sort();
}

const localeDir = join(dirname(fileURLToPath(import.meta.url)), "../locales");

test("every locale has the same keys and placeholder slots as English", () => {
    const files = readdirSync(localeDir).filter((file) => file.endsWith(".json")).sort();
    assert.ok(files.includes("en.json"));
    const english = new Map(flatten(JSON.parse(readFileSync(join(localeDir, "en.json"), "utf8"))));
    assert.ok(english.size > 0);
    for (const file of files) {
        if (file === "en.json") {
            continue;
        }
        const messages = new Map(flatten(JSON.parse(readFileSync(join(localeDir, file), "utf8"))));
        const missing = [...english.keys()].filter((key) => !messages.has(key));
        const extra = [...messages.keys()].filter((key) => !english.has(key));
        assert.deepEqual(missing, [], `${file} is missing keys`);
        assert.deepEqual(extra, [], `${file} has extra keys`);
        for (const [key, value] of english) {
            const translated = messages.get(key);
            assert.equal(typeof translated, "string");
            assert.notEqual(translated?.trim(), "");
            assert.deepEqual(placeholders(translated ?? ""), placeholders(value), `${file} ${key}`);
        }
    }
});
