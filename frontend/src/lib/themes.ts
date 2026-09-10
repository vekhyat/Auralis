// Auralis identity is locked: one world (dawn catalog), two skins (light / dark).
// The skins live as CSS custom properties in index.css; there are no selectable
// base colors or accent palettes anymore. Legacy stored values (`theme`,
// `baseColor`) are accepted by these helpers and ignored.

export const BASE_COLOR_NAMES = ["neutral"] as const;
export const ACCENT_COLOR_NAMES = ["prussian"] as const;
export type BaseColorName = (typeof BASE_COLOR_NAMES)[number];
export type AccentColorName = (typeof ACCENT_COLOR_NAMES)[number];
export type ThemeName = BaseColorName | AccentColorName | (string & {});

const TOKEN_KEYS = [
    "background",
    "foreground",
    "card",
    "card-foreground",
    "popover",
    "popover-foreground",
    "primary",
    "primary-foreground",
    "secondary",
    "secondary-foreground",
    "muted",
    "muted-foreground",
    "accent",
    "accent-foreground",
    "destructive",
    "border",
    "input",
    "ring",
];

export function normalizeBaseColorName(_baseColorName: unknown): BaseColorName {
    return "neutral";
}

export function normalizeThemeName(_themeName: unknown, _baseColorName?: unknown): ThemeName {
    return "neutral";
}

export function applyTheme(_themeName?: unknown, _baseColorName?: unknown): void {
    // The identity is defined once in index.css. Older versions painted inline
    // variables onto <html>; strip any leftovers so the locked skins apply.
    const root = document.documentElement;
    for (const key of TOKEN_KEYS) {
        root.style.removeProperty(`--${key}`);
    }
}

/** Kept for call-site compatibility; returns the single locked skin pair. */
export function getThemesForBaseColor(_baseColorName?: unknown): Array<{
    name: ThemeName;
    label: string;
}> {
    return [
        { name: "neutral", label: "Auralis" },
        { name: "prussian", label: "Prussian" },
    ];
}
