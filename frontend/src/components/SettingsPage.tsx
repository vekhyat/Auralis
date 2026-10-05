import { useState, useEffect, useCallback, useRef } from "react";
import { CommunitySourcesSettings, type CommunitySourcesHandle } from "@/components/CommunitySourcesSettings";
import { SourceConnectionsSettings } from "@/components/SourceConnectionsSettings";
import { ListeningConnectionsSettings } from "@/components/ListeningConnectionsSettings";
import { useTranslation } from "react-i18next";
import { flushSync } from "react-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { InputWithContext } from "@/components/ui/input-with-context";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { FolderOpen, Save, RotateCcw, Trash2, ExternalLink, DatabaseBackup, Search, FolderLock } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { getSettings, getSettingsWithDefaults, loadSettings, saveSettings, resetToDefaultSettings, applyThemeMode, TEMPLATE_VARIABLES, DEFAULT_SETTINGS, type Settings as SettingsType, type MetadataTagToggles, type ExistingFileCheckMode, } from "@/lib/settings";
import { FormatEditor } from "@/components/FormatEditor";
import { BackupSettings, RestoreSettings, SelectFolder, OpenConfigFolder } from "../../wailsjs/go/main/App";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { openExternal } from "@/lib/utils";
import i18n, { APP_LANGUAGES, type AppLanguage } from "@/i18n";
import chatGPTIcon from "@/assets/icons/chatgpt.svg";
import geminiIcon from "@/assets/icons/gemini.png";
function qualityChoice(settings: SettingsType): "16" | "24" | "atmos" {
    if (settings.downloader === "tidal") {
        if (settings.tidalQuality === "ATMOS")
            return "atmos";
        if (settings.tidalQuality === "HI_RES_LOSSLESS")
            return "24";
        return "16";
    }
    if (settings.downloader === "qobuz")
        return settings.qobuzQuality === "27" ? "24" : "16";
    if (settings.downloader === "amazon") {
        if (settings.amazonQuality === "atmos")
            return "atmos";
        if (settings.amazonQuality === "24")
            return "24";
        return "16";
    }
    return settings.autoQuality === "24" || settings.autoQuality === "atmos" ? settings.autoQuality : "16";
}
function withAutoQuality(settings: SettingsType, quality: "16" | "24" | "atmos"): SettingsType {
    return {
        ...settings,
        downloader: "auto",
        autoQuality: quality,
        tidalQuality: quality === "atmos" ? "ATMOS" : quality === "24" ? "HI_RES_LOSSLESS" : "LOSSLESS",
        qobuzQuality: quality === "24" ? "27" : "6",
        amazonQuality: quality,
    };
}
interface SettingsPageProps {
    onUnsavedChangesChange?: (hasUnsavedChanges: boolean) => void;
    onResetRequest?: (resetFn: () => void) => void;
    onForYouToggle?: (enabled: boolean) => void;
    initialSection?: "connections";
}
const AUTO_CONVERT_BITRATES: SettingsType["autoConvertBitrate"][] = ["320k", "256k", "192k", "128k"];
const LYRICS_TRANSLATION_LANGUAGES = [
    { code: "en", labelKey: "translation.sources.english", flag: "gb" },
    { code: "id", labelKey: "translation.sources.indonesian", flag: "id" },
    { code: "ms", labelKey: "translation.sources.malay", flag: "my" },
    { code: "es", labelKey: "translation.sources.spanish", flag: "es" },
    { code: "pt", labelKey: "translation.sources.portuguese", flag: "pt" },
    { code: "fr", labelKey: "translation.sources.french", flag: "fr" },
    { code: "de", labelKey: "translation.sources.german", flag: "de" },
    { code: "it", labelKey: "translation.sources.italian", flag: "it" },
    { code: "nl", labelKey: "translation.sources.dutch", flag: "nl" },
    { code: "pl", labelKey: "translation.sources.polish", flag: "pl" },
    { code: "ro", labelKey: "translation.sources.romanian", flag: "ro" },
    { code: "hu", labelKey: "translation.sources.hungarian", flag: "hu" },
    { code: "cs", labelKey: "translation.sources.czech", flag: "cz" },
    { code: "sv", labelKey: "translation.sources.swedish", flag: "se" },
    { code: "da", labelKey: "translation.sources.danish", flag: "dk" },
    { code: "fi", labelKey: "translation.sources.finnish", flag: "fi" },
    { code: "no", labelKey: "translation.sources.norwegian", flag: "no" },
    { code: "el", labelKey: "translation.sources.greek", flag: "gr" },
    { code: "ru", labelKey: "translation.sources.russian", flag: "ru" },
    { code: "uk", labelKey: "translation.sources.ukrainian", flag: "ua" },
    { code: "tr", labelKey: "translation.sources.turkish", flag: "tr" },
    { code: "ar", labelKey: "translation.sources.arabic", flag: "sa" },
    { code: "he", labelKey: "translation.sources.hebrew", flag: "il" },
    { code: "fa", labelKey: "translation.sources.persian", flag: "ir" },
    { code: "hi", labelKey: "translation.sources.hindi", flag: "in" },
    { code: "bn", labelKey: "translation.sources.bengali", flag: "bd" },
    { code: "ta", labelKey: "translation.sources.tamil", flag: "in" },
    { code: "th", labelKey: "translation.sources.thai", flag: "th" },
    { code: "vi", labelKey: "translation.sources.vietnamese", flag: "vn" },
    { code: "ja", labelKey: "translation.sources.japanese", flag: "jp" },
    { code: "ko", labelKey: "translation.sources.korean", flag: "kr" },
    { code: "zh", labelKey: "translation.sources.chinese", flag: "cn" },
    { code: "tl", labelKey: "translation.sources.tagalog", flag: "ph" },
] as const;
const METADATA_TAG_OPTIONS: Array<{
    key: keyof MetadataTagToggles;
    labelKey: string;
    example: string;
}> = [
    { key: "title", labelKey: "translation.common.title", example: "Golden" },
    { key: "artist", labelKey: "translation.common.artist", example: "HUNTR/X / EJAE / AUDREY NUNA / REI AMI" },
    { key: "album", labelKey: "translation.common.album", example: "KPop Demon Hunters (Soundtrack from the Netflix Film)" },
    { key: "albumArtist", labelKey: "translation.common.albumArtist", example: "KPop Demon Hunters Cast / HUNTR/X / Saja Boys" },
    { key: "date", labelKey: "translation.settings.dateYear", example: "2025-06-20" },
    { key: "trackNumber", labelKey: "translation.settings.trackNumber", example: "4/12" },
    { key: "discNumber", labelKey: "translation.settings.discNumber", example: "1/1" },
    { key: "genre", labelKey: "translation.settings.genre", example: "K-Pop" },
    { key: "composer", labelKey: "translation.settings.composer", example: "EJAE / Mark Sonnenblick / Joong Gyu Kwak" },
    { key: "copyright", labelKey: "translation.common.copyright", example: "Â© 2025 Visva Records / Republic Records" },
    { key: "label", labelKey: "translation.settings.labelPublisher", example: "K-Pop Demon Hunters" },
    { key: "isrc", labelKey: "literal.common.isrc", example: "QZ8BZ2513510" },
    { key: "upc", labelKey: "literal.common.upc", example: "00602478398346" },
    { key: "comment", labelKey: "translation.settings.comment", example: "https://open.spotify.com/track/1CPZ5BxNNd0n0nF4Orb9JS" },
];
export function SettingsPage({ onUnsavedChangesChange, onResetRequest, onForYouToggle, initialSection }: SettingsPageProps) {
    const { t } = useTranslation();
    const [savedSettings, setSavedSettings] = useState<SettingsType>(getSettings());
    const [tempSettings, setTempSettings] = useState<SettingsType>(savedSettings);
    const communitySourcesRef = useRef<CommunitySourcesHandle>(null);
    const connectionsRef = useRef<HTMLDivElement>(null);
    const focusConnections = useCallback(() => {
        if (initialSection === "connections") {
            connectionsRef.current?.scrollIntoView({ block: "start" });
            connectionsRef.current?.focus({ preventScroll: true });
        }
    }, [initialSection]);
    const [communitySourcesDirty, setCommunitySourcesDirty] = useState(false);
    const [showResetConfirm, setShowResetConfirm] = useState(false);
    const [showMetadataAdvanced, setShowMetadataAdvanced] = useState(false);
    const [showLyricsAdvanced, setShowLyricsAdvanced] = useState(false);
    const [lyricsLanguageSearch, setLyricsLanguageSearch] = useState("");
    const [showBackupDialog, setShowBackupDialog] = useState(false);
    const [backupAction, setBackupAction] = useState<"backup" | "restore" | "open" | null>(null);
    const [showCustomTidalApiDialog, setShowCustomTidalApiDialog] = useState(false);
    const [showCustomQobuzApiDialog, setShowCustomQobuzApiDialog] = useState(false);
    const hasUnsavedChanges = communitySourcesDirty || JSON.stringify(savedSettings) !== JSON.stringify(tempSettings);
    const normalizedLyricsLanguageSearch = lyricsLanguageSearch.trim().toLocaleLowerCase();
    const filteredLyricsTranslationLanguages = normalizedLyricsLanguageSearch
        ? LYRICS_TRANSLATION_LANGUAGES.filter((language) => language.code.includes(normalizedLyricsLanguageSearch)
            || t(language.labelKey).toLocaleLowerCase().includes(normalizedLyricsLanguageSearch))
        : LYRICS_TRANSLATION_LANGUAGES;
    const selectedQuality = qualityChoice(tempSettings);
    const isAtmosSelected = selectedQuality === "atmos";
    const showCdFallback = selectedQuality === "24" || (isAtmosSelected && tempSettings.allowAtmosFallback && tempSettings.atmosFallbackQuality === "24");
    const resetToSaved = useCallback(() => {
        const freshSavedSettings = getSettings();
        flushSync(() => {
            setTempSettings(freshSavedSettings);
            communitySourcesRef.current?.reset();
        });
    }, []);
    useEffect(() => {
        if (onResetRequest) {
            onResetRequest(resetToSaved);
        }
    }, [onResetRequest, resetToSaved]);
    useEffect(() => {
        onUnsavedChangesChange?.(hasUnsavedChanges);
    }, [hasUnsavedChanges, onUnsavedChangesChange]);
    useEffect(() => {
        void i18n.changeLanguage(tempSettings.language);
    }, [tempSettings.language]);
    useEffect(() => {
        applyThemeMode(savedSettings.themeMode);
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            if (savedSettings.themeMode === "auto") {
                applyThemeMode("auto");
            }
        };
        mediaQuery.addEventListener("change", handleChange);
        return () => mediaQuery.removeEventListener("change", handleChange);
    }, [savedSettings.themeMode]);
    useEffect(() => {
        applyThemeMode(tempSettings.themeMode);
    }, [tempSettings.themeMode]);
    useEffect(() => {
        const loadDefaults = async () => {
            const currentSettings = getSettings();
            if (!currentSettings.downloadPath) {
                const settingsWithDefaults = await getSettingsWithDefaults();
                setSavedSettings(settingsWithDefaults);
                setTempSettings(settingsWithDefaults);
                await saveSettings(settingsWithDefaults);
            }
        };
        loadDefaults();
    }, []);
    const handleSave = async () => {
        if (await communitySourcesRef.current?.save() === false) return;
        // The page no longer offers a store. Saving applies the visible quality and lets the app pick the file.
        await saveSettings(withAutoQuality(tempSettings, qualityChoice(tempSettings)));
        await i18n.changeLanguage(tempSettings.language);
        const persistedSettings = getSettings();
        setSavedSettings(persistedSettings);
        setTempSettings(persistedSettings);
        toast.success(t("translation.settings.settingsSaved"));
        onUnsavedChangesChange?.(false);
    };
    const handleReset = async () => {
        const defaultSettings = await resetToDefaultSettings();
        await communitySourcesRef.current?.resetToDefaults();
        setTempSettings(defaultSettings);
        setSavedSettings(defaultSettings);
        applyThemeMode(defaultSettings.themeMode);
        await i18n.changeLanguage(defaultSettings.language);
        setShowResetConfirm(false);
        toast.success(t("translation.settings.settingsResetDefault"));
    };
    const handleBrowseFolder = async () => {
        try {
            const selectedPath = await SelectFolder(tempSettings.downloadPath || "");
            if (selectedPath && selectedPath.trim() !== "") {
                setTempSettings((prev) => ({ ...prev, downloadPath: selectedPath }));
            }
        }
        catch (error) {
            console.error("Error selecting folder:", error);
            toast.error(t("translation.migrated.SettingsPage.errorSelectingFolder", { value1: error }));
        }
    };
    const handleOpenConfigFolder = async () => {
        setBackupAction("open");
        try {
            await OpenConfigFolder();
        }
        catch (error) {
            toast.error(t("translation.settings.errorOpeningConfigFolderValue1", { value1: error }));
        }
        finally {
            setBackupAction(null);
        }
    };
    const handleBackupSettings = async () => {
        setBackupAction("backup");
        try {
            if (await BackupSettings()) {
                toast.success(t("translation.settings.backupSettingsSuccess"));
            }
        }
        catch (error) {
            toast.error(t("translation.settings.backupSettingsError", { value1: error }));
        }
        finally {
            setBackupAction(null);
        }
    };
    const handleRestoreSettings = async () => {
        setBackupAction("restore");
        try {
            if (!(await RestoreSettings())) {
                return;
            }
            const restoredSettings = await loadSettings();
            await saveSettings(restoredSettings);
            setSavedSettings(restoredSettings);
            setTempSettings(restoredSettings);
            await i18n.changeLanguage(restoredSettings.language);
            applyThemeMode(restoredSettings.themeMode);
            setShowBackupDialog(false);
            toast.success(t("translation.settings.restoreSettingsSuccess"));
            onUnsavedChangesChange?.(false);
        }
        catch (error) {
            toast.error(t("translation.settings.restoreSettingsError", { value1: error }));
        }
        finally {
            setBackupAction(null);
        }
    };
    const handleQualityChange = (value: "16" | "24" | "atmos") => {
        setTempSettings((prev) => withAutoQuality(prev, value));
    };
    const persistCustomTidalApi = useCallback(async (nextValue: string) => {
        const normalizedValue = nextValue.trim().replace(/\/+$/g, "");
        const persistedSettings = getSettings();
        const nextSavedSettings: SettingsType = {
            ...persistedSettings,
            customTidalApi: normalizedValue,
        };
        await saveSettings(nextSavedSettings);
        const nextSavedState = getSettings();
        setSavedSettings(nextSavedState);
        setTempSettings((prev) => ({
            ...prev,
            customTidalApi: nextSavedState.customTidalApi,
        }));
    }, []);
    const persistCustomQobuzApi = useCallback(async (nextValue: string) => {
        const normalizedValue = nextValue.trim().replace(/\/+$/g, "");
        const persistedSettings = getSettings();
        const nextSavedSettings: SettingsType = {
            ...persistedSettings,
            customQobuzApi: normalizedValue,
        };
        await saveSettings(nextSavedSettings);
        const nextSavedState = getSettings();
        setSavedSettings(nextSavedState);
        setTempSettings((prev) => ({
            ...prev,
            customQobuzApi: nextSavedState.customQobuzApi,
        }));
    }, []);
    return (<div className="mx-auto w-full max-w-5xl space-y-10 pb-10">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold tracking-tight">{t("translation.common.settings")}</h1>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setShowResetConfirm(true)} className="gap-1.5">
            <RotateCcw className="h-4 w-4"/>
            {t("translation.common.resetDefault")}
          </Button>
          <Button onClick={handleSave} className="gap-1.5">
            <Save className="h-4 w-4"/>
            {t("translation.common.saveChanges")}
          </Button>
        </div>
      </div>

      <div className="flex-1 space-y-10">
        <section className="max-w-2xl space-y-4">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.settings.general")}</h2>
              <div className="space-y-2">
                <Label htmlFor="language">{t("translation.settings.language")}</Label>
                <Select value={tempSettings.language} onValueChange={(value: AppLanguage) => setTempSettings((prev) => ({ ...prev, language: value }))}>
                  <SelectTrigger id="language" className="w-fit min-w-44">
                    <SelectValue placeholder={t("translation.settings.selectLanguage")}/>
                  </SelectTrigger>
                  <SelectContent>
                    {APP_LANGUAGES.map((language) => (<SelectItem key={language.value} value={language.value}>
                        <span className="flex items-center gap-2">
                          <img src={`/assets/flags/${language.flag}.svg`} alt="" className="h-3.5 w-5 rounded-[2px] object-cover"/>
                          {language.label}
                        </span>
                      </SelectItem>))}
                  </SelectContent>
                </Select>
              </div>

              <div className="min-w-0 max-w-56 space-y-2">
                <Label htmlFor="theme-mode">{t("translation.settings.mode")}</Label>
                <Select value={tempSettings.themeMode} onValueChange={(value: "auto" | "light" | "dark") => setTempSettings((prev) => ({ ...prev, themeMode: value }))}>
                    <SelectTrigger id="theme-mode" className="w-full">
                      <SelectValue placeholder={t("translation.settings.selectThemeMode")}/>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="auto">{t("translation.settings.auto")}</SelectItem>
                      <SelectItem value="light">{t("translation.settings.light")}</SelectItem>
                      <SelectItem value="dark">{t("translation.settings.dark")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

              <div className="flex items-center gap-3 pt-2">
                <Switch id="sfx-enabled" checked={tempSettings.sfxEnabled} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                sfxEnabled: checked,
            }))}/>
                <Label htmlFor="sfx-enabled" className="cursor-pointer text-sm font-normal">
                  {t("translation.settings.soundEffects")}
                </Label>
              </div>

              <div className="flex items-center gap-3 pt-1">
                <Switch id="show-update-notifications" checked={tempSettings.showUpdateNotifications} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                showUpdateNotifications: checked,
            }))}/>
                <Label htmlFor="show-update-notifications" className="cursor-pointer text-sm font-normal">
                  {t("translation.settings.updateNotifications")}
                </Label>
              </div>
        </section>

        <section className="max-w-2xl space-y-4">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.settings.downloadPath")}</h2>
              <div className="space-y-2">
                <Label htmlFor="download-path">{t("translation.settings.downloadPath")}</Label>
                <div className="flex gap-2">
                  <InputWithContext id="download-path" value={tempSettings.downloadPath} onChange={(e) => setTempSettings((prev) => ({
                ...prev,
                downloadPath: e.target.value,
            }))} placeholder={t("literal.settings.cUsersYourusernameMusic")}/>
                  <Button type="button" onClick={handleBrowseFolder} className="gap-1.5">
                    <FolderOpen className="h-4 w-4"/>
                    {t("translation.common.browse")}
                  </Button>
                </div>
              </div>

              <div className="space-y-4 pt-2">
                <div className="relative flex min-h-5 items-center gap-3 pr-28">
                  <Switch id="embed-lyrics" checked={tempSettings.embedLyrics} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, embedLyrics: checked }))}/>
                  <Label htmlFor="embed-lyrics" className="cursor-pointer text-sm font-normal">{t("translation.sources.embedLyrics")}</Label>
                  {tempSettings.embedLyrics && (<Button type="button" variant="outline" size="sm" className="absolute right-0 top-1/2 -translate-y-1/2" onClick={() => setShowLyricsAdvanced(true)}>
                    {t("translation.settings.advanced")}
                  </Button>)}
                </div>
                <div className="flex items-center gap-3">
                  <Switch id="embed-max-quality-cover" checked={tempSettings.embedMaxQualityCover} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, embedMaxQualityCover: checked }))}/>
                  <Label htmlFor="embed-max-quality-cover" className="cursor-pointer text-sm font-normal">{t("translation.migrated.SettingsPage.embedMaxQualityCover")}</Label>
                </div>
                <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
                  <div className="flex items-center gap-3">
                    <Switch id="embed-genre" checked={tempSettings.embedGenre} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, embedGenre: checked }))}/>
                    <Label htmlFor="embed-genre" className="cursor-pointer text-sm font-normal">{t("translation.migrated.SettingsPage.embedGenre")}</Label>
                  </div>
                  {tempSettings.embedGenre && (<div className="flex items-center gap-3">
                    <Switch id="use-single-genre" checked={tempSettings.useSingleGenre} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, useSingleGenre: checked }))}/>
                    <Label htmlFor="use-single-genre" className="cursor-pointer text-sm font-normal">{t("translation.migrated.SettingsPage.useSingleGenre")}</Label>
                  </div>)}
                </div>
                <div className="flex items-center gap-3">
                  <Switch id="use-first-artist-only" checked={tempSettings.useFirstArtistOnly} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, useFirstArtistOnly: checked }))}/>
                  <Label htmlFor="use-first-artist-only" className="cursor-pointer text-sm font-normal">{t("translation.migrated.SettingsPage.useFirstArtistOnly")}</Label>
                </div>
                {!tempSettings.useFirstArtistOnly && (<div className="space-y-2">
                  <Label className="text-sm">{t("translation.migrated.SettingsPage.artistSeparator")}</Label>
                  <Select value={tempSettings.separator} onValueChange={(value: "comma" | "semicolon") => setTempSettings((prev) => ({ ...prev, separator: value }))}>
                    <SelectTrigger className="h-9 w-fit"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="comma">{t("translation.sources.comma")}</SelectItem>
                      <SelectItem value="semicolon">{t("translation.sources.semicolon")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>)}
                <div className="space-y-2 pt-1">
                  <Label>{t("translation.settings.config")}</Label>
                  <Button type="button" variant="outline" onClick={() => setShowBackupDialog(true)} className="w-fit">
                    {t("translation.settings.backupSettings")}
                  </Button>
                </div>
              </div>
        </section>
        <section className="max-w-2xl space-y-4">
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

        <CommunitySourcesSettings ref={communitySourcesRef} onDirtyChange={setCommunitySourcesDirty} />
        <SourceConnectionsSettings />

        <section className="max-w-3xl space-y-4">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.settings.naming")}</h2>
        {(() => {
            const separateToggle = (<div className="flex items-center gap-2">
              <Label htmlFor="separate-album-filename" className="text-sm cursor-pointer font-normal">{t("translation.settings.separateFilename")}</Label>
              <Switch id="separate-album-filename" checked={tempSettings.useSeparateAlbumFilename} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, useSeparateAlbumFilename: checked }))}/>
            </div>);
            const folderSingleTrackToggle = (<div className="flex items-center gap-2">
              <Label htmlFor="apply-folder-single-track" className="text-sm cursor-pointer font-normal">{t("translation.settings.singleTrack")}</Label>
              <Switch id="apply-folder-single-track" checked={tempSettings.applyFolderToSingleTrack} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, applyFolderToSingleTrack: checked }))}/>
            </div>);
            return (<div className="space-y-6">
              <FormatEditor tokens={TEMPLATE_VARIABLES} fields={[
                    ...(tempSettings.useSeparateAlbumFilename ? [
                        { title: t("translation.settings.filenameSingleTrack"), titleAccessory: separateToggle, value: tempSettings.filenameTemplate, defaultValue: DEFAULT_SETTINGS.filenameTemplate, suffix: ".flac", placeholder: "{artist} - {title}", column: "left" as const, onChange: (next: string) => setTempSettings((prev) => ({ ...prev, filenameTemplate: next })) },
                        { title: t("translation.settings.filenameAlbumPlaylistTrack"), value: tempSettings.albumFilenameTemplate, defaultValue: DEFAULT_SETTINGS.albumFilenameTemplate, suffix: ".flac", placeholder: "{track}. {title}", column: "left" as const, onChange: (next: string) => setTempSettings((prev) => ({ ...prev, albumFilenameTemplate: next })) },
                    ] : [
                        { title: t("translation.settings.filename"), titleAccessory: separateToggle, value: tempSettings.filenameTemplate, defaultValue: DEFAULT_SETTINGS.filenameTemplate, suffix: ".flac", placeholder: "{track}. {artist} - {title}", column: "left" as const, onChange: (next: string) => setTempSettings((prev) => ({ ...prev, filenameTemplate: next })) },
                    ]),
                    { title: t("translation.settings.folderStructure"), titleAccessory: folderSingleTrackToggle, value: tempSettings.folderTemplate, defaultValue: DEFAULT_SETTINGS.folderTemplate, suffix: "/", placeholder: "{album_artist}/{album}", column: "right" as const, onChange: (next: string) => setTempSettings((prev) => ({ ...prev, folderTemplate: next })) },
                ]}/>
            </div>);
        })()}
        </section>

        <section className="space-y-6">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.settings.fileManagement")}</h2>
          <div className="grid grid-cols-1 gap-8 lg:grid-cols-2">
            <div className="space-y-4 lg:pr-8 lg:border-r">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.settings.fileOutput")}</h3>

              <div className="flex items-center gap-3">
                <Switch id="create-playlist-folder" checked={tempSettings.createPlaylistFolder} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                createPlaylistFolder: checked,
            }))}/>
                <Label htmlFor="create-playlist-folder" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.playlistFolder")}
                </Label>
              </div>

              {tempSettings.createPlaylistFolder && (<div className="flex items-center gap-3 pl-7">
                <Switch id="playlist-owner-folder-name" checked={tempSettings.playlistOwnerFolderName} onCheckedChange={(checked) => setTempSettings((prev) => ({
                    ...prev,
                    playlistOwnerFolderName: checked,
                }))}/>
                <Label htmlFor="playlist-owner-folder-name" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.playlistOwnerFolderName")}
                </Label>
              </div>)}

              <div className="flex items-center gap-3">
                <Switch id="save-cover" checked={tempSettings.saveCover} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                saveCover: checked,
            }))}/>
                <Label htmlFor="save-cover" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.autoDownloadSeparateCover")}
                </Label>
              </div>

              <div className="space-y-2">
                <Label htmlFor="existing-file-check-mode">{t("translation.settings.existingFileCheck")}</Label>
                <Select value={tempSettings.existingFileCheckMode} onValueChange={(value: ExistingFileCheckMode) => setTempSettings((prev) => ({
                ...prev,
                existingFileCheckMode: value,
            }))}>
                  <SelectTrigger id="existing-file-check-mode">
                    <SelectValue placeholder={t("translation.settings.selectExistingFileCheckMode")}/>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="filename">{t("translation.settings.filename")}</SelectItem>
                    <SelectItem value="isrc">ISRC</SelectItem>
                    <SelectItem value="hybrid">{t("translation.settings.hybrid")}</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-3">
                <Switch id="redownload-with-suffix" checked={tempSettings.redownloadWithSuffix} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                redownloadWithSuffix: checked,
            }))}/>
                <Label htmlFor="redownload-with-suffix" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.redownloadSuffix")}
                </Label>
              </div>

              <div className="flex items-center gap-3">
                <Switch id="export-logs-file" checked={tempSettings.exportLogsFile} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                exportLogsFile: checked,
            }))}/>
                <Label htmlFor="export-logs-file" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.generateFailedLogs")}
                </Label>
              </div>
            </div>

            <div className="space-y-6 lg:pl-0">
              <div className="space-y-4">
                <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.settings.audioProcessing")}</h3>
                <div className="flex items-center gap-3">
                  <Switch id="auto-convert-audio" checked={tempSettings.autoConvertAudio} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, autoConvertAudio: checked }))}/>
                  <Label htmlFor="auto-convert-audio" className="text-sm font-normal cursor-pointer">{t("translation.settings.autoConvertAudio")}</Label>
                </div>
                {tempSettings.autoConvertAudio && (<div className="space-y-4 pl-7">
                  <div className="flex gap-3 flex-wrap">
                    <div className="space-y-2"><Label htmlFor="auto-convert-format">{t("translation.common.format")}</Label><Select value={tempSettings.autoConvertFormat} onValueChange={(value: SettingsType["autoConvertFormat"]) => setTempSettings((prev) => ({ ...prev, autoConvertFormat: value }))}>
                      <SelectTrigger id="auto-convert-format" className="w-32"><SelectValue /></SelectTrigger>
                      <SelectContent><SelectItem value="mp3">{t("literal.common.mp3")}</SelectItem><SelectItem value="m4a-aac">{t("literal.settings.m4aAac")}</SelectItem><SelectItem value="m4a-alac">{t("literal.settings.m4aAlac")}</SelectItem><SelectItem value="wav">{t("literal.common.wav")}</SelectItem><SelectItem value="aiff">{t("literal.common.aiff")}</SelectItem><SelectItem value="opus">{t("literal.common.opus")}</SelectItem></SelectContent>
                    </Select></div>
                    {!(["m4a-alac", "wav", "aiff"] as string[]).includes(tempSettings.autoConvertFormat) && (<div className="space-y-2"><Label htmlFor="auto-convert-bitrate">{t("translation.settings.bitrate")}</Label><Select value={tempSettings.autoConvertBitrate} onValueChange={(value: SettingsType["autoConvertBitrate"]) => setTempSettings((prev) => ({ ...prev, autoConvertBitrate: value }))}>
                      <SelectTrigger id="auto-convert-bitrate" className="w-32"><SelectValue /></SelectTrigger><SelectContent>{AUTO_CONVERT_BITRATES.map((bitrate) => <SelectItem key={bitrate} value={bitrate}>{bitrate}</SelectItem>)}</SelectContent>
                    </Select></div>)}
                  </div>
                  <div className="flex items-center gap-3"><Switch id="auto-convert-delete-original" checked={tempSettings.autoConvertDeleteOriginal} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, autoConvertDeleteOriginal: checked }))}/><Label htmlFor="auto-convert-delete-original" className="text-sm font-normal cursor-pointer">{t("translation.settings.deleteOriginalFileAfterConvert")}</Label></div>
                </div>)}
                <div className="flex items-center gap-3">
                  <Switch id="auto-resample-audio" checked={tempSettings.autoResampleAudio} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, autoResampleAudio: checked }))}/>
                  <Label htmlFor="auto-resample-audio" className="text-sm font-normal cursor-pointer">{t("translation.settings.autoResampleAudio")}</Label>
                </div>
                {tempSettings.autoResampleAudio && (<div className="space-y-4 pl-7">
                  <div className="flex gap-3 flex-wrap">
                    <div className="space-y-2"><Label htmlFor="auto-resample-bit-depth">{t("translation.settings.bitDepth")}</Label><Select value={tempSettings.autoResampleBitDepth} onValueChange={(value: SettingsType["autoResampleBitDepth"]) => setTempSettings((prev) => ({ ...prev, autoResampleBitDepth: value }))}>
                      <SelectTrigger id="auto-resample-bit-depth" className="w-32"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="16">{t("literal.common.value16Bit")}</SelectItem><SelectItem value="24">{t("literal.common.value24Bit")}</SelectItem></SelectContent>
                    </Select></div>
                    <div className="space-y-2"><Label htmlFor="auto-resample-rate">{t("translation.settings.sampleRate")}</Label><Select value={tempSettings.autoResampleSampleRate} onValueChange={(value: SettingsType["autoResampleSampleRate"]) => setTempSettings((prev) => ({ ...prev, autoResampleSampleRate: value }))}>
                      <SelectTrigger id="auto-resample-rate" className="w-32"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="44100">{t("literal.settings.value44Value1Khz")}</SelectItem><SelectItem value="48000">{t("literal.settings.value48Khz")}</SelectItem><SelectItem value="96000">{t("literal.settings.value96Khz")}</SelectItem><SelectItem value="192000">{t("literal.settings.value192Khz")}</SelectItem></SelectContent>
                    </Select></div>
                  </div>
                  <div className="flex items-center gap-3"><Switch id="auto-resample-delete-original" checked={tempSettings.autoResampleDeleteOriginal} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, autoResampleDeleteOriginal: checked }))}/><Label htmlFor="auto-resample-delete-original" className="text-sm font-normal cursor-pointer">{t("translation.settings.deleteOriginalFileAfterResample")}</Label></div>
                </div>)}
                <div className="space-y-3">
                  <div className="flex items-center gap-3">
                    <Switch id="auto-replaygain-tags" checked={tempSettings.autoReplayGainTags} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, autoReplayGainTags: checked }))}/>
                    <Label htmlFor="auto-replaygain-tags" className="cursor-pointer text-sm font-normal">{t("translation.settings.autoWriteReplayGainTags")}</Label>
                  </div>
                  {tempSettings.autoReplayGainTags && (<div className="max-w-xs space-y-2 pl-7">
                    <Label htmlFor="auto-replaygain-mode">{t("translation.settings.replayGainMode")}</Label>
                    <Select value={tempSettings.autoReplayGainMode} onValueChange={(value: SettingsType["autoReplayGainMode"]) => setTempSettings((prev) => ({ ...prev, autoReplayGainMode: value }))}>
                      <SelectTrigger id="auto-replaygain-mode"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="track">{t("translation.settings.replayGainTrackMode")}</SelectItem>
                        <SelectItem value="album">{t("translation.settings.replayGainAlbumMode")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>)}
                </div>
              </div>

              <div className="space-y-4">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.settings.m3u8Playlist")}</h3>

              <div className="flex items-center gap-3">
                <Switch id="create-m3u8-file" checked={tempSettings.createM3u8File} onCheckedChange={(checked) => setTempSettings((prev) => ({
                ...prev,
                createM3u8File: checked,
            }))}/>
                <Label htmlFor="create-m3u8-file" className="text-sm cursor-pointer font-normal">
                  {t("translation.settings.createM3u8PlaylistFile")}
                </Label>
              </div>
              </div>
            </div>
          </div>
        </section>

        <section className="max-w-4xl space-y-4">
          <h2 className="border-b border-border pb-1.5 text-sm font-semibold tracking-tight">{t("translation.common.metadata")}</h2>
          <div className="min-w-0 space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-[11px] font-semibold tracking-widest uppercase text-muted-foreground">{t("translation.settings.embeddedTags")}</h3>
              <div className="flex gap-2">
                <Button type="button" variant="outline" size="sm" onClick={() => setTempSettings((prev) => ({ ...prev, metadataTags: Object.fromEntries(METADATA_TAG_OPTIONS.map(({ key }) => [key, true])) as unknown as MetadataTagToggles }))}>{t("translation.settings.enableAll")}</Button>
                <Button type="button" variant="outline" size="sm" onClick={() => setTempSettings((prev) => ({ ...prev, metadataTags: Object.fromEntries(METADATA_TAG_OPTIONS.map(({ key }) => [key, false])) as unknown as MetadataTagToggles }))}>{t("translation.settings.disableAll")}</Button>
                <Button type="button" variant="outline" size="sm" onClick={() => setShowMetadataAdvanced(true)}>{t("translation.settings.advanced")}</Button>
              </div>
            </div>
            <div className="grid min-w-0 grid-cols-1 md:grid-cols-2 md:gap-x-8">
              {[METADATA_TAG_OPTIONS.slice(0, 7), METADATA_TAG_OPTIONS.slice(7)].map((column, columnIndex) => (<div key={columnIndex} className={`min-w-0 ${columnIndex === 1 ? "md:border-l md:pl-8" : ""}`}>
                {column.map((option) => (<div key={option.key} className="flex min-w-0 items-center gap-3 overflow-hidden py-2">
                  <Switch id={`metadata-tag-${option.key}`} checked={tempSettings.metadataTags[option.key]} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, metadataTags: { ...prev.metadataTags, [option.key]: checked } }))}/>
                  <Label htmlFor={`metadata-tag-${option.key}`} className="flex min-w-0 flex-1 cursor-pointer items-baseline gap-1 overflow-hidden text-sm font-normal">
                    <span className="shrink-0">{t(option.labelKey)}</span>
                    <span className="min-w-0 flex-1 truncate text-muted-foreground">(e.g. {option.example})</span>
                  </Label>
                </div>))}
              </div>))}
            </div>
          </div>
        </section>

        <div ref={connectionsRef} tabIndex={-1} className="scroll-mt-8">
          <ListeningConnectionsSettings onForYouToggle={onForYouToggle} onReady={focusConnections} />
        </div>
      </div>

      <Dialog open={showCustomTidalApiDialog} onOpenChange={setShowCustomTidalApiDialog}>
        <DialogContent className="sm:max-w-md [&>button]:hidden">
          <DialogHeader>
            <div className="flex items-center justify-between gap-3">
              <DialogTitle>{t("translation.migrated.SettingsPage.tidalSource")}</DialogTitle>
              <button type="button" onClick={() => openExternal("https://github.com/binimum/hifi-api")} className="inline-flex cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:text-foreground hover:underline">
                {t("translation.migrated.SettingsPage.howDoICreateOne")}
                <ExternalLink className="h-3 w-3"/>
              </button>
            </div>
            <DialogDescription />
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="custom-tidal-api">{t("translation.migrated.SettingsPage.instanceURL")}</Label>
              <div className="flex gap-2">
                <Input id="custom-tidal-api" type="url" value={tempSettings.customTidalApi || ""} onChange={(e) => {
            const nextValue = e.target.value.replace(/\/+$/g, "");
            void persistCustomTidalApi(nextValue);
        }} placeholder="https://your-hifi-api.example"/>
                {tempSettings.customTidalApi && (<Button type="button" variant="destructive" size="icon" onClick={() => {
                void persistCustomTidalApi("");
            }}>
                    <Trash2 className="h-4 w-4"/>
                  </Button>)}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCustomTidalApiDialog(false)}>
              {t("translation.common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showCustomQobuzApiDialog} onOpenChange={setShowCustomQobuzApiDialog}>
        <DialogContent className="sm:max-w-md [&>button]:hidden">
          <DialogHeader>
            <div className="flex items-center justify-between gap-3">
              <DialogTitle>{t("translation.migrated.SettingsPage.qobuzSource")}</DialogTitle>
              <button type="button" onClick={() => openExternal("https://github.com/QobuzDL/Qobuz-DL")} className="inline-flex cursor-pointer items-center gap-1 text-xs text-muted-foreground hover:text-foreground hover:underline">
                {t("translation.migrated.SettingsPage.howDoICreateOne")}
                <ExternalLink className="h-3 w-3"/>
              </button>
            </div>
            <DialogDescription />
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="custom-qobuz-api">{t("translation.migrated.SettingsPage.instanceURL")}</Label>
              <div className="flex gap-2">
                <Input id="custom-qobuz-api" type="url" value={tempSettings.customQobuzApi || ""} onChange={(e) => {
            const nextValue = e.target.value.replace(/\/+$/g, "");
            void persistCustomQobuzApi(nextValue);
        }} placeholder="https://your-qobuz-dl.example"/>
                {tempSettings.customQobuzApi && (<Button type="button" variant="destructive" size="icon" onClick={() => {
                void persistCustomQobuzApi("");
            }}>
                    <Trash2 className="h-4 w-4"/>
                  </Button>)}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCustomQobuzApiDialog(false)}>
              {t("translation.common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showMetadataAdvanced} onOpenChange={setShowMetadataAdvanced}>
        <DialogContent className="sm:max-w-md [&>button]:hidden">
          <DialogHeader>
            <DialogTitle>{t("translation.settings.advancedMetadata")}</DialogTitle>
          </DialogHeader>
          <div className="flex items-center justify-between gap-4">
            <Label>{t("translation.settings.dateFormat")}</Label>
            <div className="flex w-fit border border-border bg-muted/50 p-0.5">
              <button type="button" onClick={() => setTempSettings((prev) => ({ ...prev, metadataDateFormat: "full" }))} className={`cursor-pointer px-3 py-1 text-sm font-medium transition-colors ${tempSettings.metadataDateFormat === "full" ? "bg-background text-foreground" : "text-muted-foreground hover:text-foreground"}`}>{t("translation.settings.fullDateFormat")}</button>
              <button type="button" onClick={() => setTempSettings((prev) => ({ ...prev, metadataDateFormat: "year" }))} className={`cursor-pointer px-3 py-1 text-sm font-medium transition-colors ${tempSettings.metadataDateFormat === "year" ? "bg-background text-foreground" : "text-muted-foreground hover:text-foreground"}`}>{t("translation.settings.yearOnlyFormat")}</button>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setShowMetadataAdvanced(false)}>{t("translation.common.close")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showLyricsAdvanced} onOpenChange={setShowLyricsAdvanced}>
        <DialogContent className="sm:max-w-md [&>button]:hidden">
          <DialogHeader>
            <DialogTitle>{t("translation.settings.advanced")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-5">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="lrclib-title-fallback" className="cursor-pointer">{t("translation.sources.lrclibTitleFallback")}</Label>
              <Switch id="lrclib-title-fallback" checked={tempSettings.lrclibTitleFallback} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, lrclibTitleFallback: checked }))}/>
            </div>
            <div className="space-y-2">
              <Label>{t("translation.sources.lyricsTranslation")}</Label>
              <Select value={tempSettings.lyricsTranslationMode} onValueChange={(value: SettingsType["lyricsTranslationMode"]) => setTempSettings((prev) => ({ ...prev, lyricsTranslationMode: value }))}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">{t("translation.sources.translationOff")}</SelectItem>
                  <SelectItem value="chatgpt"><span className="flex items-center gap-2"><img src={chatGPTIcon} alt="" className="h-4 w-4 object-contain brightness-0 dark:invert"/>{t("translation.sources.chatGPT")}</span></SelectItem>
                  <SelectItem value="gemini"><span className="flex items-center gap-2"><img src={geminiIcon} alt="" className="h-4 w-4 object-contain"/>{t("translation.sources.googleGemini")}</span></SelectItem>
                </SelectContent>
              </Select>
            </div>
            {tempSettings.lyricsTranslationMode !== "off" && (<>
              <div className="flex items-center justify-between gap-4">
                <Label htmlFor="lyrics-translation-auto-fallback" className="cursor-pointer">{t("translation.sources.translationAutoFallback")}</Label>
                <Switch id="lyrics-translation-auto-fallback" checked={tempSettings.lyricsTranslationAutoFallback} onCheckedChange={(checked) => setTempSettings((prev) => ({ ...prev, lyricsTranslationAutoFallback: checked }))}/>
              </div>
              <div className="space-y-2">
                <Label>{t("translation.sources.translationLanguage")}</Label>
                <Select value={tempSettings.lyricsTranslationLang} onValueChange={(value) => setTempSettings((prev) => ({ ...prev, lyricsTranslationLang: value }))} onOpenChange={(open) => {
                if (!open)
                    setLyricsLanguageSearch("");
            }}>
                  <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent className="max-h-56">
                    <div className="sticky top-0 z-10 bg-popover p-1" onKeyDown={(event) => event.stopPropagation()}>
                      <div className="relative">
                        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"/>
                        <Input value={lyricsLanguageSearch} onChange={(event) => setLyricsLanguageSearch(event.target.value)} placeholder={t("translation.sources.searchLanguage")} className="h-8 pl-8" autoFocus/>
                      </div>
                    </div>
                    {filteredLyricsTranslationLanguages.map((language) => (<SelectItem key={language.code} value={language.code} textValue={t(language.labelKey)}>
                      <span className="flex items-center gap-2">
                        <img src={`/assets/flags/${language.flag}.svg`} alt="" className="h-3 w-4 shrink-0 rounded-[1px] object-cover"/>
                        {t(language.labelKey)}
                      </span>
                    </SelectItem>))}
                    {filteredLyricsTranslationLanguages.length === 0 && (<div className="px-2 py-6 text-center text-sm text-muted-foreground">{t("translation.sources.noLanguageFound")}</div>)}
                  </SelectContent>
                </Select>
              </div>
            </>)}
          </div>
          <DialogFooter>
            <Button onClick={() => setShowLyricsAdvanced(false)}>{t("translation.common.close")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showResetConfirm} onOpenChange={setShowResetConfirm}>
        <DialogContent className="max-w-md [&>button]:hidden">
          <DialogHeader>
            <DialogTitle>{t("translation.migrated.SettingsPage.resetToDefault")}</DialogTitle>
            <DialogDescription>
              {t("translation.migrated.SettingsPage.thisWillResetAllSettingsToTheir")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowResetConfirm(false)}>
              {t("translation.common.cancel")}
            </Button>
            <Button onClick={handleReset}>{t("translation.common.reset")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showBackupDialog} onOpenChange={setShowBackupDialog}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("translation.settings.backupSettings")}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3">
            <Button type="button" variant="outline" onClick={() => void handleBackupSettings()} disabled={backupAction !== null} className="justify-start gap-2">
              <Save className="h-4 w-4 shrink-0"/>
              <span className="font-medium">{t("translation.settings.backup")}</span>
            </Button>
            <Button type="button" variant="outline" onClick={() => void handleRestoreSettings()} disabled={backupAction !== null} className="justify-start gap-2">
              <DatabaseBackup className="h-4 w-4 shrink-0"/>
              <span className="font-medium">{t("translation.settings.restore")}</span>
            </Button>
            <Button type="button" variant="outline" onClick={() => void handleOpenConfigFolder()} disabled={backupAction !== null} className="justify-start gap-2">
              <FolderLock className="h-4 w-4 shrink-0"/>
              <span className="font-medium">{t("translation.settings.openConfigFolder")}</span>
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>);
}
