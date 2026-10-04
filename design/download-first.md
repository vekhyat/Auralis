# Artwork library and automatic downloads

Mode: Operate. Implementation: Go + Wails v2 + React; Windows WebView2.
The user explicitly rejects Electron and chooses an artwork-led music library.

## Direction contract

THESIS: Choose music by its artwork; request files once; continue exploring.
OWN-WORLD: A record sleeve shelf on cool paper, with quiet Prussian navigation.
Artwork owns the color; native system typography keeps this a desktop tool.
STORY: Find → inspect → Download → keep browsing → open the saved folder.
FIRST VIEWPORT: Search across the top, a stable destination column, large square
recent covers with titles/artists, and a persistent download strip along the bottom.
FORM: 64px draggable titlebar, 184px destinations, 76px download strip; 208px
collection cover above the track list. Compact track rows keep only Download,
Preview, and More visible. Grid scales to the available desktop width.
Signature interaction: Download submits to one persistent worker immediately;
the strip reflects active music while Library remains usable. Pause persists
through additions/retries for the current session; Resume is explicit.
Motion: a small cover lift on hover; reduced motion disables that movement.

The user-pinned artwork direction overrides the random direction assignment.
The existing palette, providers, metadata, binding names, and persistence remain
the product foundation. No new theme chooser or desktop framework is introduced.

## Workflow contract

- Tracks, selections, albums, playlists, and artist collections use the same worker.
- New requests begin automatically when idle; later requests wait in order.
- Pause lets the current track finish and suspends subsequent work. Provider
  cooldowns also suspend automatic starts. New requests do not override a pause.
- Retry schedules the worker and preserves successful/skipped tracks and paths.
- Cancel current stops its transfer, removes that request after it settles, and
  leaves subsequent requests waiting for Resume. Completed files are retained.
- Waiting requests can be removed individually. Running requests are protected.
- Restored pending/paused requests can be resumed explicitly after reopening.
- Downloads opens on All; type filters refine it rather than splitting execution.
- Existing history, settings, folder naming, tagging, lyrics, and cover tools remain.

## Verification boundaries

The development-only preview uses real album art/metadata from Apple's public
catalog, explicitly labeled sample track lists, and simulated transfers. Its
entry is separate from production index.html. Browser interaction checks do not
prove live provider availability or native shutdown behavior. New workflow copy
currently falls back to English across locales; existing translations remain.
