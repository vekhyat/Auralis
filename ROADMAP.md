# ROADMAP.md: Auralis feature roadmap

A planning document for the next big features in Auralis. For each feature it covers what it is,
the sub-features, how to build it on top of the current Go/Wails codebase, the risks, and ideas
for improving it later.

> Status: **proposal**. Nothing here is committed yet. Several features conflict with current
> statements in `PRODUCT.md` (see [§8 Product conflicts](#8-product-conflicts-to-resolve)), so
> that file has to be updated before any of them ship.

---

## Contents

1. [Taste-aware recommendations from streaming accounts](#1-taste-aware-recommendations-from-streaming-accounts)
2. [Player-ready library management (Poweramp & friends)](#2-player-ready-library-management-poweramp--friends)
3. [Android phone connection & sync](#3-android-phone-connection--sync)
4. [iPod sync without iTunes](#4-ipod-sync-without-itunes)
5. [Auralis for Android](#5-auralis-for-android)
6. [Shared architecture](#6-shared-architecture)
7. [Phased delivery plan](#7-phased-delivery-plan)
8. [Product conflicts to resolve](#8-product-conflicts-to-resolve)
9. [Open questions](#9-open-questions)

### What already exists and can be reused

| Existing piece | Where | Reused by |
|---|---|---|
| Library index (path ↔ ISRC ↔ Spotify ID, bbolt) | `backend/library_index.go` | "Already owned?" filtering, playlist matching, sync diffing |
| Tag read/write (taglib via wazero, FLAC/MP3/M4A) | `backend/tagging.go`, `backend/filemanager.go` | Library Doctor, device profiles, iPod DB building |
| Filename/folder templates + batch rename | `backend/filemanager.go` (`PreviewRename`, `RenameFiles`), settings `folderTemplate` | Library re-organisation |
| M3U8 writer (relative paths) | `app.go` `CreateM3U8File` | Playlist manager, device playlists |
| FFmpeg convert / resample | `backend/ffmpeg.go`, `backend/resample.go` | Transcoding for phones and iPods |
| ReplayGain, lyrics, covers, MusicBrainz enrich | `backend/replaygain.go`, `lyrics*.go`, `cover.go`, `musicbrainz.go` | Library Doctor auto-fixes |
| Spotify metadata (anonymous web token) | `backend/spotify_metadata.go`, `spotify_totp.go` | Discography gaps, new-release watching |
| Persistent queue / execution lease | `backend/queue_db.go`, download execution | Queuing recommended items, sync jobs |
| On-demand tool download pattern | `DownloadFFmpeg` binding | Fetching `adb` the same way |
| Toast notifications (`go-toast`, indirect dep) | Wails runtime | New-release and "device connected" notifications |

---

## 1. Taste-aware recommendations from streaming accounts

**Goal:** the user connects one or more listening sources. Auralis builds a local taste profile
and suggests tracks, albums, and artists to download, leaving out anything already in the library.

### 1.1 Account connections

| Source | Auth | Useful data | Notes |
|---|---|---|---|
| **Spotify** | OAuth 2.0 Authorization Code **with PKCE** (no client secret needed) | Top artists/tracks (short/medium/long term), saved tracks, saved albums, followed artists, playlists, recently played | Spotify's recommendations, related-artists, and audio-features endpoints are **not available to new apps** (since Nov 2024). Development-mode apps only work for a small allow-list of users, and Spotify keeps tightening that. See the risks below. |
| **Last.fm** | API key, plus optional web auth for private data | Full scrobble history, top artists/tracks/albums by period, loved tracks, **similar artists/tracks**, tag charts | Easiest and most open source. Fills the gap left by Spotify's deprecated recommendation endpoints. |
| **ListenBrainz** | User token (pasted from profile page) | Listens, collaborative-filtering recommendations, similar artists, fresh releases | Open data and MusicBrainz IDs, which match the existing `musicbrainz.go`. |
| **Apple Music** | MusicKit: developer token (JWT signed with a MusicKit key, which needs a paid Apple Developer account) plus a user token from MusicKit JS in a webview | Library songs/albums, recently played, heavy rotation, ratings, Apple's own recommendations | Needs a $99/yr developer account and a key that must not ship inside the binary. See the no-account fallback below. |
| **Tidal / Deezer / YouTube Music** | Varies (Tidal has a newer developer API; Deezer has restricted new app registrations; YT Music has no official API) | Favourites, playlists | Lower priority. Check what each platform currently allows before doing any work here. |

**Fallbacks that need no API at all** (these are the most robust and should be offered first-class):

- **Spotify "Download your data"** → *Extended streaming history* JSON. This is the complete play
  history with `ms_played`, so it is better than the API for taste modelling and is unaffected
  by developer-mode limits.
- **Apple privacy export** (privacy.apple.com) → Apple Music play-activity CSVs.
- **Apple Music / iTunes `Library.xml` export** → library plus play counts and ratings.
- **Local library**: what is already downloaded counts as taste too.
- **iPod play counts / Rockbox `.scrobbler.log`** (see §4). Devices feed back into taste.

#### Implementation

- **Loopback OAuth flow:** start a one-shot HTTP listener on `http://127.0.0.1:<random port>/callback`
  and open the system browser to the authorize URL. Spotify requires a loopback **IP literal**
  (`127.0.0.1`), not `localhost`. Use PKCE (`code_verifier`/`code_challenge`, S256) and validate `state`.
  Handling `auralis://` as a redirect through the existing protocol handler
  (`protocol_windows.go`) is an alternative, but loopback is more reliable.
- **Bring-your-own client ID:** in Settings → Connections, the user pastes their own Spotify client ID
  from developer.spotify.com. This sidesteps the per-app user cap. If Spotify revokes a client
  ID, only that one user is affected. Ship a short guided setup with screenshots.
- **Token storage:** never store tokens as plaintext in `~/.auralis`. Encrypt them with Windows
  **DPAPI** (`CryptProtectData` via `golang.org/x/sys/windows`), scoped to the current user,
  and reuse the ACL hardening in `file_acl_windows.go`. Refresh tokens rotate automatically.
- **Sync model:** an incremental pull per source, stored in a new bbolt bucket (`taste.db`).
  Keep raw events (plays, saves, follows) separate from the derived profile so the
  profile can be recomputed when the algorithm changes.
- **Read-only scopes only** (`user-top-read`, `user-library-read`, `user-read-recently-played`,
  `playlist-read-private`, `user-follow-read`). Auralis never writes to the user's accounts.

### 1.2 Taste profile

- **Entities:** artists, albums, tracks, and tags/genres (from Last.fm tags, MusicBrainz genres,
  and embedded `GENRE` tags), plus era (release decade) and optional audio traits (tempo, key, and loudness from
  the existing analyzer pages).
- **Affinity scores:** a weighted sum of signals with time decay, e.g.
  `affinity(artist) = Σ weight(signal) × e^(−age/τ)`. Example weights: top-artist rank > saved album >
  saved track > play > followed. Plays under ~30 s count as skips and give a small negative signal.
- **Profile views:** "Core" (long-term), "Current" (last 4 weeks), and "Rising" (accelerating).
  Show them in a small "Your taste" panel so the user can see *why* they get each suggestion.
- **Manual controls:** pin or ban an artist, mute a genre, and an "I'm over this" decay boost.

### 1.3 Recommendation sources (candidate generation)

Ordered by value and how easy they are to build:

1. **Liked but not owned:** saved Spotify tracks/albums, Last.fm loved tracks, and Apple library
   items with no match in the library index (ISRC first, then the normalised artist + title + duration
   fallback). This is the most immediately useful list and needs no "intelligence".
2. **Complete the album:** albums where the user has liked or played ≥ N tracks but doesn't own the whole album.
3. **Discography gaps:** for artists above an affinity threshold, list their albums/EPs that are
   missing from the library. This reuses the Spotify artist metadata already fetched in `spotify_metadata.go`.
4. **New-release radar:** a background check (daily) of top and followed artists for releases newer
   than the last check, with a Windows toast. "Auto-queue new releases from pinned artists" is an
   optional toggle.
5. **Similar artists:** Last.fm `artist.getSimilar` and ListenBrainz similar-artists, seeded from top
   artists. Suggest each candidate's most popular album or "best entry point".
6. **Tag/genre exploration:** Last.fm `tag.getTopAlbums` for the user's strongest tags, filtered by era.
7. **Collaborative (ListenBrainz CF):** use the server-side recommendations directly.
8. **(Stretch) Sound-alike:** local audio embeddings for "more like this track". Spotify audio-features
   are gone, so this has to be computed locally (e.g. an ONNX model run off the UI thread). It is
   heavy, so leave it for last.

### 1.4 Ranking & presentation

- **Score** = Σ(seed affinity × candidate similarity) × novelty bonus × popularity dampening.
  Before ranking, filter out owned items (library index), queued or downloaded items (history), dismissed
  items, and banned artists.
- **Diversity:** cap items per artist per shelf, and use maximal-marginal-relevance ordering so one
  artist cannot flood the page.
- **Explainability:** every card shows a reason, e.g. *"Because you play Radiohead a lot"*,
  *"You liked 6 tracks from this album"*, *"New from an artist you follow"*.
- **UI (DESIGN.md compliant):** a new **For You** destination in the left column, with artwork-led
  shelves: "Liked, not downloaded", "Finish these albums", "New releases", "Because you
  listen to X", "Explore <tag>". Each card gets one-click Download, ⋯ → Dismiss / Not interested in artist.
  "Download shelf" queues the whole row through the existing queue.
- **Feedback loop:** downloads count as positive signal, dismissals as negative. Both are fed back into the
  affinity model.

### 1.5 Mirror mode (playlist sync)

- Watch selected Spotify playlists, including Liked Songs. For Spotify playlists, diff by
  `snapshot_id`. When tracks are added, queue them automatically. When tracks are removed, optionally
  remove them from the matching local `.m3u8` (and never delete files without asking).
- Regenerate the playlist's `.m3u8` after each sync so it stays player-ready (§2).

### 1.6 Risks

- **Platform terms:** Spotify's developer policy restricts what API data may be used for, and
  using it to drive downloads from other sources may get a client ID revoked. BYO client IDs and the
  offline data-export import keep this risk small and contained to one user. Keep the integration read-only.
- **API churn:** Spotify has removed endpoints and tightened access more than once. Every source
  should sit behind a `TasteSource` interface (§6) so that losing one source degrades the feature
  rather than breaking it.
- **Apple developer token:** it can't be embedded safely, so Apple Music either needs the user's own key or a
  small token-minting service. The Library.xml and privacy-export paths avoid this entirely.

### 1.7 Future improvements

- A local "Auralis Wrapped" yearly or monthly report built from all sources plus device play counts.
- Scrobble device plays (iPod/Rockbox logs) to Last.fm/ListenBrainz on sync.
- "Fill a 32 GB device with my taste" (combines with §3/§4 capacity planning).
- Natural-language shelf filters ("upbeat 2000s indie I don't own yet") over the local profile.

---

## 2. Player-ready library management (Poweramp & friends)

**Goal:** a library that looks correct in **Poweramp**, Android's MediaStore-based players
(Samsung Music, Google Files, etc.), foobar2000, MusicBee, Plex/Jellyfin/Navidrome, and
iPod/Rockbox, with no split albums, missing covers, or broken playlists.

### 2.1 Why libraries look wrong in players

Most "broken library" symptoms come from a handful of tag and file issues:

| Symptom in player | Usual cause | Fix |
|---|---|---|
| One album shows up as several | `ALBUMARTIST` missing or inconsistent across tracks; featured artists in `ARTIST` without `ALBUMARTIST` | Set one `ALBUMARTIST` for the whole release |
| Compilation split into 20 "albums" | No `ALBUMARTIST=Various Artists` / `COMPILATION=1` | Set both |
| Disc 2 tracks interleave with disc 1 | Missing `DISCNUMBER`/`DISCTOTAL` | Write them; optionally use `Disc 1/`, `Disc 2/` folders |
| Wrong track order | Missing or `"1/12"`-style track numbers that the player can't parse | Normalise `TRACKNUMBER` and `TRACKTOTAL` |
| No or slow artwork | No embedded art, a huge PNG (5–10 MB), or no folder art | Embed a ≤1000 px JPEG plus `cover.jpg` in the folder |
| Duplicate artists ("A & B", "A, B") | Artist separator conventions | Use multi-value artist tags; Poweramp can split on configured separators, MediaStore can't |
| Playlist imports empty | Absolute Windows paths (`C:\...`), backslashes, wrong encoding | Write UTF-8 `.m3u8` with relative forward-slash paths |
| Files skipped on phone/SD | Characters illegal on FAT/exFAT/Android storage (`: * ? " < > \|`), trailing dots/spaces, paths > 255 bytes | Sanitise for the target |
| Lyrics not shown | Lyrics only embedded in a frame that player doesn't read | Also write `.lrc` sidecar files (Poweramp reads them) |

### 2.2 Library Doctor (scan → report → fix)

- **Scan** a library root using the existing index walk (`collectLibraryIndexEntries`), extended to
  capture full tags, embedded-art size, and folder-art presence.
- **Rules engine:** each rule has an `id`, a `severity`, a `detect(album|track)` step, and an optional `fix()`. Starting rules:
  - inconsistent or missing `ALBUMARTIST` within an album folder
  - missing track/disc numbers, gaps in track numbering
  - missing/oversized/non-square cover; no `cover.jpg`
  - mixed formats or bit depths within one album
  - likely duplicates: same ISRC, same artist+title+duration ±2 s, or (stretch) the same
    Chromaprint/AcoustID fingerprint via `fpcalc`
  - illegal or problematic filenames for the selected target profile
  - files outside the template structure
  - orphans: empty folders, stray `.jpg`/`.lrc` with no audio, zero-byte or truncated audio (reuse
    `download_validation.go` integrity checks)
- **Fix with preview:** every fix produces a plan (a diff of tags and paths) shown before it is applied, the
  same way `PreviewRename` works today. Fixes use `TagFileMerge` so ReplayGain and MBIDs are preserved.
- **Undo journal:** keep an append-only log of every change (old/new tags, old/new path) so
  "Undo last fix batch" is always possible. Path moves go through `MoveLibraryIndexFile` so the
  index stays in sync.
- **UI:** a "Library health" page with a score, grouped issues, per-album drill-in, and
  "Fix all safe issues".

### 2.3 Target profiles

These are presets that bundle all player-specific conventions. A profile is used both for the **main
library** and for **device exports** (§3/§4).

| Profile | Folder layout | Cover | Artist handling | Playlists | Extra |
|---|---|---|---|---|---|
| **Poweramp** | `{album_artist}/{album}/{disc}{track}. {title}` | embedded ≤1000 px JPEG + `cover.jpg` | multi-value or `;`-separated | `.m3u8` relative, in `Playlists/` | `.lrc` sidecars, ReplayGain tags |
| **Android MediaStore** (Samsung Music, etc.) | same | embedded JPEG | single primary artist + `ALBUMARTIST` | `.m3u8` | conservative filenames |
| **Rockbox / iPod** | `{album_artist}/{album}/…` | embedded JPEG + `cover.jpg` (Rockbox can't read progressive JPEG, so use baseline) | primary artist | `.m3u8` | see §4 |
| **Plex/Jellyfin/Navidrome** | `{album_artist}/{album} ({year})/…` | `cover.jpg`/`folder.jpg` | MusicBrainz IDs preferred | server-managed | MBIDs from `musicbrainz.go` |
| **Custom** | user templates (existing settings) | user choice | user choice | user choice | |

### 2.4 Playlist manager

- Create, edit, and reorder local playlists. Store them as `.m3u8` (the canonical file) plus an internal record of
  ISRCs/Spotify IDs, so a playlist can be re-resolved after files move.
- **Import** Spotify, Apple, or Last.fm playlists and match them against the library. Show matched and missing tracks, then
  "Download missing" queues the gaps.
- **Export modes:** relative to the playlist file (default), relative to the music root, or
  device-absolute (`/storage/emulated/0/Music/...` or `/sdcard/...`) for players that need it.
- Harden `CreateM3U8File`. It currently falls back to an absolute Windows path when
  `filepath.Rel` fails (e.g. a different drive). In device-export mode that should be an error or a
  skipped entry instead.
- Add `#EXTINF:<seconds>,<artist> - <title>` lines so players can show entries even before scanning.

### 2.5 Implementation notes

- New package `backend/library/` with `doctor.go`, `rules_*.go`, `profiles.go`, `journal.go`,
  and `playlists.go`. New Wails bindings must be **additive only** (existing binding names are a
  non-negotiable contract).
- Watch folders with `ReadDirectoryChangesW` (or `fsnotify`) for incremental index updates instead
  of full rescans.
- Run long scans as cancellable jobs with progress events (reuse the `progress.go` patterns).

### 2.6 Future improvements

- An "album view" editor (a lightweight MusicBrainz-Picard-style tagger) to retag a whole release from a
  MusicBrainz release ID in one click.
- AcoustID-based identification for untagged files.
- Optionally export a `.nomedia` file into non-music folders so Android galleries ignore cover folders.

---

## 3. Android phone connection & sync

**Goal:** plug in (or pair over Wi-Fi) an Android phone. Auralis recognises the device, shows its
music folder, organises it with a target profile (§2.3), and keeps it in sync with the PC library.

### 3.1 Connection methods

| Method | Setup for user | Speed | Capabilities | Implementation |
|---|---|---|---|---|
| **MTP** (USB "File transfer") | None | Slow with many small files | List/read/write/delete; moves and timestamps unreliable | Windows Portable Devices (WPD) COM API via `go-ole` (already an indirect dependency) |
| **ADB** (USB debugging) | Enable developer options | Fast | Everything: push, pull, `mv`, shell, trigger media rescans, free-space query | Download `platform-tools` on demand (like FFmpeg today; check Google's redistribution terms), talk to the adb server |
| **Wireless ADB** (Android 11+) | Pair once with a code | Fast-ish | Same as ADB, no cable | Same as ADB with `adb pair`/`adb connect` |
| **Companion app** over Wi-Fi | Install Auralis Android app (§5) | Fast | Full control, rescans, two-way playlists | Local HTTP API with pairing; ties into §5 |
| **Syncthing / folder sync** | User already runs Syncthing | n/a | Auralis just maintains a "device-ready" export folder | Zero device code. The cheapest option to ship first. |

**Recommendation:** ship the **export-folder + Syncthing** path first (zero device code), then
**MTP** (no setup for the user), then **ADB** as the "fast/pro" option. The companion app comes later.

### 3.2 Device detection & identity

- Detect arrival and removal by subscribing to device notifications (`RegisterDeviceNotification` for the WPD
  interface GUID and volumes), or by polling WPD enumeration every few seconds as a first version. For ADB,
  use `adb track-devices`.
- Identify a device by serial number and model, and store a **per-device profile**: friendly name,
  target folder (internal or SD card), target profile, format policy, selection rules, and last-sync
  manifest version.
- Show a "Phone connected: Pixel 8 · 41 GB free" toast and a **Devices** destination in the left column.

### 3.3 "Format the music folder on the phone"

There are two models. **Default to model A.**

- **A. PC is the source of truth (one-way mirror).** Auralis builds the phone's music folder from
  the PC library through the device profile. It is safe, predictable, and works with every transport.
- **B. Adopt an existing phone library.** For phones that already hold music:
  1. Scan the phone folder (read tags by pulling files, or only the first and last few KB where formats allow).
  2. Match each file against the PC library (ISRC → tags → fingerprint).
  3. Offer to **import unknown files to the PC**, then switch the device to model A.
  4. Alternatively, reorganise in place: rename and move on the device, and retag by pull → tag → push.
     This is slow over MTP but fine over ADB.

### 3.4 Sync engine

- **Manifest on device:** `Music/.auralis/manifest.json` records, for each file, the source library
  path, content hash, size, profile version, and transcode settings. The diff uses the manifest rather than
  MTP timestamps (which are unreliable).
- **Plan → preview → execute:**
  - The planner computes `add / update / move / delete / keep` operations, detects moves by hash so files aren't
    re-copied, and checks free space **before** starting.
  - The preview groups changes by album with totals ("+312 tracks, 9.4 GB; −12 tracks").
  - The executor is resumable (journal per operation), runs with bounded concurrency, verifies size after copy,
    and supports cancellation (reuse `download_cancel.go` patterns).
- **Selection rules:** the whole library; chosen artists, albums, or playlists; "recently added (90 days)";
  "For You" picks (§1); "fill to X GB by affinity".
- **Format policy per device:** keep lossless, or transcode FLAC → **Opus 160 / AAC 256 / MP3 V0**
  using FFmpeg. Cache transcodes by `(source hash, settings)` so re-syncs are instant. Carry over
  tags, art, ReplayGain, and lyrics.
- **Playlists:** regenerate `.m3u8` files for the device paths into `Music/Playlists/`.
- **After sync:** trigger a rescan. Over ADB, use a media-scan broadcast on older Android or ask
  Poweramp to rescan through its public intent API. Over MTP, rely on MediaStore or the player's own
  scanner and show a hint.

### 3.5 Risks

- MTP behaviour differs between vendors (Samsung, Xiaomi, etc.). It needs a device test matrix and
  generous timeouts and retries.
- Android scoped storage restricts which folders a companion app can write to (relevant for §5),
  but ADB and MTP are unaffected.
- Never delete on the device without showing the plan first. Default "delete" to "move to `.auralis/trash`".

### 3.6 Future improvements

- Two-way playlists: pull playlists created on the phone (Poweramp can export `.m3u8`) back to the PC.
- Sync several devices with different profiles (car USB stick, DAP, phone).
- Support plain USB sticks, SD cards, and DAPs (FiiO, Sony, etc.) through the same engine. They are just mass-storage
  targets with their own profile.

---

## 4. iPod sync without iTunes

**Goal:** connect a classic-era iPod and add, remove, and organise music plus playlists and artwork
directly from Auralis, without iTunes.

### 4.1 Two modes

| Mode | Devices | Difficulty | What Auralis does |
|---|---|---|---|
| **Rockbox mode** | Any iPod running Rockbox (also iFlash/SD-modded units) | **Easy** | Treat it as a mass-storage target (§3 engine): copy files with the Rockbox profile and write `.m3u8` playlists. Rockbox plays FLAC natively. |
| **Stock firmware mode** | iPod Classic (all), Video 5G/5.5G, mini, nano 1G–5G, shuffle | **Hard** | Write Apple's `iTunesDB` and `ArtworkDB` ourselves and copy files into `iPod_Control/Music/Fxx/` |

Out of scope: **iPod touch** (iOS: needs AFC via libimobiledevice plus Apple's SQLite media
library) and probably **nano 6G/7G** at first (they use the newer SQLite/`iTunesCDB` databases and a
hashAB checksum). These can be revisited later.

### 4.2 Stock firmware: how it works

- **Detection:** the iPod mounts as a USB mass-storage drive (disk mode) with a hidden
  `iPod_Control` folder. Read `iPod_Control/Device/SysInfo`. On newer models, read
  `SysInfoExtended` (via a SCSI inquiry with `DeviceIoControl` if the file isn't present) to get the
  model, the **FireWire GUID**, and supported artwork formats.
- **Mac-formatted (HFS+) iPods** aren't readable on Windows. Detect this and explain that the user can restore or format to FAT32.
- **Audio formats:** stock firmware plays MP3, AAC (M4A), **ALAC**, WAV, and AIFF, but **not FLAC**. Transcode
  FLAC → ALAC (lossless) or AAC 256 (space). For compatibility, resample hi-res to 16-bit/44.1 kHz
  with the existing `resample.go`.
- **The database (`iPod_Control/iTunes/iTunesDB`):** a little-endian binary tree of chunks
  (`mhbd` → `mhsd` → `mhlt` track list → `mhit` tracks with `mhod` string children; `mhlp` → `mhyp`
  playlists → `mhip` items). The format is well documented (wikiPodLinux archives, libgpod source).
  Write a **pure-Go reader/writer** (`backend/devices/ipod/itunesdb`):
  1. Parse the existing DB so tracks added elsewhere aren't destroyed, and keep unknown fields byte-for-byte.
  2. Add, remove, and update tracks and playlists, including the master playlist and podcast-safe handling.
  3. Serialise and write it atomically (temp file + rename), always **backing up** the previous DB first.
- **Checksums:** iPod Classic 6G/7G and nano 3G/4G reject DBs without a valid **hash58** computed from
  the FireWire GUID (algorithm public, implemented in libgpod). nano 5G uses **hash72**, which needs a
  `HashInfo` derived from an iTunes-written DB. Support hash58 in v1 and treat hash72 as later.
- **Artwork:** the iPod doesn't read embedded art. It needs `iPod_Control/Artwork/ArtworkDB` plus
  `.ithmb` files containing thumbnails in **model-specific sizes and pixel formats** (e.g. RGB565 at
  several sizes), so generate them with `golang.org/x/image`, which is already a dependency.
- **Play counts and ratings:** read the `Play Counts` file (and the on-the-go playlists) the iPod writes after
  playback, merge it into the DB, and feed it to the taste engine (§1) or scrobble it to Last.fm.
- **Eject:** after writing, flush and request a safe eject (`CM_Request_Device_Eject` in `cfgmgr32`).

**Alternative to writing it ourselves:** bind **libgpod** (C, LGPL, used by gtkpod, Rhythmbox, and
Strawberry). It covers more models, but building it and its GLib dependency chain on Windows through
cgo is painful, and LGPL needs dynamic linking. Recommended path: a pure-Go implementation, using libgpod
**as the reference** and for cross-checking the bytes it writes in tests.

### 4.3 UI

- The iPod appears under **Devices** with its model artwork, capacity bar, and firmware mode.
- Library browser with drag-to-device, the same selection rules as §3.4, and a "Sync playlists" checklist.
- A "Doctor" for the iPod: orphaned files with no DB entry, DB entries with missing files, and corrupt DB recovery
  from the backup.

### 4.4 Risks

- Writing a broken iTunesDB makes the iPod show an empty library (recoverable, but scary). Mitigate with backups, a read-back
  verification after writing, and a golden-file test corpus from real devices.
- The model matrix is large, so ship with an explicit "verified models" list and a "use at your own risk" flag for the rest.
- Ageing hardware: many units have failing HDDs. Show SMART-ish warnings if write errors occur.

### 4.5 Future improvements

- One-click "Install Rockbox" helper (download the official build for the detected model).
- Smart iPod fill: "fill 80 GB, prioritising high affinity, at ALAC for top albums and AAC elsewhere".
- Gapless metadata (`gapless` fields in `mhit`) and sound check (iPod's ReplayGain equivalent),
  calculated from the existing `replaygain.go` values.

---

## 5. Auralis for Android

> Interpretation: an Android version of Auralis itself. If you instead meant "sync an iPod from
> an Android phone", see §9.

### 5.1 Recommended path: in two steps

1. **Companion app (first):** the phone pairs with the desktop over the local network.
   - It is a **sync receiver** for §3, which removes the need for MTP/ADB.
   - It acts as a **remote control**: browse "For You", search, and tap Download, and the desktop does the work.
   - It provides two-way playlists and reads local play history (if the app is also the player or reads Poweramp exports).
2. **Standalone app (later):** downloading, tagging, and library management directly on the phone.

### 5.2 Technical approach

- **Core:** compile the Go backend as an Android library with **gomobile bind** (`.aar`). Most of
  `backend/` is portable Go. Tagging already uses taglib via **wazero (pure Go, no cgo)**, which
  is a big advantage on Android.
- **UI:** reuse the React frontend inside an Android `WebView`, with a small JS bridge that mirrors the
  Wails binding surface (same method names, so the frontend changes stay minimal). A native
  Kotlin/Compose shell handles permissions, notifications, and the foreground service. (Wails v2 has no
  mobile target, so this hosting layer is needed anyway.)
- **Windows-specific pieces to replace:**
  - Verification: Edge/CDP (`verify_cdp.go`, `verify_window_windows.go`) → an in-app `WebView` page.
  - FFmpeg: ship a per-ABI FFmpeg build (ffmpeg-kit has been retired, so build your own) or use
    Android Media3 for transcodes where possible.
  - File dialogs and Explorer integration → the Storage Access Framework.
- **Storage:** with scoped storage (Android 11+), write audio through **MediaStore** (`Music/Auralis/…`)
  or a user-picked SAF tree. Avoid `MANAGE_EXTERNAL_STORAGE`.
- **Background work:** downloads run in a foreground service (Android 14+ needs a declared type such as
  `dataSync`, and newer versions cap its runtime) or through WorkManager with resumable chunks.
- **Distribution:** GitHub Releases plus F-Droid/Obtainium. The Play Store isn't a realistic target for this app.

### 5.3 Future improvements

- Built-in player with Poweramp-grade library views, so taste data stays local.
- Android Auto / car USB export profile.

---

## 6. Shared architecture

```
backend/
  credentials/       DPAPI-encrypted token store (Windows), keyring abstraction for later ports
  taste/             TasteSource interface, importers (spotify, lastfm, listenbrainz, apple xml/export),
                     profile model, candidate generators, ranker
  library/           doctor + rules, target profiles, undo journal, playlist manager
  devices/
    detect/          WPD + volume + adb device watcher → DeviceInfo events
    mtp/             WPD COM transport
    adb/             adb server client + platform-tools fetcher
    massstorage/     plain drive transport (USB stick, SD, Rockbox iPod)
    ipod/            SysInfo, itunesdb (read/write), artworkdb, hash58, playcounts
  syncengine/        planner, manifest, transcode cache, resumable executor
```

Core interfaces:

```go
// A place music comes from for taste modelling.
type TasteSource interface {
    ID() string
    Connected() bool
    Pull(ctx context.Context, since time.Time) ([]TasteEvent, error)
}

// Anything Auralis can sync to: phone (MTP/ADB), USB stick, Rockbox or stock iPod.
type SyncTarget interface {
    Info() DeviceInfo
    List(ctx context.Context, root string) ([]RemoteEntry, error)
    Put(ctx context.Context, localPath, remotePath string, progress func(int64)) error
    Move(ctx context.Context, from, to string) error
    Delete(ctx context.Context, remotePath string) error
    FreeSpace(ctx context.Context) (int64, error)
    Commit(ctx context.Context) error // iPod: write iTunesDB/ArtworkDB; Android: trigger rescan
}
```

The stock-firmware iPod is a `SyncTarget` whose `Put` places files into `iPod_Control/Music/Fxx` and
whose `Commit` writes the database. This lets one planner/executor serve every device type.

Cross-cutting rules:

- New Wails bindings are **additive**. Existing names don't change.
- All long operations are cancellable jobs with progress events and persistent state, so they survive an app restart.
- All destructive operations go through plan → preview → journal → execute.
- New i18n keys for every string, keeping locale parity (`locale-parity.test.ts`).

---

## 7. Phased delivery plan

| Phase | Scope | Size | Why this order |
|---|---|---|---|
| **0 – Foundations** | Credential store, full-tag library scan, job/progress framework, `SyncTarget` interface, "Devices" and "For You" destinations (empty states) | M | Everything else depends on it |
| **1 – Player-ready library** | Library Doctor (core rules + undo), target profiles, playlist manager + `.m3u8` hardening, export-to-folder | L | Lowest risk, immediately useful, and needed by phone and iPod sync |
| **2 – Taste v1** | Last.fm + Spotify (PKCE, BYO client ID) + Spotify data-export import; "Liked, not owned", "Finish these albums", "Discography gaps", new-release radar; playlist mirror mode | L | Highest-value recommendation shelves need no ML |
| **3 – Android sync** | Export folder + Syncthing guide → MTP transport → ADB transport; manifest diff, transcode cache, rescan triggers | L | MTP needs no setup on the phone |
| **4 – iPod** | Rockbox mode (quick win, reuses phase 3) → stock firmware for Classic/Video (iTunesDB + hash58 + ArtworkDB) → play-count import | XL | Hardest reverse-engineered format, so it goes last |
| **5 – Taste v2** | Similar-artist and tag shelves, ListenBrainz, Apple Library.xml/privacy export, device play counts, diversity ranking | M | Builds on phases 2–4 data |
| **6 – Android app** | Companion (sync receiver + remote control) → standalone | XL | Biggest new surface. Phases 1–3 de-risk it |

Testing per phase: golden-file tests for tag and DB writers (byte-exact round-trips), fake
`SyncTarget` for the planner/executor, a recorded-fixture HTTP client for taste sources, and a
small real-device matrix (2–3 Android vendors, iPod Classic 7G + Video 5.5G) before each release.

---

## 8. Product conflicts to resolve

`PRODUCT.md` currently states things that these features change:

- **"No accounts"**: §1 adds *optional, read-only connections to third-party accounts*. Auralis
  itself still has no account. Reword it to something like *"No Auralis account. Optional read-only
  connections to listening services, stored locally."*
- **"A single operator at a desk who wants files, not feeds"**: "For You" is a feed. Frame it as
  a *download backlog* ("things you probably want as files") rather than an endless discovery feed, and keep it off by default.
- **"Windows desktop app"**: §5 adds Android. Decide whether that is the same product or a sibling.
- **"Not a streaming client"**: unchanged. None of these features adds playback (except the
  possible §5 player, which would need its own decision).

---

## 9. Open questions

1. **"The same software but for Android":** did you mean an Android version of Auralis (§5), or
   the iPod-sync feature running *from* an Android phone (iPod connected over USB-OTG)? The latter is
   possible through Android USB-host mass-storage libraries reading FAT32, but it's niche, and it
   would reuse the pure-Go iTunesDB writer from §4.
2. Which iPod models do you own or want to support first? This decides whether hash58 is enough for v1.
3. Is a paid Apple Developer account acceptable for direct Apple Music integration, or are the
   Library.xml/privacy-export imports enough?
4. Should phone sync default to **lossless** or a **space-saving transcode**?
5. Do your phones use internal storage only, or SD cards (which affects filename rules and MTP behaviour)?
