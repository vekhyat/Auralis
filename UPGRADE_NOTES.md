# Auralis reliability upgrades

The implementation preserves the existing Wails method names, provider configuration, queue resume behavior, and Dawn Catalog identity. Work remains local and uncommitted.

## Download execution and queue

- Direct downloads and queue runs share an execution lease. Automatic queue starts wait for an active direct download to finish.
- Stop and pause apply to the current operation. Cooldowns leave the current item paused, and adding another item does not automatically resume stopped work.
- Queue mutations cannot remove running items. Mixed runs use global Pause All and Stop All controls.
- Failed queue writes retry with bounded backoff and expose an unsaved state with Retry. A failed database read or corrupt saved payload never triggers an empty replacement.
- Legacy queue and fetch-history data are removed only after confirmed migration. Queue storage integration tests call the actual persistence module through controlled Wails bridge fixtures.
- Normal window close waits for queue persistence. A failed save keeps the window open. Backend shutdown cancels and drains tracked downloads before closing its stores.

## Files and storage

- Settings use a shared repository with serialized, atomic writes. Loading settings no longer rewrites the file; startup migration handles normalization.
- Recent-fetch JSON uses atomic replacement. Download history is written without the former delayed background goroutine.
- Existing-file checks validate audio metadata and use expected duration when available. Failed checks do not remove existing files. These checks do not decode every audio frame or replace a full integrity scan.
- Transfers use private staging directories inside the writable output folder. The library index and file tools exclude those directories.
- Publication preserves valid existing media when suffixing is disabled. Invalid media is replaced only after the new recording validates. Suffix mode chooses an unoccupied final name and updates related paths, sidecars, history, and library indexing.
- FFmpeg archive installation checks GitHub's SHA-256 digest, bounds download/extraction sizes, and stages executable replacements. Network requests and metadata probes have deadlines.
- Catalog resolvers use operation-specific contexts, so ending a download does not cancel unrelated catalog work.
- Verification browser startup fails cleanly if its process group cannot be established.

## Interface and build

- Metadata requests carry a client request ID through Wails and streamed events. Older requests cannot commit results or streams into a newer selection.
- Secondary pages load on demand and use fresh lazy components on retry. Shared non-component helpers were separated from React components.
- Keyboard rows handle Enter and Space; remove actions have labels and separate selection semantics. Primary navigation identifies the current destination.
- Provider status uses translated words. Locale keys and interpolation placeholders have a parity test.
- Generated Wails code is excluded from handwritten-source lint rules. Application source passes lint without blanket rule suppression.
- Wails module and CLI are pinned to 2.16.0. CI pins the checked Go, Node, and pnpm versions, applies tag versions to the app, and validates before release builds. Windows binaries are no longer compressed with UPX.

## Verification

From `frontend`:

```powershell
pnpm test
pnpm run typecheck
pnpm run lint
pnpm run build
pnpm run bundle:report
```

For Go checks, isolate app storage without changing the default module cache:

```powershell
$previousAppDir = $env:AURALIS_APP_DIR
try {
    $env:AURALIS_APP_DIR = Join-Path $env:TEMP 'auralis-check-data'
    go test ./...
    go vet ./...
} finally {
    $env:AURALIS_APP_DIR = $previousAppDir
}
```

When `AURALIS_APP_DIR` is set, the Windows WebView profile also uses a directory under that path. This explicitly handles Wails' Go loader, which does not retain the WebView environment-variable override used in an earlier smoke test.

The validation workflow covers frontend behavior tests, TypeScript, lint, production assets, Go tests/vet, and a Linux race-detector job. The race job is configured but has not run remotely in this task. Local Windows race checks are blocked by the installed 32-bit GCC.

Native UI checks reached Library, Queue, Tools, Audio Quality Analyzer, and Settings. The close gate has unit coverage; an uninterrupted native close/save interaction was not confirmed. Provider downloads, real verification challenges, and the macOS/Linux release artifacts remain unverified. Existing Essentia bundling warnings remain.

The larger product ideas from the review, including download receipts, an integrity-scan interface, and a broader provider-service refactor, remain follow-up work. They are not represented as delivered features by this reliability pass.
