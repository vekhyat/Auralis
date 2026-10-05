import { useState } from "react";
import { t } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { SelectSyncPlaylist } from "../../../wailsjs/go/main/App";
import { syncengine } from "../../../wailsjs/go/models";
import { Calendar, Disc3, FileMusic, FolderArchive, Music, Plus, X } from "lucide-react";
import type { SelectionEditorProps } from "./types";
import { validateRecentDays } from "./validation";

const PRESET_DAYS = ["0", "30", "60", "90", "180", "365"] as const;

export function SelectionRulesEditor({ rules, onChange, disabled }: SelectionEditorProps) {
    const [newArtist, setNewArtist] = useState("");
    const [newAlbum, setNewAlbum] = useState("");
    const [newPlaylist, setNewPlaylist] = useState("");
    const [customDaysInput, setCustomDaysInput] = useState(
        !PRESET_DAYS.includes(String(rules.recent_days || 0) as typeof PRESET_DAYS[number])
            ? String(rules.recent_days || 0)
            : "90",
    );

    const updateRules = (patch: Partial<syncengine.Selection>) => {
        const next = syncengine.Selection.createFrom({
            whole_library: rules.whole_library ?? true,
            artists: rules.artists ?? [],
            albums: rules.albums ?? [],
            playlists: rules.playlists ?? [],
            recent_days: rules.recent_days ?? 0,
            ...patch,
        });
        onChange(next);
    };

    const handleAddArtist = () => {
        const trimmed = newArtist.trim();
        if (!trimmed) return;
        const current = rules.artists || [];
        if (!current.some((a) => a.toLowerCase() === trimmed.toLowerCase())) {
            updateRules({ artists: [...current, trimmed] });
        }
        setNewArtist("");
    };

    const handleRemoveArtist = (artistToRemove: string) => {
        const current = rules.artists || [];
        updateRules({ artists: current.filter((a) => a !== artistToRemove) });
    };

    const handleAddAlbum = () => {
        const trimmed = newAlbum.trim();
        if (!trimmed) return;
        const current = rules.albums || [];
        if (!current.some((a) => a.toLowerCase() === trimmed.toLowerCase())) {
            updateRules({ albums: [...current, trimmed] });
        }
        setNewAlbum("");
    };

    const handleRemoveAlbum = (albumToRemove: string) => {
        const current = rules.albums || [];
        updateRules({ albums: current.filter((a) => a !== albumToRemove) });
    };

    const handleAddPlaylist = (pathToAdd?: string) => {
        const target = (pathToAdd ?? newPlaylist).trim();
        if (!target) return;
        if (!target.toLowerCase().endsWith(".m3u8")) {
            toast.error(t("translation.sync.selectionPlaylistsPlaceholder"));
            return;
        }
        const current = rules.playlists || [];
        if (!current.includes(target)) {
            updateRules({ playlists: [...current, target] });
        }
        if (!pathToAdd) setNewPlaylist("");
    };

    const handleRemovePlaylist = (playlistToRemove: string) => {
        const current = rules.playlists || [];
        updateRules({ playlists: current.filter((p) => p !== playlistToRemove) });
    };

    const handleBrowsePlaylist = async () => {
        try {
            const selected = await SelectSyncPlaylist(t("translation.sync.selectionPlaylists"), t("translation.sync.selectionPlaylists"));
            if (selected) {
                if (selected.toLowerCase().endsWith(".m3u8")) {
                    handleAddPlaylist(selected);
                } else {
                    toast.error(t("translation.sync.selectionPlaylistsPlaceholder"));
                }
            }
        } catch (err) {
            toast.error(String(err));
        }
    };

    const currentRecentDays = rules.recent_days || 0;
    const isPreset = PRESET_DAYS.includes(String(currentRecentDays) as typeof PRESET_DAYS[number]);
    const recentSelectionValue = isPreset ? String(currentRecentDays) : "custom";

    const handleRecentChange = (val: string) => {
        if (val === "custom") {
            const parsed = parseInt(customDaysInput, 10);
            const valid = validateRecentDays(isNaN(parsed) ? 90 : parsed);
            if (valid.valid) {
                updateRules({ recent_days: isNaN(parsed) ? 90 : parsed });
            }
        } else {
            const days = parseInt(val, 10);
            updateRules({ recent_days: isNaN(days) ? 0 : days });
        }
    };

    const handleCustomDaysBlur = () => {
        const parsed = parseInt(customDaysInput, 10);
        if (Number.isNaN(parsed) || parsed < 0 || parsed > 36500) {
            toast.error(t("translation.sync.validationRecentDays"));
            setCustomDaysInput("90");
            updateRules({ recent_days: 90 });
            return;
        }
        updateRules({ recent_days: parsed });
    };

    const hasFilters =
        (rules.artists && rules.artists.length > 0) ||
        (rules.albums && rules.albums.length > 0) ||
        (rules.playlists && rules.playlists.length > 0) ||
        (rules.recent_days && rules.recent_days > 0);

    return (
        <section
            aria-label={t("translation.sync.ariaSelectionRules")}
            className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4"
        >
            <div className="flex flex-col gap-1">
                <div className="flex items-center justify-between">
                    <Label
                        htmlFor="whole-library-toggle"
                        className="text-xs font-semibold cursor-pointer flex items-center gap-2"
                    >
                        <FolderArchive className="size-4 text-primary" />
                        {t("translation.sync.selectionWholeLibrary")}
                    </Label>
                    <Switch
                        id="whole-library-toggle"
                        aria-label={t("translation.sync.selectionWholeLibrary")}
                        checked={rules.whole_library ?? true}
                        disabled={disabled}
                        onCheckedChange={(checked) => updateRules({ whole_library: checked })}
                    />
                </div>
                <p className="text-[11px] text-muted-foreground">
                    {rules.whole_library
                        ? t("translation.sync.selectionWholeLibraryDesc")
                        : t("translation.sync.selectionCustomDesc")}
                </p>
            </div>

            {!rules.whole_library && (
                <div className="flex flex-col gap-4 border-t pt-3">
                    {/* Artists Selection */}
                    <div className="flex flex-col gap-2">
                        <div className="flex items-center justify-between">
                            <Label className="text-xs font-medium flex items-center gap-1.5">
                                <Music className="size-3.5 text-muted-foreground" />
                                {t("translation.sync.selectionArtists")}
                                {rules.artists && rules.artists.length > 0 ? (
                                    <span className="text-[10px] text-muted-foreground">({rules.artists.length})</span>
                                ) : null}
                            </Label>
                            {rules.artists && rules.artists.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => updateRules({ artists: [] })}
                                    disabled={disabled}
                                    className="cursor-pointer text-[10px] text-muted-foreground hover:text-foreground"
                                >
                                    {t("translation.sync.selectionClear")}
                                </button>
                            )}
                        </div>

                        <div className="flex gap-2">
                            <Input
                                value={newArtist}
                                onChange={(e) => setNewArtist(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        handleAddArtist();
                                    }
                                }}
                                disabled={disabled}
                                placeholder={t("translation.sync.selectionArtistsPlaceholder")}
                                aria-label={t("translation.sync.selectionArtists")}
                                className="h-8 text-xs"
                            />
                            <Button
                                type="button"
                                size="sm"
                                variant="outline"
                                disabled={disabled || !newArtist.trim()}
                                onClick={handleAddArtist}
                                aria-label={t("translation.sync.selectionAdd")}
                                className="h-8 px-2.5"
                            >
                                <Plus className="size-3.5" />
                                <span className="sr-only sm:not-sr-only sm:ml-1 text-xs">
                                    {t("translation.sync.selectionAdd")}
                                </span>
                            </Button>
                        </div>

                        {rules.artists && rules.artists.length > 0 ? (
                            <div className="flex flex-wrap gap-1.5 pt-1 max-h-32 overflow-y-auto">
                                {rules.artists.map((artist) => (
                                    <span
                                        key={artist}
                                        className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs text-foreground"
                                    >
                                        <span className="truncate max-w-[180px]">{artist}</span>
                                        <button
                                            type="button"
                                            onClick={() => handleRemoveArtist(artist)}
                                            disabled={disabled}
                                            aria-label={t("translation.sync.selectionRemove", { item: artist })}
                                            className="text-muted-foreground hover:text-foreground cursor-pointer"
                                        >
                                            <X className="size-3" />
                                        </button>
                                    </span>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {/* Albums Selection */}
                    <div className="flex flex-col gap-2">
                        <div className="flex items-center justify-between">
                            <Label className="text-xs font-medium flex items-center gap-1.5">
                                <Disc3 className="size-3.5 text-muted-foreground" />
                                {t("translation.sync.selectionAlbums")}
                                {rules.albums && rules.albums.length > 0 ? (
                                    <span className="text-[10px] text-muted-foreground">({rules.albums.length})</span>
                                ) : null}
                            </Label>
                            {rules.albums && rules.albums.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => updateRules({ albums: [] })}
                                    disabled={disabled}
                                    className="cursor-pointer text-[10px] text-muted-foreground hover:text-foreground"
                                >
                                    {t("translation.sync.selectionClear")}
                                </button>
                            )}
                        </div>

                        <div className="flex gap-2">
                            <Input
                                value={newAlbum}
                                onChange={(e) => setNewAlbum(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        handleAddAlbum();
                                    }
                                }}
                                disabled={disabled}
                                placeholder={t("translation.sync.selectionAlbumsPlaceholder")}
                                aria-label={t("translation.sync.selectionAlbums")}
                                className="h-8 text-xs"
                            />
                            <Button
                                type="button"
                                size="sm"
                                variant="outline"
                                disabled={disabled || !newAlbum.trim()}
                                onClick={handleAddAlbum}
                                aria-label={t("translation.sync.selectionAdd")}
                                className="h-8 px-2.5"
                            >
                                <Plus className="size-3.5" />
                                <span className="sr-only sm:not-sr-only sm:ml-1 text-xs">
                                    {t("translation.sync.selectionAdd")}
                                </span>
                            </Button>
                        </div>

                        {rules.albums && rules.albums.length > 0 ? (
                            <div className="flex flex-wrap gap-1.5 pt-1 max-h-32 overflow-y-auto">
                                {rules.albums.map((album) => (
                                    <span
                                        key={album}
                                        className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs text-foreground"
                                    >
                                        <span className="truncate max-w-[180px]">{album}</span>
                                        <button
                                            type="button"
                                            onClick={() => handleRemoveAlbum(album)}
                                            disabled={disabled}
                                            aria-label={t("translation.sync.selectionRemove", { item: album })}
                                            className="text-muted-foreground hover:text-foreground cursor-pointer"
                                        >
                                            <X className="size-3" />
                                        </button>
                                    </span>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {/* Playlists Selection */}
                    <div className="flex flex-col gap-2">
                        <div className="flex items-center justify-between">
                            <Label className="text-xs font-medium flex items-center gap-1.5">
                                <FileMusic className="size-3.5 text-muted-foreground" />
                                {t("translation.sync.selectionPlaylists")}
                                {rules.playlists && rules.playlists.length > 0 ? (
                                    <span className="text-[10px] text-muted-foreground">({rules.playlists.length})</span>
                                ) : null}
                            </Label>
                            {rules.playlists && rules.playlists.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => updateRules({ playlists: [] })}
                                    disabled={disabled}
                                    className="cursor-pointer text-[10px] text-muted-foreground hover:text-foreground"
                                >
                                    {t("translation.sync.selectionClear")}
                                </button>
                            )}
                        </div>

                        <div className="flex flex-wrap gap-2">
                            <Input
                                value={newPlaylist}
                                onChange={(e) => setNewPlaylist(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        handleAddPlaylist();
                                    }
                                }}
                                disabled={disabled}
                                placeholder={t("translation.sync.selectionPlaylistsPlaceholder")}
                                aria-label={t("translation.sync.selectionPlaylists")}
                                className="h-8 min-w-[200px] flex-1 text-xs font-mono"
                            />
                            <Button
                                type="button"
                                size="sm"
                                variant="outline"
                                disabled={disabled || !newPlaylist.trim()}
                                onClick={() => handleAddPlaylist()}
                                aria-label={t("translation.sync.selectionAdd")}
                                className="h-8 px-2.5"
                            >
                                <Plus className="size-3.5" />
                                <span className="sr-only sm:not-sr-only sm:ml-1 text-xs">
                                    {t("translation.sync.selectionAdd")}
                                </span>
                            </Button>
                            <Button
                                type="button"
                                size="sm"
                                variant="outline"
                                disabled={disabled}
                                onClick={() => void handleBrowsePlaylist()}
                                className="h-8 text-xs"
                            >
                                {t("translation.sync.selectionBrowsePlaylist")}
                            </Button>
                        </div>

                        {rules.playlists && rules.playlists.length > 0 ? (
                            <div className="flex flex-wrap gap-1.5 pt-1 max-h-32 overflow-y-auto">
                                {rules.playlists.map((playlist) => (
                                    <span
                                        key={playlist}
                                        className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs text-foreground font-mono"
                                    >
                                        <span className="truncate max-w-[240px]">{playlist}</span>
                                        <button
                                            type="button"
                                            onClick={() => handleRemovePlaylist(playlist)}
                                            disabled={disabled}
                                            aria-label={t("translation.sync.selectionRemove", { item: playlist })}
                                            className="text-muted-foreground hover:text-foreground cursor-pointer"
                                        >
                                            <X className="size-3" />
                                        </button>
                                    </span>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    {/* Recent Window */}
                    <div className="flex flex-col gap-2">
                        <Label className="text-xs font-medium flex items-center gap-1.5">
                            <Calendar className="size-3.5 text-muted-foreground" />
                            {t("translation.sync.selectionRecentDays")}
                        </Label>
                        <div className="flex flex-wrap items-center gap-2">
                            <Select
                                value={recentSelectionValue}
                                onValueChange={handleRecentChange}
                                disabled={disabled}
                            >
                                <SelectTrigger
                                    className="h-8 w-44 text-xs"
                                    aria-label={t("translation.sync.selectionRecentDays")}
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="0">{t("translation.sync.selectionRecentAll")}</SelectItem>
                                    <SelectItem value="30">{t("translation.sync.selectionRecent30")}</SelectItem>
                                    <SelectItem value="60">{t("translation.sync.selectionRecent60")}</SelectItem>
                                    <SelectItem value="90">{t("translation.sync.selectionRecent90")}</SelectItem>
                                    <SelectItem value="180">{t("translation.sync.selectionRecent180")}</SelectItem>
                                    <SelectItem value="365">{t("translation.sync.selectionRecent365")}</SelectItem>
                                    <SelectItem value="custom">{t("translation.sync.selectionRecentCustom")}</SelectItem>
                                </SelectContent>
                            </Select>

                            {recentSelectionValue === "custom" && (
                                <div className="flex items-center gap-1.5">
                                    <Input
                                        type="number"
                                        min={0}
                                        max={36500}
                                        value={customDaysInput}
                                        onChange={(e) => setCustomDaysInput(e.target.value)}
                                        onBlur={handleCustomDaysBlur}
                                        disabled={disabled}
                                        aria-label={t("translation.sync.selectionRecentCustom")}
                                        className="h-8 w-24 text-xs"
                                    />
                                    <span className="text-xs text-muted-foreground">
                                        {t("translation.sync.selectionRecentDaysUnit")}
                                    </span>
                                </div>
                            )}
                        </div>
                    </div>

                    {!hasFilters && (
                        <p className="text-[11px] text-muted-foreground italic border-t pt-2">
                            {t("translation.sync.selectionEmptyFilterHint")}
                        </p>
                    )}
                </div>
            )}
        </section>
    );
}
