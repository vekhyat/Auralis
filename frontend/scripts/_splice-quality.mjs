import { readFileSync, writeFileSync } from "node:fs";

const path = new URL("../src/components/SettingsPage.tsx", import.meta.url);
const lines = readFileSync(path, "utf8").split(/\n/);
const start = lines.findIndex((line) => line.includes("translation.trackInfo.download"));
const naming = lines.findIndex((line) => line.includes("translation.settings.naming"));
if (start < 0 || naming < 0) {
    throw new Error(`markers missing ${start} ${naming}`);
}
let sectionStart = start;
while (sectionStart > 0 && !lines[sectionStart].includes("<section")) sectionStart--;
let sectionEnd = naming;
while (sectionEnd > sectionStart && !lines[sectionEnd].includes("<section")) sectionEnd--;
const replacement = `        <section className="max-w-2xl space-y-4">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.downloads.quality")}</h2>
          <p className="text-sm text-muted-foreground">{t("translation.downloads.qualityHint")}</p>
          <Select value={selectedQuality} onValueChange={(value: "16" | "24" | "atmos") => handleQualityChange(value)}>
            <SelectTrigger className="h-9 w-fit" aria-label={t("translation.downloads.quality")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="16">{t("literal.backend.value16BitValue441khz")}</SelectItem>
              <SelectItem value="24">{t("translation.migrated.SettingsPage.24Bit48kHz192kHz")}</SelectItem>
              <SelectItem value="atmos">{t("literal.backend.dolbyAtmos")}</SelectItem>
            </SelectContent>
          </Select>
          {isAtmosSelected && (<div className="flex flex-wrap items-center gap-3">
            <Switch id="allow-atmos-fallback" checked={tempSettings.allowAtmosFallback} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, allowAtmosFallback: checked }))}/>
            <Label htmlFor="allow-atmos-fallback" className="cursor-pointer text-sm font-normal">{t("translation.sources.fallbackFlac")}</Label>
            {tempSettings.allowAtmosFallback && (<Select value={tempSettings.atmosFallbackQuality} onValueChange={(value: "16" | "24") => setTempSettings((prev) => ({ ...prev, atmosFallbackQuality: value }))}>
              <SelectTrigger className="h-8 w-fit"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="16">{t("literal.backend.value16BitValue441khz")}</SelectItem>
                <SelectItem value="24">{t("translation.migrated.SettingsPage.24Bit48kHz192kHz")}</SelectItem>
              </SelectContent>
            </Select>)}
          </div>)}
          {showCdFallback && (<div className="flex items-center gap-3">
            <Switch id="allow-fallback" checked={tempSettings.allowFallback} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, allowFallback: checked }))}/>
            <Label htmlFor="allow-fallback" className="cursor-pointer text-sm font-normal">{t("translation.migrated.SettingsPage.allowQualityFallback16Bit")}</Label>
          </div>)}
        </section>
`;
const next = [...lines.slice(0, sectionStart), ...replacement.split(/\n/), ...lines.slice(sectionEnd)];
writeFileSync(path, next.join("\n"));
console.log(`replaced ${sectionStart + 1}-${sectionEnd} `);
