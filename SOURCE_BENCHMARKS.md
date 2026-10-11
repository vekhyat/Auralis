# Source checks and download priority

## October 5, 2026: non-working routes removed

Each removal was confirmed from outside this network (Cloudflare DNS-over-HTTPS
plus check-host.net nodes in four countries), because the local DNS filter can
make reachable hosts look dead.

| Removed | Evidence | Change |
| --- | --- | --- |
| Saavn.dev JioSaavn fallback | `saavn.dev` is NXDOMAIN; every external node failed to resolve it | Search and stream-URL fallbacks deleted; JioSaavn uses only the official API |
| Samidy catalog check | `/info/` returns HTTP 401 from every external node | Removed from the resource registry and connection checks |
| Qobuz direct catalog check | `track/get` with the bundled app ID returns Qobuz's own HTTP 401 "User authentication is required" | Removed from the resource registry and connection checks |
| Saved TIDAL / Amazon downloader | Antra TIDAL lookup timed out; Antra Amazon returned 404 (table below) | Old settings naming either now load as automatic selection |

The gated live audit in `source_resource_audit_test.go` still probes these hosts,
so a recovery would show up in future audits.

## October 5, 2026 follow-up

The community defaults and website checks were audited again from this Windows
machine. Following the user's request, the 18 hosted community profiles and four
empty server templates are **removed from the shipped defaults**. Stored copies
of retired public hosts are also excluded, so an old enabled flag cannot restore
them. Unrelated custom configuration is preserved internally.

Settings no longer contains community sources, source connections, API checks,
or custom-server dialogs. Automatic downloads retain Qobuz, Deezer, Apple Music,
and official JioSaavn. TIDAL, Amazon, unverified community/grant routes, and the
failed Saavn.dev fallback are excluded from automatic selection. Protocol code
remains for compatibility; users choose file quality without managing sources.

The final community audit recorded **0 validated, 0 API-available, 15 failed,
3 authentication-required, and 4 not configured**. Samidy returned HTTP 401;
both Lucida profiles returned HTTP 403. DAB Yeet returned ordinary non-JSON,
which is now correctly reported as a failure instead of a browser verification
requirement. No community candidate passed the API prerequisite for a sample
download. These results do not establish availability after user authentication.

Separate built-in-route checks used Come Together by The Beatles, ISRC
GBAYE0601690. Successful rows passed title/artist matching, complete transfer,
duration checks, FFprobe, and a full FFmpeg decode. Bit depth was also inspected
on the retained sample files. Times include lookup, transfer and validation;
they are single checks and are not comparable to the historical medians below.

| Built-in route | Current result | Elapsed |
| --- | --- | --- |
| Antra / Qobuz | Passed: 16-bit FLAC, 44.1 kHz, 259.95 s | 12.15 s |
| Antra / Deezer | Passed: 16-bit FLAC, 44.1 kHz, 259.67 s | 14.44 s |
| Antra / Apple | HTTP 503 initially; retry passed: 24-bit ALAC, 44.1 kHz, 259.67 s | 48.07 s on retry |
| JioSaavn | Passed: AAC, 44.1 kHz, 259.95 s | 9.02 s |
| Antra / TIDAL | ISRC lookup timed out on both attempts; audio not reached | 15.47 / 15.68 s |
| Antra / Amazon | ISRC lookup returned HTTP 404; audio not reached | 1.07 s |

These are fixture-specific results, not catalog-wide guarantees. No new hosted
source was promoted. The historical benchmark seed remains dated as originally
measured; this follow-up does not turn one sample into a new latency ranking.
Sanitized evidence is under `resources/source-benchmarks/2026-10-05/`.

The integrated changes also tighten what verification means:

- Qobuz/DAB ordinary HTML and empty responses fail normally; only recognizable
  login/challenge pages or access-denied responses prompt authentication.
- Lucida and catalog checks reject mismatched recording identities.
- Internal Community/SpotBye and Zarz checks classify local grants as saved
  sessions, because reuse of an existing grant establishes neither current API
  access nor downloadable audio. These checks are no longer shown in Settings.
- Browser-cookie verification checks the selected source API before accepting
  the session, and continues to leave audio unverified. API-key, bearer and
  Subsonic failures instead request credential configuration.
- Ranked downloads reject missing files, previews with unknown catalog duration,
  and incompatible codecs before accepting a route or improving its rank.
  Known short recordings can still pass when their expected duration is supplied.
- The live community report now persists the final sample-download outcome,
  including failures, instead of leaving the earlier API-only success in place.

Validation: `go test ./...`, `go vet ./...`, `go build ./...`, Windows
`go test -race ./backend`, all 39 frontend tests (including locale parity),
TypeScript, lint, and the frontend production build passed. Wails bindings were
regenerated. Browser/session regression tests use local fixtures; native Edge
challenge completion and authenticated hosted downloads were not exercised.

## Historical October 3 measurements

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
routes were retained as fallbacks in that historical build, but are excluded
from the current automatic route list.
Their session credentials were neither imported nor exposed by this audit.

## Historical community source adapters

Auralis retains native protocol implementations for compatibility and internal
diagnostics. The following table records the removed presets, not the current
default registry. The former Settings controls have been deleted.

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

### Internal verification implementation

The API, sample-download and browser-verification bindings remain available for
compatibility and developer diagnostics. Source connections, catalogs, and
API status are not exposed in Settings.

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
- Automatic routes are restricted to currently retained working services;
  retired public candidate profiles are excluded even if previously saved.
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
