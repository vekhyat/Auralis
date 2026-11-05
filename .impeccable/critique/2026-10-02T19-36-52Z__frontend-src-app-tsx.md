---
target: current Auralis redesign
total_score: 21
max_score: 40
na_heuristics: 
p0_count: 2
p1_count: 2
timestamp: 2026-10-02T19-36-52Z
slug: frontend-src-app-tsx
---
Method: dual-agent (A: 01a0fe16-027e-7d43-98bb-5cf71edb46aa · B: 01a0fe16-0280-7d31-951e-9ad885ec4344)

# Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | The download shelf tells the truth. Fetch and download still share one moment, and a search typed on Settings never appears. |
| 2 | Match System / Real World | 2 | Album, artist, and Download match the desk. Source, Songlink, HiFi API, and 16-bit/44.1kHz do not. |
| 3 | User Control and Freedom | 3 | Back, pause, resume, cancel, and remove work. Two Resume labels, and Clear All sits in the routine toolbar. |
| 4 | Consistency and Standards | 2 | A library sleeve opens. A search sleeve only selects. Download, View tracks & details, and In downloads are three verbs for one job. |
| 5 | Error Prevention | 2 | Bad links are caught. Two identical cover icons are not. A whole artist is one click from a discography download. |
| 6 | Recognition Rather Than Recall | 2 | Sleeves work. Search open is a second click, album actions are unlabeled icons, and the store order is nameless marks. |
| 7 | Flexibility and Efficiency | 2 | Enter fetches a link, then the user still clicks Download. Enter on a search does nothing. |
| 8 | Aesthetic and Minimalist Design | 2 | The library is quiet and specific. Album, search, and Downloads add a second search, a filter console, and equal-weight icons. |
| 9 | Error Recovery | 2 | Retry exists. The words around failure are a VPN list, a store name, or another website. |
| 10 | Help and Documentation | 1 | The only guidance teaches an inspect step the product should not require. |
| **Total** | | **21/40** | **Acceptable** |

# Design Specificity Verdict

**LLM assessment.** The library is authored. Cool paper, iron ink, one Prussian accent, and the sleeves carrying the color make a desk, not a streaming homepage and not a purple downloader template. `CatalogPane`, `ArtworkCard`, and `DownloadShelf` belong to this product. The sentence "Ready when you are" sounds like Auralis.

The working surfaces do not. After a search, the primary button is `View tracks & details` and the side panel says `Select an entry to inspect it here.` An album becomes a spreadsheet: checkbox, number, title, duration, plays, and three icons per row. Downloads repeats the shelf's controls and adds type tabs, a status filter, and two red buttons. Settings is still a SpotiFLAC control panel, with `Source`, `Songlink`, `16-bit/44.1kHz`, and `Tidal` / `Qobuz` `Add Instance` at the same rank as the folder where files go. Once the sleeves leave the screen, the UI could be any catalog utility.

**Deterministic scan.** `detect.mjs --json frontend/src` exited 0 and returned `[]`. No rule hits, so there is nothing to mark as a false positive. The scan does not score information architecture or copy. A clean detector run is not evidence that the job is frictionless.

**Visual overlays.** No reliable user-visible overlay. A script tag for `http://127.0.0.1:8400/detect.js` was injected into the preview, and the fetch failed (`Failed to fetch`, plus a console `ERR_CONNECTION_REFUSED`). No `impeccable` console messages. Fallback: the live Vite preview at `http://127.0.0.1:5173/preview.html` was opened, resized, and walked through Library, a `radiohead` search, the In Rainbows inspector, and Settings. Existing captures in `.impeccable/review/` cover the album page and an active download.

# Overall Impression

The shell is already the right object: one field, artwork, a download strip that stays put. The job inside it is still "inspect, then manage a source." The next redesign should not invent a new look. It should make paste and search end in a file, and take stores, bitrates, and resolvers off every screen a person actually sees.

# What's Working

- The library is a specific place. Large sleeves, titles, artists, and almost no chrome. It does not look like Spotify, and it does not look like a settings app.
- `DownloadShelf` is the right persistent object. It names the music, stays available while browsing, and already says downloads start on their own.
- `OmnibarSearch` is the right primary control: one field, no Fetch button, placeholder "Paste a link, or search tracks, albums, artists, playlists."

# Priority Issues

**[P0] Settings still makes the user the source operator.**
**Why it matters:** The person was promised they could paste or search and not think about where the audio comes from. Settings is a primary destination, and its `Download` section shows `Link Resolver` (`Songlink`), `Allow Resolver Fallback`, `Community`, `Source` (`Auto`), an icon order whose accessible value is `Tidal Qobuz Amazon Music`, `16-bit/44.1kHz`, then `Tidal` and `Qobuz` buttons labeled `Add Instance`. That is the opposite of a quiet lossless promise.
**Fix:** Settings keeps folder, filename, folder structure, embed lyrics, embed cover, language, and light/dark. Delete `Source`, the order picker, the bitrate picker, Songlink, and custom instances from the path a person sees. One silent default: best file available, fall through without asking. If a power control must survive, put it behind one collapsed `Advanced` disclosure that is not open by default.
**Suggested command:** `$impeccable distill`

**[P0] Paste and search still end on an inspection desk.**
**Why it matters:** Enter on a Spotify link only fetches. The omnibar keeps `https://open.spotify.com/...`, so the app looks like a Spotify browser, and the album page waits for another `Download` click. A text search is worse. `submit()` in `useSmartSearch` has no branch for a search, so Enter does nothing. Typing `radiohead` on Settings leaves the user on Settings. On Library, a click only selects, and the inspector's button is `View tracks & details` next to `open.spotify.com`. The library subtitle teaches the wrong job: "Explore the artwork and track list. Download it in one click."
**Fix:** A pasted link starts saving immediately. Show the sleeve as confirmation, with the track list available to uncheck songs, not as a gate. The omnibar returns to its placeholder or shows the title, not the foreign URL. On a search result, the primary button is `Download`. Opening the track list is secondary and named `Choose tracks`. Typing in the omnibar always leaves Settings, Downloads, or History and shows results. One search field. Remove the second boxes (`Search albums...`, `Search tracks...`) until a list is long enough to need them.
**Suggested command:** `$impeccable distill`

**[P1] Downloads is a second product sitting on a good shelf.**
**Why it matters:** The shelf already pauses, cancels, and opens the folder. The page repeats those controls, then adds `All` / `Tracks` / `Albums` / `Playlists` / `Artists`, an eight-status filter, another search, `Clear Finished`, and a red `Clear All` beside a red `Cancel current download`. Status voice splits `Running` from `WAITING`. A person who only wanted a file now has a queue console.
**Fix:** The page is a receipt: what is saving, what is waiting, what failed, with `Retry` on a failure. The shelf remains the only pause, resume, and cancel. Type and status filters move into one overflow. One danger color, used for cancel of the current transfer, not for tidying finished rows.
**Suggested command:** `$impeccable quieter`

**[P1] A failure hands the worry back.**
**Why it matters:** `Fetch Failed` tells the user to try Surfshark, ExpressVPN, or Proton VPN, and to pick a country "not blocked by Spotify." A cooldown banner talks about a scheduled server break. History can offer `Open in Tidal`. `TrackList` still has `Check download sources`, which renders Tidal, Qobuz, Amazon Music, and `Not Found`. The end of a bad moment should be the song, not the plumbing.
**Fix:** "Couldn't save this" and `Retry` on that row. No VPN list, no store links, and no server-status site in the main window. History remembers the file and the date. If nothing can be saved, say that in plain language and stop. First-run setup for FFmpeg should read as "Setting things up," not a GitHub binary lecture titled `FFmpeg Required`.
**Suggested command:** `$impeccable clarify`

**[P2] The album page drops the library's manners.**
**Why it matters:** The sleeve and the `Download` button are strong, then the eye falls into a filter bar and a row of equal icon buttons. Two of those icons are the same image (`Download All Separate Covers` vs `Download Separate Album Cover`), distinguishable only by tooltip. The count is labeled `tracks` beside `Artist` and `Release Date`. `PLAYS` is a streaming statistic on a file tool. This is where the redesign stops looking like one product.
**Fix:** One `Download`. Lyrics and covers move into `More`. Metadata becomes one line: album, artist, year, track count. Drop `PLAYS` from the default table. Use the same sleeve-and-ruled-row language as the library. Do not add color, type, or decoration. The sleeves are already the color.
**Suggested command:** `$impeccable layout`

# Persona Red Flags

**Jordan (first time, Spotify link).** The field is obvious, and that part works. They paste, press Enter, and the bar fills with a Spotify URL while the album loads. They will think that was the download. If they hesitate, the unlabeled icons and the per-track download buttons make the big `Download` feel optional. If the fetch fails, the dialog names three VPN brands.

**Alex (paste and Enter).** A link is one extra click after Enter. A song title is three: type, click a sleeve that only selects, then `View tracks & details`, then `Download`. Enter on the search text does not commit. Opening Downloads adds `Pause All`, `Cancel current download`, and `Clear All` before the file is the point.

**Mina (wants the file, must never learn the store).** She is fine only if she never opens Settings, History, or More. The moment she opens Settings she has to look at `Source`, a Tidal → Qobuz → Amazon order, and `16-bit/44.1kHz`. On the main path she still waits through a metadata fetch before anything saves, and a failure talks about Spotify and VPNs.

# Minor Observations

- The up-arrow beside the library title has no label.
- Search result cards drop the library's "Album · when" line and show a bare `6`.
- The search inspector's empty state, "Select an entry to inspect it here.", is the product talking to itself.
- `Resume` on the shelf and `Resume All` on the page do the same job.
- `ApiStatusTab` is not mounted, but the provider dashboard and the unused tagline "Fetch lossless audio from Tidal, Qobuz, and Amazon Music" are still in the tree.
- "Your music, ready to keep." sits on recently explored artwork, which is not files on disk. Use that headline only for the empty invitation. When items exist, the page is just the shelf.
- At 1200px the destination names survive. The small-window album is tighter, not broken. The downloads toolbar still does not fit the job.

# Questions to Consider

- If the file is the product, why is the moment after Enter an inspection desk?
- Who is the `Source` dropdown for, if this person was promised they would never learn the store's name?
- The library already looks finished. What if album, search, and downloads were forced to use only the elements that page already earned?
