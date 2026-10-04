# Source checks and download priority

Checked from this Windows machine on October 3, 2026 (IST). These are network
measurements for the tested fixtures, not a catalog-wide availability guarantee.
The isolated diagnostics do not load saved user settings or verification sessions.

## Measured download order

Each row below passed three full-track downloads, duration checks, codec checks,
and a complete FFmpeg decode. The fixture is The Beatles' Come Together, about
260 seconds. Median time includes resolution and transfer; it excludes the final
diagnostic FFmpeg decode. Source-specific setup/caching can affect the timings.

| Priority for 16-bit requests | Route | Audio | Median | Passed |
| --- | --- | --- | --- | --- |
| 1 | Antra / Qobuz | FLAC, 16-bit/44.1 kHz | 5.83 s | 3/3 |
| 2 | Antra / Deezer | FLAC, 16-bit/44.1 kHz | 8.32 s | 3/3 |
| 3 | Antra / Apple Music | ALAC, 16-bit/44.1 kHz | 15.02 s | 3/3 |
| 4 | Antra / TIDAL | FLAC, 16-bit/44.1 kHz | 21.69 s | 3/3 |
| Fallback | Antra / Amazon | Sample failed audio materialization | Unranked | 0/1 |
| Final lossy fallback | Official JioSaavn | AAC/44.1 kHz | 0.71 s | 3/3 |

Qobuz's 24-bit/96 kHz sample also passed 3/3, with a 55.64 s median. This is a
larger file from a different release, so that time is not compared with the
16-bit rows. It seeds only the 24-bit priority. Atmos was not benchmarked and
does not inherit a stereo result. An additional production-path check hit its
90-second diagnostic cutoff once, then passed on retry in 43.43 s with a longer
cutoff. Those control results are saved separately; the table above reports the
original three-sample benchmark.

Apple's text-search endpoint returned 404 in the initial pass. Its ISRC lookup
and direct selected-track audio worked. Amazon resolved the fixture but failed
to produce readable audio. No decryption behavior was changed in this work.

## New candidate and resource results

Samidy Hi-Fi returned valid metadata on 3/3 requests (219 ms median) and is in
the resource registry as a catalog source. Its playback endpoint required
authorization, so it is also a Hi-Fi download profile (below) with no measured
audio.

The other Monochrome/QQDL/SquidWTF/Kinoplus/p1nkhamster candidates did not pass
playback resolution from this machine. DAB and Lucida were blocked by access
checks or network failures. No new public audio endpoint passed validation.
Self-hosted projects without a configured server/account are not counted as
working hosted endpoints.

| Resource | Responses passing schema/identity checks | Median request time |
| --- | --- | --- |
| TIDAL catalog | 3/3 | 79 ms |
| Samidy catalog | 3/3 | 219 ms |
| LRCLIB lyrics | 3/3 | 275 ms |
| Spotify recording identifier | 3/3 | 302 ms |
| Songstats links | 3/3 | 613 ms |
| song.link links | 3/3 | 1,285 ms |
| MusicBrainz recordings | 2/3 | 582 ms |
| Deezer catalog | 2/3 | 3,474 ms |
| Qobuz direct catalog with the tested application ID | Authentication required | Unranked |
| Saavn.dev | Network checks failed | Unranked |

Community/SpotBye and Zarz returned verification requirements. Bootstrap HTTP
200 means a challenge is available, not that an audio download passed. Those
routes remain fallbacks and can move up after successful authenticated use.
Their session credentials were neither imported nor exposed by this audit.

## Community source adapters

Auralis includes native download adapters for these community APIs and servers.
They run as additional routes inside the existing ranked attempts for their
service. They are configured in **Settings → Community sources**, stored in
`community-sources.json`, and checked from the same section.

| Adapter | Protocol | Services | Default profiles |
| --- | --- | --- | --- |
| Hi-Fi / Monochrome | `/track/`, `/trackManifests/`, queued `202` playback jobs | TIDAL | 13 hosted instances (Samidy, Monochrome ×4, SquidWTF Triton, QQDL ×5, Kinoplus, p1nkhamster) |
| Qobuz-DL / Bryan-DL / SquidWTF | `/api/download-music` | Qobuz | `qobuz.squid.wtf`; Qobuz-DL and Bryan-DL server templates |
| Qobuz REST | `/download-url/{id}`, `X-API-Key` | Qobuz | Server template |
| DAB | `/api/search` recording match, then `/api/stream` | Qobuz | `dabmusic.xyz`, `dab.yeet.su` |
| Lucida | Page data, `/api/load`, worker handoff polling | Qobuz, Amazon Music | `lucida.to` (one profile per service) |
| Octo-Fiesta / Subsonic | Salted token auth, `search3`, `stream?format=raw` | TIDAL, Qobuz, Amazon, Deezer, or Apple (selectable) | Server template |

Credentials are never stored in the settings file or returned to the interface.
API-key, bearer, and Subsonic profiles name an environment variable. Cookie-based
profiles can also connect through **Verify in app**. The environment defaults are
`AURALIS_QOBUZ_REST_KEY`, `AURALIS_DAB_COOKIE`, `AURALIS_LUCIDA_COOKIE`, and
`AURALIS_SUBSONIC_AUTH` (`username:password`). Server URLs must use HTTPS, or HTTP
on a loopback address, and cannot embed credentials. Lucida and DAB sessions
come from the user's own interactive login or verification in Auralis; the app
does not automate or bypass verification pages. Browser-derived sessions are
stored separately in `community-source-sessions.bin`, protected by Windows
DPAPI, and scoped to the configured source. An environment credential overrides
the browser session for that origin.

### Verification inside Auralis

**Settings → Community download sources** provides API checks, sample downloads,
and **Verify in app** for browser-based sources. **Settings → Source connections**
also exposes the original Antra, Community/SpotBye, Zarz, and JioSaavn routes and
the catalog, metadata, lyrics, and link resources.

On Windows, verification uses an isolated Edge process embedded as a child of
the main Auralis window. It stays hidden until attached to the app viewport;
there is no separate verification dialog or system-browser fallback. Existing
Community/SpotBye and Zarz grants continue automatically after verification.
Cookie-based community sources use **Check connection** after the user completes
the site's login or check; the page stays open if the API has not accepted the
session. Cancel closes only the matching verification run. Cookie domain/path
rules apply to media and redirects, including Hi-Fi manifest segments.

API access and session connection remain distinct from validated full-track
audio. API-key and Subsonic sources require their configured credentials;
browser verification cannot supply those credentials. In-app hosting is
Windows-only and requires Microsoft Edge.

A community route only succeeds after the shared checks: matching track,
full-length audio rather than a preview, the requested codec family (FLAC/ALAC,
or E-AC-3 for Atmos), and 24-bit depth when requested. Protected manifests, a
different track, and Atmos requests on adapters without an Atmos selection are
rejected rather than downgraded. A failing route pauses for ten minutes. Changing
a profile's server or credential settings discards its stored checks and timings.

streamrip, tiddl, and qobuz-cli are standalone clients rather than hosted
services, so they are not integrated as routes. The adapters above cover the
server projects they share APIs with.

### Live check of the default profiles

API checks ran twice on October 3, 2026 from this machine. The second run used
the final adapters (`resources/source-benchmarks/community-adapters.json`). A
sample download runs only after an API check responds, and none responded.
**No hosted community profile delivered audio**, so none has a download
measurement and they rank behind validated routes.

| Result | Profiles |
| --- | --- |
| Authorization required (401/403 or browser verification page) | Samidy Hi-Fi, DAB Music, DAB Yeet, Lucida / Qobuz, Lucida / Amazon |
| Connection failed or timed out | Monochrome ×4, SquidWTF Triton, QQDL ×5, Kinoplus, p1nkhamster, SquidWTF Qobuz |
| Needs a user-supplied server | Qobuz REST, Qobuz-DL, Bryan-DL, Octo-Fiesta / Subsonic |

Protocol behavior is covered by tests in `backend/community_adapters_test.go`
that run against local servers. These tests are not evidence that any hosted
instance works. A profile moves up in priority only after it passes a sample
download or a real download.

## Runtime behavior

- Auto downloads use recent, quality-specific performance to order services.
- TIDAL, Qobuz, and Amazon independently rank their download routes.
- Reliable validated results come first; median latency breaks equal reliability.
- Failed, blocked, and untested sources remain available as later fallbacks.
- JioSaavn stays last for lossless and Atmos requests because it supplies AAC.
- Configured custom servers are still tried first within their selected service.
- Resource resolver ranking applies when resolver fallback is enabled.
- Cancellation, existing files, previews, and unreadable audio do not earn a
  successful-download measurement. 24-bit responses are checked for bit depth.
- Measurements are stored in `source-performance.json` in Auralis' data folder.
  Entries older than seven days stop influencing priority. Built-in measurements
  in `backend/source_benchmarks.json` provide the initial evidence.

The registries are available through `GetDownloadSources`, `GetResourceSources`,
and `GetSourceBenchmarks`. They distinguish audio download sources from metadata.

## Repeating the diagnostics

Use a new isolated results folder and run the opt-in tests:

```powershell
$env:AURALIS_LIVE_SOURCE_CHECK = '1'
$env:AURALIS_LIVE_RESULTS_DIR = Join-Path $env:TEMP ('auralis-check-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
$env:AURALIS_APP_DIR = Join-Path $env:AURALIS_LIVE_RESULTS_DIR 'app-data'
$env:GOCACHE = Join-Path $env:TEMP 'auralis-source-go-cache'
go test ./backend -run 'TestLiveRankedSourceBenchmark|TestLiveSourceResourceAudit|TestLiveSourceResolverAudit' -v -count=1 -timeout 20m
go test ./backend -run TestLiveCommunitySourceAdapters -v -count=1 -timeout 10m
```

The diagnostic tests write classified results; a passing test process does not
mean every provider worked. Read the per-source results. The JSON reports for
this run are in `resources/source-benchmarks/`. Benchmarking fetches sample audio;
the repeated benchmark removes its samples after validation.
