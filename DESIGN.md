---
name: Auralis Artwork Library
description: Choose music by its artwork, download once, and keep exploring.
colors:
  primary: "oklch(0.38 0.08 250)"
  paper: "oklch(0.97 0.006 250)"
  ink: "oklch(0.22 0.02 250)"
  card: "oklch(0.985 0.004 250)"
  rule: "oklch(0.86 0.01 250)"
  dark-paper: "oklch(0.19 0.012 250)"
  dark-ink: "oklch(0.92 0.008 250)"
  dark-primary: "oklch(0.74 0.085 250)"
typography:
  heading:
    fontFamily: 'system-ui, "Segoe UI", sans-serif'
    fontSize: "32px"
    fontWeight: 600
    lineHeight: "1.15"
    letterSpacing: "-0.025em"
  body:
    fontFamily: 'system-ui, "Segoe UI", sans-serif'
    fontSize: "14px"
    fontWeight: 400
  data:
    fontFamily: '"Cascadia Mono", ui-monospace, monospace'
    fontSize: "12px"
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
  xl: "12px"
spacing:
  compact: "8px"
  group: "20px"
  section: "32px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    rounded: "{rounded.md}"
    height: "36px"
  search-input:
    backgroundColor: "{colors.card}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    height: "40px"
---

# Auralis Artwork Library

## Overview

Auralis remains a Windows desktop downloader built with Go, Wails v2, React,
and Windows WebView2. The user explicitly excludes Electron. PRODUCT.md owns
product truth; design/download-first.md owns the surface workflow.

The user selected an artwork-led music library. Record sleeves supply color and
recognition; quiet cool-paper chrome supports browsing and file management.
The former text-only catalog, small side inspector, and manual Start Queue
experience are superseded. Light remains default, with a matching dark skin.

## Colors

The CSS custom properties in frontend/src/index.css remain normative. Prussian
ink identifies the active destination, selection, focus, and primary Download.
Dark uses the same token roles. Destructive red marks errors and destructive
controls. Availability uses actual provider names and links. Covers are untinted.
There is no accent, font, or base-color picker.

## Typography

System UI/Segoe UI keeps the interface native without webfont loading. Page and
collection headings are 30–32px semibold; search inspector headings are 24px;
artwork titles/controls are 14px; compact track titles are 13px with 12px metadata.
Monospace is reserved for durations, speeds, identifiers, counts, and logs.
Artwork titles truncate with their full title available; collection headings wrap.

## Layout

- Shell: a 64px draggable titlebar holds search and Windows window controls.
  Controls opt out of Wails dragging. A 184px destination column holds Library,
  Downloads, History, and Settings; Debug/version/help stay in overflow.
- Content scrolls independently between titlebar and the persistent 76px download
  shelf. The content uses 32px padding and a 1600px maximum width.
- Library: recent artwork uses 2/3/5 columns. Search uses 2/3/4 columns and a
  selected-result inspector. These are recently explored items, not a streaming
  feed or a complete representation of files already on disk.
- Collections: a 208px square cover sits beside title/actions/metadata above the
  full-width track list. Artist releases also use artwork shelves.
- Downloads opens on All. Type/status filters refine one workspace; they do not
  create separate workers. Collections expand into track rows.
- Verify desktop at 1440×900 and 1200×720. Below 1100px, navigation narrows to
  152px and search inspection stacks; below 760px, destinations become an icon
  column. These fallbacks do not establish a shipping mobile platform.

## Elevation & Depth

Flat surfaces, subtle active washes, and hairline separators provide hierarchy.
Avoid decorative shadows and glass. A cover lifts 4px on hover over 200ms;
reduced motion disables it. Spinners indicate work. Notifications sit 92px above
bottom so they cannot cover download controls.

## Shapes

Shared radius tokens are 4/6/8/12px. Artwork squares use 8px corners; selection
containers use 12px. Existing utility panels/toasts retain some explicit 2px
corners. Track rows use rules rather than cards. Keep the existing ruled-stamp
Auralis mark and static Lucide icons.

## Components

ArtworkCard/CoverArt: titles, artists, and real metadata accompany square covers.
Missing or failed images use a music-icon fallback. Selection has a quiet accent
wash and clear keyboard focus. InspectorPane is a side inspector for search and
an artwork header above collection tracks. Download is primary; metadata stays a
definition list. Lyrics, cover, source, and folder actions keep their operations.

TrackList keeps Download, Preview, and More visible. More contains secondary
lyrics/cover/source actions. Checkboxes have accessible names including track
identity, keyboard handlers, and a mixed select-all state.

DownloadShelf stays available while browsing. It shows active music, waiting
count, size/speed, pause/resume/cancel, Downloads, and Open Folder. Meaningful
state text is a polite atomic status region; changing MB/s stays outside it.
Saved feedback and failure guidance keep the operator oriented.

Download starts automatically when idle; later requests wait. Pause lets the
current track settle and holds subsequent work, including new requests, until
explicit Resume. Cooldowns also suspend work. Retry preserves saved/skipped
tracks and schedules the worker. Cancel current removes its request after the
transfer settles, preserves completed files, and leaves later requests resumable.
Interrupted saved requests remain available after reopening. Preserve the shared
execution lease, persistence failure notices, protected running items, and backend
binding names. There is no required Start Queue step for a fresh normal download.

History retains download/fetch records. Settings retains stacked configuration
sections and light/auto/dark skins. Debug retains its log view. Naming, metadata,
lyrics, covers, and provider behavior retain their established contracts.

Verification: 39 frontend tests, type checking, lint, and the Windows Wails build
pass. Six browser captures and 13 interaction checks cover both desktop sizes
using production React components with development-only simulated bindings.
Sample track lists/transfers are labeled; preview fixtures are excluded from the
production entry. This does not prove live provider downloads or native close
behavior. The current broader Go suite fails TestAntraAmazonResolvePassesGrantedKey
in existing Amazon mirror code. New workflow copy falls back to English in other
locales; existing translations and key IDs remain.

For You preserves the incumbent artwork-library direction in Operate mode for
the native desktop app. The shell supplies the 32px content inset and quiet
chrome; this surface adds no floating navigation or replacement visual world.
It is off by default and becomes a normal destination after enabling it in
Connections. Artwork-led shelves show translated suggestion reasons. Card and
shelf Download actions use the existing persistent queue; artists without a
downloadable ID open catalog search. More menus dismiss an item, pin an artist,
or ban an artist. A small taste summary shows artists, genres, event count, and
last sync. The settings shortcut and empty-state action deep-link to Connections.

Connections keeps optional, read-only listening-service credentials encrypted
locally. Spotify setup uses the operator's own Client ID, the registered portless
redirect `http://127.0.0.1`, and a dynamic loopback port during authorization.
Last.fm requires an API key and username. Spotify export import accepts a folder,
ZIP, or JSON file. For You and Connections share translated sync phases,
progress, and Cancel; the active state survives navigation until the backend's
completion or cancellation event settles it.

For You verification: 39 frontend tests, locale-key parity, type checking, lint,
targeted Go tests, and build passed. Twenty-two simulated-browser interaction
checks cover 1440x900, 1200x720, and 1402x876, including queue acceptance,
settings navigation, shared sync/cancel, imports, and preference actions. This
scope does not verify live OAuth or native transfers. Captures are recorded in
`.impeccable/review/for-you-1440.png`, `for-you-1200.png`, `for-you-1402.png`,
and `connections-1402.png` under the same directory; they document implemented
output, not an approved comp.

## Do's and Don'ts

- Do use prominent real artwork for browsing and compact rows for tracks.
- Do keep download controls available while the operator explores.
- Do name actions for their result: Download, Resume, Retry, Cancel current.
- Do preserve explicit pause, completed files, and persistence safeguards.
- Don't restore a compulsory Add to Queue → Start Queue workflow.
- Don't migrate to Electron or treat provider changes as styling.
- Don't represent preview fixtures as user data or browser QA as native QA.
