# DESIGN.md — Auralis "Dawn Catalog"

Written from the shipped build (`build/bin/Auralis.exe`), not from intent. Product truth
lives in [`PRODUCT.md`](./PRODUCT.md); this file records the visual world as it exists.

---

## Thesis

Auralis is a **catalog desk you type into**. The primary control is an omnibar in the
titlebar; everything below it is ruled paper — dense rows you read, select, and send to
the queue. There is no navigation rail, no centered hero, no cover-card theater.

## World

**Dawn catalog**: cool paper, iron ink, hairline rules. Light is the default scene — a
daytime desk. Dark is the same desk after hours: same hue family, cool slate paper,
never OLED black.

| Token | Light | Dark |
|---|---|---|
| Paper (`--background`) | `oklch(0.97 0.006 250)` | `oklch(0.19 0.012 250)` |
| Ink (`--foreground`) | `oklch(0.22 0.02 250)` | `oklch(0.92 0.008 250)` |
| Rule (`--border`) | `oklch(0.86 0.01 250)` | `oklch(0.31 0.015 250)` |
| Accent (Prussian) | `oklch(0.38 0.08 250)` | `oklch(0.74 0.085 250)` |

One accent. It appears in exactly four jobs:

1. The current destination word in the titlebar.
2. The focused omnibar / focused input.
3. The selected row (ink text on a faint Prussian wash).
4. The primary action button in an inspector.

Status is **words**, not traffic lights: `Done`, `Failed`, `Skipped`, `Pending`,
`Running` set as small mono caps in ink or muted ink; destructive red only for failure.
No success-green, no warning-amber surfaces anywhere.

## Type

- UI: `system-ui, "Segoe UI", sans-serif` — Windows-native, no webfont loading.
- Data (ISRC, times, counts, versions, log): `"Cascadia Mono", ui-monospace`.
- Scale: page titles `text-lg/semibold`; section heads small-caps `text-sm/semibold`
  over a hairline; row titles `text-[13px]/medium`; meta `text-xs` muted.
- No hero type. Nothing larger than `text-lg` outside numeric tool readouts (`text-2xl`).

## Shape & line

- Radius **0–2px** everywhere: inputs, buttons, dialogs, covers, popovers
  (`--radius-md/lg/xl = 2px`, `--radius-sm = 0px`).
- Hairline rules (`1px border-border`) structure every list. Rows are separated by
  rules, not cards. Cards exist as quiet panels only where a container is required.
- No shadows on any surface. Popovers/toasts are separated by rule + contrast alone.
- Covers are sharp squares: 28px in rows, 120px in inspectors.

## Mark

Ink stamp on paper: a square hairline frame containing three ruled rows, the middle one
Prussian — a catalog entry highlighted. Ships as `frontend/public/icon.svg`, regenerated
to `build/appicon.png` and `build/windows/icon.ico` by `frontend/scripts/generate-icon.js`.

## Icons

One family: **Lucide, static**. All looping animated nav/tool icons were removed with
their wrapper components (`ui/home.tsx`, `ui/history-icon.tsx`, `ui/tool-case.tsx`, …).
Icons are 14–16px, muted by default, never animated in loops.

## Shell — the omnibar titlebar

```
[ ▪ Auralis ] [← →] [ paste-or-search omnibar ……… ] [ Library Queueⁿ History Tools Settings ] [⋯] [ _ □ × ]
```

- One fixed 44px bar; the whole bar drags, controls opt out (`--wails-draggable`).
- The **omnibar is the product**: paste a Spotify link or type a query; Enter commits;
  clipboard-paste and clear live inside the field; validation dialogs are shared paper.
- Destinations are **words**, current one in accent with an underline rule; Queue count
  is a mono numeral after the word.
- Back/forward walk catalog history. Reload is not chrome.
- A quiet `⋯` overflow holds version, Debug logs, issue-report dialog, website.
- Window controls are recognizable Windows affordances (min/max/close), close hovers
  destructive.
- Volume and IP/network left the titlebar: preview volume lives in Settings behavior,
  network status in Settings → Status; no flag-and-eye menubar.
- Minimum window 1280×800 (min 1200×720) so the desk split never collapses.

## Library page — catalog + inspector

Empty state: one line of instruction under the omnibar, recent searches as underlined
text links, recent fetches as a **ruled list** (28px cover · title · artist · type word ·
time · text Remove). No cards, no colored type chips, no circular X.

Search state: dense result rows (title · artist · type data right-aligned). Type filters
are words with counts above the rows; filter + sort are inline tools in the pane header.
Clicking a row selects it (accent ink); the selected entry is inspected in the right
pane; double-click fetches directly.

Fetched item: track lists (or discography rows) fill the left pane; the inspector pane
on the right holds a modest 120px sharp cover, name at readable scale, metadata as a
definition list over hairlines, and actions as a text/button row (Queue, Lyrics, Cover,
Folder…). Availability renders as provider-name links (Tidal/Qobuz/Amazon) with small
uncolored marks; not-found is a quiet word. Explicit is spelled out, never a red E.

One row grammar everywhere: `CatalogRow` powers recents, search results, discography
rows; `TrackList` renders the same grammar as a table for albums/playlists/queue-scale
lists (# · thumb 28px · title (+Explicit, status word) · album · dur · plays · actions).

## Secondary surfaces

- **Queue:** full-width ruled table; type filters as words with counts; status column is
  words (Running in accent, Failed in destructive, rest in ink/muted). Expandable items
  keep their sub-track sheets.
- **History:** Downloads/Fetches as text filters; same ruled table grammar; format and
  timestamps in mono.
- **Tools:** grouped rows — title, one-line description, chevron. Group filters are
  words. No saturated tiles, no emerald/violet/rose blocks.
- **Settings:** stacked sections with hairline headings (General · Download path ·
  Download source · Custom instances · Naming · File management · Metadata · Status).
  No tab strip, no base/accent color dots, no Google-fonts menu. Theme control is the
  single light/auto/dark mode select. Stored legacy `theme`/`baseColor`/`fontFamily`
  values are ignored at apply time; first run defaults to **light**.
- **Debug:** monospace log on a bordered paper card; levels differentiated by ink weight,
  errors in destructive red.
- **Toasts:** popover paper, ink text, muted icon; error icon red. No pastel slabs.
- **Cooldown:** a ruled notice strip on the paper near the top of content.
- **Download progress:** docked bottom-right as a quiet status line (mono MB/s).
- **Scrollbars:** thin, trackless, rule-colored thumbs; never brand-colored.

## Motion

Functional only: spinners, fade/slide of toasts and banners, hover tints. No looping
animations, no ping badges, no animated nav art. Reduced-motion honored globally.

## Identity lock

Two skins, one system. `themes.ts` collapses to compatibility stubs; the skins live as
CSS custom properties in `index.css`. There is no accent picker, no font picker, and no
code path that paints theme variables onto `:root` anymore.

## Old → new map

| SpotiFLAC DNA | Auralis now |
|---|---|
| `Sidebar.tsx` rail | deleted; destinations are titlebar words (`pages.ts`) |
| `Header.tsx` hero + tagline + version badge | deleted; version sits in the overflow menu |
| Centered `max-w-4xl` home column | full-width desk split |
| `SearchBar.tsx` field + Fetch button | `OmnibarSearch` in titlebar; Enter fetches |
| Typing placeholders | static hint copy |
| 130px recent-fetch cards, type chips, red X | ruled recents list via `CatalogRow`, text Remove |
| 192px cover hero, huge titles, red E | 120px sharp inspector cover, `text-lg` names, spelled Explicit |
| Bordered table, 40px thumbs | ruled rows, 28px thumbs, status words |
| Volume/IP menubar in titlebar | removed; network in Settings → Status |
| Colored tool tiles | grouped rows |
| Flush settings tab strip | stacked hairline sections |
| Scroll-top FAB | killed |
| Teal rounded waveform mark | ruled-stamp mark, Prussian on paper |
| 17 accents × 7 bases, Geist/Google fonts | locked two-skin dawn catalog, system UI + Cascadia Mono |

## Verification

- `tsc -b` clean; `eslint` error count equal to pre-change baseline (all remaining
  findings are pre-existing patterns elsewhere in the codebase; all new files lint clean).
- `go test ./backend` passes (backend untouched).
- `wails build` → `build/bin/Auralis.exe`.
- Side-by-side against SpotiFLAC: different skeleton (no rail, no hero, no card grid),
  different palette family, different type system, different mark. Not matchable at a glance.
