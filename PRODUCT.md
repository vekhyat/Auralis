# PRODUCT.md — Auralis

Product truth for Auralis. Visual/interaction decisions in `DESIGN.md` defer to this file.

## What it is

Auralis is a **Windows desktop app** (Wails, frameless window) for fetching lossless audio.
The user pastes a streaming link (Spotify URL/URI) or types a search, inspects the result,
and downloads tracks, albums, playlists, or artist collections in one action. A persistent
worker starts automatically and handles later requests in the background. Files are fetched in
lossless quality from **Tidal, Qobuz, and Amazon Music**, with extra fallbacks from
**Deezer, Apple Music, and JioSaavn** (community/mirror APIs; no user account inside this app).

- Stack: Go backend + Wails v2 + React 19 + Tailwind CSS v4 + shadcn/radix primitives.
- Config and data live under `~/.auralis`; custom protocol `auralis://`.
- MIT fork of **SpotiFLAC**: original LICENSE copyright and README credit remain; community
  hosts and session User-Agents are unchanged.
- Not affiliated with Spotify, Tidal, Qobuz, Amazon Music, or Deezer. Provider names appear
  only as availability/link labels.

## Core jobs

1. **Find** — paste a Spotify link or search the catalog by text.
2. **Inspect** — see metadata, track list, and per-provider availability before committing.
3. **Download** — start immediately; monitor Downloads; pause/resume/retry/cancel.
4. **Keep tidy** — naming templates, folder structures, embedded tags, lyrics, covers.

## What it is not

- Not a streaming client. No playback beyond short previews.
- No accounts, no paywalls, no donate/support pages, no "other projects" cross-promo.
- Not a theme playground: identity is locked (light/dark skins of one design).

## Audience & scene

A single operator at a desk who wants files, not feeds. The app is a tool surface:
artwork-led browsing, compact track rows, quiet chrome, keyboard-first entry (paste → Enter).

## Brand commitments

- Name: **Auralis**
- Visual world: **artwork library** — cool paper, iron ink, prominent record sleeves, one Prussian-ink accent
- Shell: **download-first library** — titlebar search, a stable destination column, and persistent download controls
- Identity: **locked** — light (default) and dark skins only

## Non-negotiables when changing code

- Wails `App` binding method names do not change (frontend↔backend contract).
- i18n key IDs stay stable; English copy changes only when a label's job changes.
- Provider protocols, community session encryption, provider hosts, and User-Agents
  remain product constants. Download requests use the shared execution lease and persistent store.
- Desktop runtime stays **Go + Wails v2 + Windows WebView2**. The user explicitly
  excludes Electron. React is the UI, not a change of desktop framework.
