# Auralis IPC contract

Binding contract for the Tauri port. The React frontend stays on the Wails v2 names and JSON shapes. Source of truth is the Go `App` methods and `encoding/json` (including `MarshalJSON`), not a cleaned-up rename.

Generator facts, checked against Wails v2.16.0 (`go.mod:15`, `github.com/wailsapp/wails/v2/internal/binding` and `internal/typescriptify`):

- Only exported methods on `*App` are bound. The generated JavaScript calls `window['go']['main']['App'][Name](...)`. Arguments are positional. The `.d.ts` names them `arg1`, `arg2`, … and not the Go parameter names.
- Every binding returns a Promise. `(T, error)` becomes `Promise<T>`. A lone `error`, or no result, becomes `Promise<void>`. A Go error rejects the promise. Two non-error results would be `Promise<A|B>` (none of these methods do that).
- Signatures use `Array<T>` for slices and `Record<K, V>` for maps. `[]byte` in a signature is `string` (standard base64). Pointers are stripped in the signature; a nil pointer is JSON `null`.
- Structs become classes in `frontend/wailsjs/go/models.ts` under `export namespace <package>`. This document writes that class as `models.<package>.<Type>`. Inside a namespace, Wails leaves same-package fields unqualified; the meaning is the same class.
- Struct field names in the class and on the wire come from the `json` tag. An untagged exported field uses the Go field name. `json:"-"` is omitted. `omitempty` and pointer fields are optional on the class (`field?: T`). `omitempty` drops the key for the Go zero value (`false`, `0`, `""`, nil pointer, nil slice, nil map, zero `time.Time`). A non-nil empty slice is still `[]`.
- Wails struct-field TypeScript is not always the JSON type. `[]byte` fields are typed `number[]` but `encoding/json` sends a base64 string. `time.Time` fields are typed `any` (the struct has no exported fields) but JSON is an RFC3339 string. Nested maps inside a struct field collapse to `any` in the class; a method signature keeps nested `Record`s. Rust serde must follow the JSON column, not the mismatched TypeScript.
- `int64` and `uint64` are JSON numbers and TypeScript `number`. Integers above 2^53−1 are not exact in JavaScript.
- Anonymous embedded structs are flattened, both by `encoding/json` and by Wails, unless the embedded field has an explicit JSON name or `json:"-"`. Those exceptions are flagged on the type.

## 1. Summary

Exported `func (a *App)` methods: **163**.

`PRODUCT.md:54` says the Wails `App` binding has 124 IPC methods. That count matches the methods that live outside `app_library.go`, `app_sync.go`, and `app_taste.go`:

| File | Exported methods |
| --- | ---: |
| `app.go` | 99 |
| `app_ipod.go` | 6 |
| `app_library.go` | 8 |
| `app_sync.go` | 13 |
| `app_taste.go` | 18 |
| `close_lifecycle.go` | 2 |
| `source_priority.go` | 9 |
| `verification_ui.go` | 8 |

`app.go` (99) + `app_ipod.go` (6) + `close_lifecycle.go` (2) + `source_priority.go` (9) + `verification_ui.go` (8) = **124**, which is the PRODUCT.md figure. `app_library.go` (8) + `app_sync.go` (13) + `app_taste.go` (18) = **39** methods added on `main` since that sentence was written. 124 + 39 = **163**.

Unexported `(a *App)` methods: **17**. They are not bound and are not part of the contract.

> **Dynamic bridge access (orchestrator review).** Seven methods are not imported from `wailsjs/go/main/App`; the frontend reaches them through `window.go.main.App` at runtime: `ReadFileAsBase64`, `DecodeAudioForAnalysis` (`useAudioAnalysis.ts`), `RankDownloadServices` (`lib/source-priority.ts`), and the four source-verification methods (`lib/source-verification.ts` `sourceVerificationBridge()`). The Tauri bridge must cover these access paths too, or they silently fail with "unavailable".

## 2. Methods

One row per exported method, in source order within each file. Call sites are invocations under `frontend/src`: a static or dynamic import from `wailsjs/go/main/App` (aliases included), or `desktopAppBindings().Name(...)` in `useDownload.ts`. `none` means the frontend never calls it. Unused methods stay in the contract. `frontend/src/preview.tsx` stubs some names on `window.go.main.App` for the dev harness; those stubs are listed after the tables and are not call sites.

### `app.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `GetCommunityBreakStatuses` | `app.go:104` | — | map[string]CommunityBreakStatus | `()=>Promise<Record<string, models.main.CommunityBreakStatus>>` | none |
| `LogStatusConsole` | `app.go:140` | level string, message string | — | `(arg1:string,arg2:string)=>Promise<void>` | none |
| `GetCurrentIPInfo` | `app.go:316` | — | (string, error) | `()=>Promise<string>` | none |
| `GetStreamingURLs` | `app.go:947` | spotifyTrackID string, region string | (string, error) | `(arg1:string,arg2:string)=>Promise<string>` | frontend/src/hooks/useDownload.ts:430, frontend/src/hooks/useDownload.ts:855 |
| `GetSpotifyMetadata` | `app.go:967` | req SpotifyMetadataRequest | (string, error) | `(arg1:models.main.SpotifyMetadataRequest)=>Promise<string>` | frontend/src/lib/api.ts:20 |
| `SearchSpotify` | `app.go:1030` | req SpotifySearchRequest | (*backend.SearchResponse, error) | `(arg1:models.main.SpotifySearchRequest)=>Promise<models.backend.SearchResponse>` | frontend/src/hooks/useSmartSearch.tsx:82 |
| `SearchSpotifyByType` | `app.go:1052` | req SpotifySearchByTypeRequest | ([]backend.SearchResult, error) | `(arg1:models.main.SpotifySearchByTypeRequest)=>Promise<Array<models.backend.SearchResult>>` | frontend/src/hooks/useMetadata.ts:111, frontend/src/hooks/useSmartSearch.tsx:219 |
| `DownloadTrack` | `app.go:1071` | req DownloadRequest | (DownloadResponse, error) | `(arg1:models.main.DownloadRequest)=>Promise<models.main.DownloadResponse>` | frontend/src/lib/api.ts:53 |
| `OpenFolder` | `app.go:1591` | path string | error | `(arg1:string)=>Promise<void>` | frontend/src/App.tsx:704 |
| `OpenConfigFolder` | `app.go:1604` | — | error | `()=>Promise<void>` | frontend/src/components/SettingsPage.tsx:220 |
| `BackupSettings` | `app.go:1612` | — | (string, error) | `()=>Promise<string>` | frontend/src/components/SettingsPage.tsx:232 |
| `RestoreSettings` | `app.go:1638` | — | (string, error) | `()=>Promise<string>` | frontend/src/components/SettingsPage.tsx:246 |
| `SelectFolder` | `app.go:1666` | defaultPath string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/components/AndroidSyncPanel.tsx:351, frontend/src/components/AndroidSyncPanel.tsx:367, frontend/src/components/AudioAnalysisPage.tsx:389, frontend/src/components/AudioConverterPage.tsx:133, frontend/src/components/AudioResamplerPage.tsx:148, frontend/src/components/DevicesPage.tsx:251, frontend/src/components/EnrichPage.tsx:187, frontend/src/components/FileManagerPage.tsx:192, frontend/src/components/LibraryHealthPage.tsx:156, frontend/src/components/ListeningConnectionsSettings.tsx:209, frontend/src/components/ReplayGainPage.tsx:274, frontend/src/components/SettingsPage.tsx:207, frontend/src/components/TempoKeyAnalyzerPage.tsx:285 |
| `SelectFile` | `app.go:1670` | — | (string, error) | `()=>Promise<string>` | none |
| `GetDefaults` | `app.go:1674` | — | map[string]string | `()=>Promise<Record<string, string>>` | frontend/src/components/EnrichPage.tsx:152, frontend/src/lib/settings.ts:802 |
| `GetDownloadProgress` | `app.go:1680` | — | backend.ProgressInfo | `()=>Promise<models.backend.ProgressInfo>` | frontend/src/hooks/useDownloadProgress.ts:32 |
| `GetDownloadQueue` | `app.go:1684` | — | backend.DownloadQueueInfo | `()=>Promise<models.backend.DownloadQueueInfo>` | none |
| `LoadPersistentDownloadQueue` | `app.go:1688` | — | (string, error) | `()=>Promise<string>` | frontend/src/lib/queue.ts:205 |
| `ReplacePersistentDownloadQueue` | `app.go:1692` | queueJSON string | error | `(arg1:string)=>Promise<void>` | frontend/src/lib/queue.ts:130 |
| `ApplyPersistentDownloadQueueChanges` | `app.go:1696` | upsertsJSON string, removedIDs []string, orderJSON string | error | `(arg1:string,arg2:Array<string>,arg3:string)=>Promise<void>` | frontend/src/lib/queue.ts:148 |
| `ClearCompletedDownloads` | `app.go:1700` | — | — | `()=>Promise<void>` | none |
| `ClearAllDownloads` | `app.go:1704` | — | — | `()=>Promise<void>` | none |
| `AddToDownloadQueue` | `app.go:1708` | spotifyID string, trackName string, artistName string, albumName string | string | `(arg1:string,arg2:string,arg3:string,arg4:string)=>Promise<string>` | frontend/src/hooks/useDownload.ts:1353, frontend/src/hooks/useDownload.ts:1650, frontend/src/hooks/useDownload.ts:422 |
| `MarkDownloadItemFailed` | `app.go:1714` | itemID string, errorMsg string | — | `(arg1:string,arg2:string)=>Promise<void>` | frontend/src/hooks/useDownload.ts:1125, frontend/src/hooks/useDownload.ts:1181, frontend/src/hooks/useDownload.ts:1440, frontend/src/hooks/useDownload.ts:1742, frontend/src/hooks/useDownload.ts:697, frontend/src/hooks/useDownload.ts:754 |
| `CancelAllQueuedItems` | `app.go:1718` | — | — | `()=>Promise<void>` | frontend/src/hooks/useDownload.ts:1449, frontend/src/hooks/useDownload.ts:1750 |
| `ForceStopDownloads` | `app.go:1722` | — | — | `()=>Promise<void>` | frontend/src/hooks/useDownload.ts:1904 |
| `ExportFailedDownloads` | `app.go:1726` | — | (string, error) | `()=>Promise<string>` | frontend/src/components/DebugLoggerPage.tsx:58 |
| `CheckAPIStatus` | `app.go:1794` | apiType string, apiURL string | bool | `(arg1:string,arg2:string)=>Promise<boolean>` | none |
| `CheckAPIStatusReport` | `app.go:1832` | apiType string, apiURL string | APIStatusReport | `(arg1:string,arg2:string)=>Promise<models.main.APIStatusReport>` | none |
| `FetchSpotiFLACStatusPayload` | `app.go:1917` | kind string | (map[string]string, error) | `(arg1:string)=>Promise<Record<string, string>>` | none |
| `CheckCustomTidalAPI` | `app.go:1932` | apiURL string | bool | `(arg1:string)=>Promise<boolean>` | none |
| `CheckCustomQobuzAPI` | `app.go:2016` | apiURL string | bool | `(arg1:string)=>Promise<boolean>` | none |
| `Quit` | `app.go:2302` | — | — | `()=>Promise<void>` | none |
| `GetDownloadHistory` | `app.go:2307` | — | ([]backend.HistoryItem, error) | `()=>Promise<Array<models.backend.HistoryItem>>` | frontend/src/components/HistoryPage.tsx:106 |
| `ClearDownloadHistory` | `app.go:2311` | — | error | `()=>Promise<void>` | frontend/src/components/HistoryPage.tsx:263 |
| `DeleteDownloadHistoryItem` | `app.go:2315` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/HistoryPage.tsx:268 |
| `GetFetchHistory` | `app.go:2319` | — | ([]backend.FetchHistoryItem, error) | `()=>Promise<Array<models.backend.FetchHistoryItem>>` | frontend/src/components/HistoryPage.tsx:115 |
| `AddFetchHistory` | `app.go:2323` | item backend.FetchHistoryItem | error | `(arg1:models.backend.FetchHistoryItem)=>Promise<void>` | frontend/src/hooks/useMetadata.ts:240 |
| `ClearFetchHistory` | `app.go:2327` | — | error | `()=>Promise<void>` | none |
| `DeleteFetchHistoryItem` | `app.go:2331` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/HistoryPage.tsx:278 |
| `ClearFetchHistoryByType` | `app.go:2335` | itemType string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/HistoryPage.tsx:272 |
| `GetRecentFetches` | `app.go:2339` | — | (string, error) | `()=>Promise<string>` | frontend/src/App.tsx:362, frontend/src/App.tsx:379 |
| `SaveRecentFetches` | `app.go:2353` | payload string | error | `(arg1:string)=>Promise<void>` | frontend/src/App.tsx:346 |
| `SaveSpectrumImage` | `app.go:2367` | audioFilePath string, base64Data string | (string, error) | `(arg1:string,arg2:string)=>Promise<string>` | frontend/src/components/AudioAnalysisPage.tsx:511, frontend/src/components/AudioAnalysisPage.tsx:572 |
| `DownloadLyrics` | `app.go:2417` | req LyricsDownloadRequest | (backend.LyricsDownloadResponse, error) | `(arg1:models.main.LyricsDownloadRequest)=>Promise<models.backend.LyricsDownloadResponse>` | frontend/src/lib/api.ts:66 |
| `DownloadCover` | `app.go:2485` | req CoverDownloadRequest | (backend.CoverDownloadResponse, error) | `(arg1:models.main.CoverDownloadRequest)=>Promise<models.backend.CoverDownloadResponse>` | frontend/src/lib/api.ts:71 |
| `DownloadHeader` | `app.go:2533` | req HeaderDownloadRequest | (backend.HeaderDownloadResponse, error) | `(arg1:models.main.HeaderDownloadRequest)=>Promise<models.backend.HeaderDownloadResponse>` | frontend/src/lib/api.ts:75 |
| `DownloadGalleryImage` | `app.go:2573` | req GalleryImageDownloadRequest | (backend.GalleryImageDownloadResponse, error) | `(arg1:models.main.GalleryImageDownloadRequest)=>Promise<models.backend.GalleryImageDownloadResponse>` | frontend/src/lib/api.ts:79 |
| `DownloadAvatar` | `app.go:2613` | req AvatarDownloadRequest | (backend.AvatarDownloadResponse, error) | `(arg1:models.main.AvatarDownloadRequest)=>Promise<models.backend.AvatarDownloadResponse>` | frontend/src/lib/api.ts:83 |
| `CheckTrackAvailability` | `app.go:2646` | spotifyTrackID string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/hooks/useAvailability.ts:22 |
| `IsFFmpegInstalled` | `app.go:2667` | — | (bool, error) | `()=>Promise<boolean>` | none |
| `IsFFprobeInstalled` | `app.go:2671` | — | (bool, error) | `()=>Promise<boolean>` | none |
| `DownloadFFmpeg` | `app.go:2683` | — | DownloadFFmpegResponse | `()=>Promise<models.main.DownloadFFmpegResponse>` | frontend/src/App.tsx:477 |
| `GetBrewPath` | `app.go:2703` | — | string | `()=>Promise<string>` | none |
| `IsBrewFFmpegInstalled` | `app.go:2707` | — | (bool, error) | `()=>Promise<boolean>` | none |
| `InstallFFmpegWithBrew` | `app.go:2717` | — | InstallFFmpegWithBrewResponse | `()=>Promise<models.main.InstallFFmpegWithBrewResponse>` | none |
| `ConvertAudio` | `app.go:2745` | req ConvertAudioRequest | ([]backend.ConvertAudioResult, error) | `(arg1:models.main.ConvertAudioRequest)=>Promise<Array<models.backend.ConvertAudioResult>>` | frontend/src/components/AudioConverterPage.tsx:235 |
| `ResampleAudio` | `app.go:2761` | req ResampleAudioRequest | ([]backend.ResampleResult, error) | `(arg1:models.main.ResampleAudioRequest)=>Promise<Array<models.backend.ResampleResult>>` | frontend/src/components/AudioResamplerPage.tsx:257 |
| `SelectAudioFiles` | `app.go:2770` | — | ([]string, error) | `()=>Promise<Array<string>>` | frontend/src/components/AudioConverterPage.tsx:120, frontend/src/components/AudioResamplerPage.tsx:135, frontend/src/components/DevicesPage.tsx:241, frontend/src/components/ReplayGainPage.tsx:265, frontend/src/components/TempoKeyAnalyzerPage.tsx:273 |
| `EnrichAudioFiles` | `app.go:2778` | filePaths []string, priority string, allowFallback bool | []backend.EnrichResult | `(arg1:Array<string>,arg2:string,arg3:boolean)=>Promise<Array<models.backend.EnrichResult>>` | frontend/src/components/EnrichPage.tsx:202 |
| `InspectEnrichFile` | `app.go:2804` | filePath string | (backend.EnrichMetadataPreview, error) | `(arg1:string)=>Promise<models.backend.EnrichMetadataPreview>` | frontend/src/components/EnrichPage.tsx:222 |
| `InspectEnrichFiles` | `app.go:2808` | filePaths []string | []backend.EnrichMetadataPreview | `(arg1:Array<string>)=>Promise<Array<models.backend.EnrichMetadataPreview>>` | frontend/src/components/EnrichPage.tsx:127 |
| `GetFlacInfoBatch` | `app.go:2812` | paths []string | []backend.FlacInfo | `(arg1:Array<string>)=>Promise<Array<models.backend.FlacInfo>>` | frontend/src/components/AudioResamplerPage.tsx:116 |
| `GetFileSizes` | `app.go:2816` | files []string | map[string]int64 | `(arg1:Array<string>)=>Promise<Record<string, number>>` | frontend/src/components/AudioAnalysisPage.tsx:311, frontend/src/components/AudioConverterPage.tsx:167, frontend/src/components/AudioResamplerPage.tsx:182, frontend/src/components/ReplayGainPage.tsx:246, frontend/src/components/TempoKeyAnalyzerPage.tsx:255 |
| `AnalyzeReplayGainFile` | `app.go:2820` | filePath string | backend.ReplayGainAnalysisResult | `(arg1:string)=>Promise<models.backend.ReplayGainAnalysisResult>` | frontend/src/components/ReplayGainPage.tsx:201, frontend/src/lib/replaygain.ts:26 |
| `AnalyzeReplayGainAlbum` | `app.go:2846` | filePaths []string | backend.ReplayGainAnalysisResult | `(arg1:Array<string>)=>Promise<models.backend.ReplayGainAnalysisResult>` | frontend/src/components/ReplayGainPage.tsx:161, frontend/src/lib/replaygain.ts:43 |
| `CancelReplayGainAnalysis` | `app.go:2872` | — | — | `()=>Promise<void>` | frontend/src/components/ReplayGainPage.tsx:123, frontend/src/components/ReplayGainPage.tsx:298 |
| `WriteReplayGainTags` | `app.go:2882` | entries []backend.ReplayGainTagWrite | []backend.ReplayGainWriteResult | `(arg1:Array<models.backend.ReplayGainTagWrite>)=>Promise<Array<models.backend.ReplayGainWriteResult>>` | frontend/src/components/ReplayGainPage.tsx:369, frontend/src/lib/replaygain.ts:80 |
| `ListDirectoryFiles` | `app.go:2886` | dirPath string | ([]backend.FileInfo, error) | `(arg1:string)=>Promise<Array<models.backend.FileInfo>>` | frontend/src/components/EnrichPage.tsx:100, frontend/src/components/FileManagerPage.tsx:151 |
| `ListAudioFilesInDir` | `app.go:2893` | dirPath string | ([]backend.FileInfo, error) | `(arg1:string)=>Promise<Array<models.backend.FileInfo>>` | frontend/src/components/AudioAnalysisPage.tsx:393, frontend/src/components/AudioConverterPage.tsx:135, frontend/src/components/AudioResamplerPage.tsx:150, frontend/src/components/ReplayGainPage.tsx:277, frontend/src/components/TempoKeyAnalyzerPage.tsx:288 |
| `ReadFileMetadata` | `app.go:2900` | filePath string | (*backend.AudioMetadata, error) | `(arg1:string)=>Promise<models.backend.AudioMetadata>` | frontend/src/components/FileManagerPage.tsx:270 |
| `ReadEmbeddedLyrics` | `app.go:2907` | filePath string | (*backend.EmbeddedLyrics, error) | `(arg1:string)=>Promise<models.backend.EmbeddedLyrics>` | frontend/src/components/LyricsManagerPage.tsx:126 |
| `ExtractLyricsToLRC` | `app.go:2914` | filePath string, overwrite bool | (*backend.ExtractLyricsResult, error) | `(arg1:string,arg2:boolean)=>Promise<models.backend.ExtractLyricsResult>` | frontend/src/components/LyricsManagerPage.tsx:292 |
| `SelectLyricsFiles` | `app.go:2921` | — | ([]string, error) | `()=>Promise<Array<string>>` | frontend/src/components/LyricsManagerPage.tsx:163 |
| `SelectLyricsFolder` | `app.go:2929` | — | (string, error) | `()=>Promise<string>` | frontend/src/components/LyricsManagerPage.tsx:176 |
| `ScanLyricsFolder` | `app.go:2933` | dir string | ([]string, error) | `(arg1:string)=>Promise<Array<string>>` | frontend/src/components/LyricsManagerPage.tsx:179 |
| `SaveLyrics` | `app.go:2940` | filePath string, lyrics string | (*backend.SaveLyricsResult, error) | `(arg1:string,arg2:string)=>Promise<models.backend.SaveLyricsResult>` | frontend/src/components/LyricsManagerPage.tsx:271 |
| `PreviewRenameFiles` | `app.go:2947` | files []string, format string | []backend.RenamePreview | `(arg1:Array<string>,arg2:string)=>Promise<Array<models.backend.RenamePreview>>` | frontend/src/components/FileManagerPage.tsx:253 |
| `RenameFilesByMetadata` | `app.go:2951` | files []string, format string | []backend.RenameResult | `(arg1:Array<string>,arg2:string)=>Promise<Array<models.backend.RenameResult>>` | frontend/src/components/FileManagerPage.tsx:405 |
| `ReadTextFile` | `app.go:2964` | filePath string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/components/FileManagerPage.tsx:290 |
| `ReadFileAsBase64` | `app.go:2972` | filePath string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/hooks/useAudioAnalysis.ts:288 (dynamic `window.go.main.App`) |
| `DecodeAudioForAnalysis` | `app.go:2981` | filePath string | (*backend.AnalysisDecodeResponse, error) | `(arg1:string)=>Promise<models.backend.AnalysisDecodeResponse>` | frontend/src/hooks/useAudioAnalysis.ts:345 (dynamic `window.go.main.App`) |
| `DecodeAudioForTempoKey` | `app.go:2989` | filePath string | (*backend.TempoKeyDecodeResponse, error) | `(arg1:string)=>Promise<models.backend.TempoKeyDecodeResponse>` | frontend/src/components/TempoKeyAnalyzerPage.tsx:160 |
| `RenameFileTo` | `app.go:2997` | oldPath string, newName string | error | `(arg1:string,arg2:string)=>Promise<void>` | frontend/src/components/FileManagerPage.tsx:388 |
| `SelectImageVideo` | `app.go:3011` | — | ([]string, error) | `()=>Promise<Array<string>>` | none |
| `ReadImageAsBase64` | `app.go:3015` | filePath string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/components/FileManagerPage.tsx:305 |
| `CheckFilesExistence` | `app.go:3182` | outputDir string, rootDir string, tracks []CheckFileExistenceRequest | []CheckFileExistenceResult | `(arg1:string,arg2:string,arg3:Array<models.main.CheckFileExistenceRequest>)=>Promise<Array<models.main.CheckFileExistenceResult>>` | frontend/src/hooks/useDownload.ts:1336, frontend/src/hooks/useDownload.ts:1629, frontend/src/hooks/useDownload.ts:202, frontend/src/hooks/useDownload.ts:404 |
| `SkipDownloadItem` | `app.go:3295` | itemID string, filePath string | — | `(arg1:string,arg2:string)=>Promise<void>` | frontend/src/hooks/useDownload.ts:1357, frontend/src/hooks/useDownload.ts:1655, frontend/src/hooks/useDownload.ts:203 |
| `GetTrackISRC` | `app.go:3299` | spotifyTrackID string | string | `(arg1:string)=>Promise<string>` | frontend/src/hooks/useDownload.ts:206, frontend/src/hooks/useDownload.ts:225, frontend/src/hooks/useLyrics.ts:28 |
| `GetPreviewURL` | `app.go:3303` | trackID string | (string, error) | `(arg1:string)=>Promise<string>` | frontend/src/components/HistoryPage.tsx:214, frontend/src/hooks/usePreview.ts:43 |
| `GetConfigPath` | `app.go:3307` | — | (string, error) | `()=>Promise<string>` | none |
| `GetFontsPath` | `app.go:3311` | — | (string, error) | `()=>Promise<string>` | none |
| `SaveSettings` | `app.go:3319` | settings map[string]interface{} | error | `(arg1:Record<string, any>)=>Promise<void>` | frontend/src/lib/settings.ts:795 |
| `SaveFonts` | `app.go:3323` | fonts []map[string]interface{} | error | `(arg1:Array<Record<string, any>>)=>Promise<void>` | frontend/src/lib/settings.ts:535 |
| `LoadSettings` | `app.go:3344` | — | (map[string]interface{}, error) | `()=>Promise<Record<string, any>>` | frontend/src/lib/settings.ts:830 |
| `LoadFonts` | `app.go:3348` | — | ([]map[string]interface{}, error) | `()=>Promise<Array<Record<string, any>>>` | frontend/src/lib/settings.ts:548 |
| `CheckFFmpegInstalled` | `app.go:3374` | — | (bool, error) | `()=>Promise<boolean>` | frontend/src/App.tsx:424 |
| `CreateM3U8File` | `app.go:3378` | m3u8Name string, outputDir string, filePaths []string | error | `(arg1:string,arg2:string,arg3:Array<string>)=>Promise<void>` | frontend/src/hooks/useDownload.ts:1474, frontend/src/hooks/useDownload.ts:1800, frontend/src/hooks/useDownload.ts:204 |
| `CreateLogFile` | `app.go:3401` | fileName string, outputDir string, logs []string | error | `(arg1:string,arg2:string,arg3:Array<string>)=>Promise<void>` | frontend/src/hooks/useDownload.ts:1518, frontend/src/hooks/useDownload.ts:1843, frontend/src/hooks/useDownload.ts:205 |

### `app_ipod.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `ListIPods` | `app_ipod.go:95` | — | ([]ipod.Device, error) | `()=>Promise<Array<models.ipod.Device>>` | frontend/src/App.tsx:303, frontend/src/components/DevicesPage.tsx:123 |
| `SendToIPod` | `app_ipod.go:102` | id string, paths []string, format string | (ipod.SendResult, error) | `(arg1:string,arg2:Array<string>,arg3:string)=>Promise<models.ipod.SendResult>` | frontend/src/components/DevicesPage.tsx:224 |
| `RemoveFromIPod` | `app_ipod.go:113` | id string, trackIDs []int | error | `(arg1:string,arg2:Array<number>)=>Promise<void>` | frontend/src/components/DevicesPage.tsx:275 |
| `EjectIPod` | `app_ipod.go:120` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/DevicesPage.tsx:262 |
| `DoctorIPod` | `app_ipod.go:127` | id string, action string | (ipod.DoctorReport, error) | `(arg1:string,arg2:string)=>Promise<models.ipod.DoctorReport>` | frontend/src/components/DevicesPage.tsx:192, frontend/src/components/DevicesPage.tsx:206, frontend/src/components/DevicesPage.tsx:292 |
| `CancelIPod` | `app_ipod.go:134` | — | — | `()=>Promise<void>` | frontend/src/components/DevicesPage.tsx:409 |

### `app_library.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `LibraryProfiles` | `app_library.go:38` | — | []library.Profile | `()=>Promise<Array<models.library.Profile>>` | frontend/src/components/LibraryHealthPage.tsx:89 |
| `StartLibraryScan` | `app_library.go:45` | root string, profileID string | error | `(arg1:string,arg2:string)=>Promise<void>` | frontend/src/components/LibraryHealthPage.tsx:174 |
| `CancelLibraryScan` | `app_library.go:88` | — | — | `()=>Promise<void>` | frontend/src/components/LibraryHealthPage.tsx:185 |
| `GetLibraryReport` | `app_library.go:98` | — | (*LibraryReport, error) | `()=>Promise<models.main.LibraryReport>` | frontend/src/components/LibraryHealthPage.tsx:97 |
| `PreviewLibraryFixes` | `app_library.go:108` | issueIDs []string | (*library.Plan, error) | `(arg1:Array<string>)=>Promise<models.library.Plan>` | frontend/src/components/LibraryHealthPage.tsx:224, frontend/src/components/LibraryHealthPage.tsx:246 |
| `ApplyLibraryPlan` | `app_library.go:128` | plan *library.Plan | (*library.ApplyResult, error) | `(arg1:models.library.Plan)=>Promise<models.library.ApplyResult>` | frontend/src/components/LibraryHealthPage.tsx:259 |
| `UndoLastLibraryFix` | `app_library.go:144` | — | (*library.ApplyResult, error) | `()=>Promise<models.library.ApplyResult>` | frontend/src/components/LibraryHealthPage.tsx:289 |
| `ExportLibraryPlaylist` | `app_library.go:155` | m3u8Path string, trackPaths []string, mode string, musicRoot string, devicePrefix string | (*library.PlaylistResult, error) | `(arg1:string,arg2:Array<string>,arg3:string,arg4:string,arg5:string)=>Promise<models.library.PlaylistResult>` | none |

### `app_sync.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `ListSyncTargets` | `app_sync.go:63` | — | ([]devices.SyncTargetView, error) | `()=>Promise<Array<models.devices.SyncTargetView>>` | frontend/src/components/AndroidSyncPanel.tsx:118, frontend/src/components/AndroidSyncPanel.tsx:139 |
| `AddFolderTarget` | `app_sync.go:70` | name string, path string | (devices.SyncTargetView, error) | `(arg1:string,arg2:string)=>Promise<models.devices.SyncTargetView>` | frontend/src/components/AndroidSyncPanel.tsx:353 |
| `RemoveFolderTarget` | `app_sync.go:75` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:378 |
| `GetDeviceProfile` | `app_sync.go:94` | id string | (devices.DeviceProfile, error) | `(arg1:string)=>Promise<models.devices.DeviceProfile>` | frontend/src/components/AndroidSyncPanel.tsx:218 |
| `SaveDeviceProfile` | `app_sync.go:99` | profile devices.DeviceProfile | error | `(arg1:models.devices.DeviceProfile)=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:265, frontend/src/components/AndroidSyncPanel.tsx:295 |
| `PlanSync` | `app_sync.go:138` | id string | (*syncengine.Plan, error) | `(arg1:string)=>Promise<models.syncengine.Plan>` | frontend/src/components/AndroidSyncPanel.tsx:297 |
| `StartSync` | `app_sync.go:255` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:318 |
| `CancelSync` | `app_sync.go:443` | — | — | `()=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:343 |
| `ResumeSync` | `app_sync.go:453` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:333 |
| `HasInterruptedSync` | `app_sync.go:480` | id string | bool | `(arg1:string)=>Promise<boolean>` | frontend/src/components/AndroidSyncPanel.tsx:109 |
| `DownloadPlatformTools` | `app_sync.go:490` | — | error | `()=>Promise<void>` | frontend/src/components/AndroidSyncPanel.tsx:278 |
| `IsPlatformToolsInstalled` | `app_sync.go:502` | — | bool | `()=>Promise<boolean>` | frontend/src/components/AndroidSyncPanel.tsx:149 |
| `SelectSyncPlaylist` | `app_sync.go:507` | title string, filterLabel string | (string, error) | `(arg1:string,arg2:string)=>Promise<string>` | frontend/src/components/sync/SelectionRulesEditor.tsx:90 |

### `app_taste.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `GetTasteSettings` | `app_taste.go:125` | — | (taste.Settings, error) | `()=>Promise<models.taste.Settings>` | frontend/src/App.tsx:237, frontend/src/components/ListeningConnectionsSettings.tsx:78 |
| `SetForYouEnabled` | `app_taste.go:134` | enabled bool | error | `(arg1:boolean)=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:126 |
| `SetSpotifyClientID` | `app_taste.go:143` | clientID string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:144, frontend/src/components/ListeningConnectionsSettings.tsx:158 |
| `ConnectSpotify` | `app_taste.go:152` | — | error | `()=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:160 |
| `DisconnectSpotify` | `app_taste.go:165` | — | error | `()=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:172 |
| `SetLastFMCredentials` | `app_taste.go:174` | apiKey string, username string | error | `(arg1:string,arg2:string)=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:183 |
| `DisconnectLastFM` | `app_taste.go:183` | — | error | `()=>Promise<void>` | frontend/src/components/ListeningConnectionsSettings.tsx:196 |
| `SelectSpotifyExportFile` | `app_taste.go:192` | — | (string, error) | `()=>Promise<string>` | frontend/src/components/ListeningConnectionsSettings.tsx:209 |
| `ImportSpotifyExport` | `app_taste.go:203` | path string | (int, error) | `(arg1:string)=>Promise<number>` | frontend/src/components/ListeningConnectionsSettings.tsx:211 |
| `SyncTasteNow` | `app_taste.go:220` | — | error | `()=>Promise<void>` | frontend/src/hooks/useTasteSync.ts:37 |
| `CancelTasteSync` | `app_taste.go:237` | — | — | `()=>Promise<void>` | frontend/src/hooks/useTasteSync.ts:45 |
| `GetTasteShelves` | `app_taste.go:246` | — | ([]taste.Shelf, error) | `()=>Promise<Array<models.taste.Shelf>>` | frontend/src/components/ForYouPage.tsx:92 |
| `DismissTasteItem` | `app_taste.go:259` | id string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/ForYouPage.tsx:183 |
| `BanTasteArtist` | `app_taste.go:268` | artist string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/ForYouPage.tsx:198 |
| `UnbanTasteArtist` | `app_taste.go:277` | artist string | error | `(arg1:string)=>Promise<void>` | none |
| `PinTasteArtist` | `app_taste.go:286` | artist string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/ForYouPage.tsx:214 |
| `UnpinTasteArtist` | `app_taste.go:295` | artist string | error | `(arg1:string)=>Promise<void>` | frontend/src/components/ForYouPage.tsx:224 |
| `GetTasteSummary` | `app_taste.go:304` | — | (taste.Summary, error) | `()=>Promise<models.taste.Summary>` | frontend/src/components/ForYouPage.tsx:93 |

### `close_lifecycle.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `FrontendReady` | `close_lifecycle.go:8` | — | — | `()=>Promise<void>` | frontend/src/main.tsx:36 |
| `FinishClose` | `close_lifecycle.go:11` | saved bool | — | `(arg1:boolean)=>Promise<void>` | frontend/src/main.tsx:34 |

### `source_priority.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `GetDownloadSources` | `source_priority.go:5` | — | []backend.DownloadSource | `()=>Promise<Array<models.backend.DownloadSource>>` | none |
| `GetSourceBenchmarks` | `source_priority.go:6` | — | []backend.SourceBenchmark | `()=>Promise<Array<models.backend.SourceBenchmark>>` | none |
| `RankDownloadServices` | `source_priority.go:7` | services []string, quality string | []string | `(arg1:Array<string>,arg2:string)=>Promise<Array<string>>` | frontend/src/lib/source-priority.ts:55 (dynamic `window.go.main.App`) |
| `GetResourceSources` | `source_priority.go:11` | — | []backend.DownloadSource | `()=>Promise<Array<models.backend.DownloadSource>>` | none |
| `ListCommunitySources` | `source_priority.go:13` | — | ([]backend.CommunitySource, error) | `()=>Promise<Array<models.backend.CommunitySource>>` | none |
| `SaveCommunitySources` | `source_priority.go:16` | sources []backend.CommunitySource | error | `(arg1:Array<models.backend.CommunitySource>)=>Promise<void>` | none |
| `GetCommunitySourceChecks` | `source_priority.go:19` | — | []backend.CommunitySourceCheck | `()=>Promise<Array<models.backend.CommunitySourceCheck>>` | none |
| `CheckCommunitySource` | `source_priority.go:22` | id string | (backend.CommunitySourceCheck, error) | `(arg1:string)=>Promise<models.backend.CommunitySourceCheck>` | none |
| `TestCommunitySourceDownload` | `source_priority.go:25` | id string | (backend.CommunitySourceCheck, error) | `(arg1:string)=>Promise<models.backend.CommunitySourceCheck>` | none |

### `verification_ui.go`

| name | Go | Go params | Go returns | TS binding | frontend call sites |
| --- | --- | --- | --- | --- | --- |
| `GetSourceVerification` | `verification_ui.go:14` | — | SourceVerificationView | `()=>Promise<models.main.SourceVerificationView>` | frontend/src/components/SourceVerificationPane.tsx:33 (dynamic `window.go.main.App`) |
| `SetSourceVerificationViewport` | `verification_ui.go:18` | id uint64, left int, top int, width int, height int | error | `(arg1:number,arg2:number,arg3:number,arg4:number,arg5:number)=>Promise<void>` | frontend/src/components/SourceVerificationPane.tsx:55, :70 (dynamic `window.go.main.App`) |
| `CancelSourceVerification` | `verification_ui.go:22` | id uint64 | error | `(arg1:number)=>Promise<void>` | frontend/src/components/SourceVerificationPane.tsx:78 (dynamic `window.go.main.App`) |
| `ConfirmSourceVerification` | `verification_ui.go:26` | — | (bool, error) | `()=>Promise<boolean>` | frontend/src/components/SourceVerificationPane.tsx:84 (dynamic `window.go.main.App`) |
| `GetSourceConnections` | `verification_ui.go:30` | — | []backend.SourceConnection | `()=>Promise<Array<models.backend.SourceConnection>>` | none |
| `CheckSourceConnection` | `verification_ui.go:31` | id string | (backend.SourceConnection, error) | `(arg1:string)=>Promise<models.backend.SourceConnection>` | none |
| `VerifySourceConnection` | `verification_ui.go:34` | id string | (backend.SourceConnection, error) | `(arg1:string)=>Promise<models.backend.SourceConnection>` | none |
| `VerifyCommunitySource` | `verification_ui.go:37` | id string | (backend.CommunitySourceCheck, error) | `(arg1:string)=>Promise<models.backend.CommunitySourceCheck>` | none |

Method rows: **163**.

### String results whose contents are JSON

The binding type stays `Promise<string>` (or a `string` argument). Wails encodes that Go string as one JSON string. The characters inside it are a document the frontend parses. Rust must return that document text. Returning a structured object, or quoting the document again, changes the value the frontend receives.

- `GetCurrentIPInfo` (`app.go:316`) returns `string(json.Marshal(info))` (`app.go:322-327`). `info` is `main.CurrentIPInfo` (`app.go:56`).
- `GetStreamingURLs` (`app.go:947`) returns `string(json.Marshal(urls))` (`app.go:959-964`). `urls` is `*backend.SongLinkURLs` (`backend/songlink.go:29`, assigned at `backend/songlink.go:109`).
- `GetSpotifyMetadata` (`app.go:967`) returns `string(json.MarshalIndent(data, "", "  "))` (`app.go:1017-1022`). `data` is the `interface{}` from `GetFilteredSpotifyData` (`backend/spotify_metadata.go:366`). After formatting, that value is a `TrackResponse` (`formatTrackData`, `spotify_metadata.go:1048`), a `*AlbumResponsePayload` (`formatAlbumData`, `spotify_metadata.go:1125`), a `PlaylistResponsePayload` (`formatPlaylistData`, `spotify_metadata.go:1226`), or a `*ArtistDiscographyPayload` (`formatArtistDiscographyData`, `spotify_metadata.go:1298`). A nil pointer would be JSON `null`.
- `GetRecentFetches` (`app.go:2339`) returns `string(json.Marshal(items))` (`app.go:2345-2350`). `items` is `[]backend.RecentFetchItem`. An empty file becomes `[]`, not `null` (`backend/recent_fetches.go:51-52`).
- `SaveRecentFetches` (`app.go:2353`) takes that same text. A blank argument is replaced with `[]` (`app.go:2355-2357`) and unmarshalled into `[]backend.RecentFetchItem` (`app.go:2359-2361`).
- `CheckTrackAvailability` (`app.go:2646`) returns `string(json.Marshal(availability))` (`app.go:2658-2663`). `availability` is `backend.TrackAvailability` (`backend/songlink.go:35`).
- `LoadPersistentDownloadQueue` (`app.go:1688`) returns the string from `backend.LoadPersistentDownloadQueue` (`backend/queue_db.go:389`). That string is a JSON array built at `backend/queue_db.go:180-196`, or `""` when the order key is missing (`backend/queue_db.go:172-174`). Each element is stored raw and must contain a string `id` (`backend/queue_db.go:96-105`). `ReplacePersistentDownloadQueue` (`app.go:1692`) and `ApplyPersistentDownloadQueueChanges` (`app.go:1696`) take those strings and do not re-shape them.
- `SaveSettings` (`app.go:3319`) and `LoadSettings` (`app.go:3344`) are `map[string]interface{}` on the binding, not strings. The file on disk is written by `MarshalConfigSettings` (`backend/config_layout.go:188`) through `orderedConfigObject.MarshalJSON` (`backend/config_layout.go:121`), which only fixes key order. That type is not an IPC payload.

Named types that appear only inside those documents (not in an `App` signature):

### `models.main.CurrentIPInfo`

Go `main.CurrentIPInfo`, defined at `app.go:56`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `IP` | 57 | `string` | ip | `string` |  |
| `Country` | 58 | `string` | country | `string` |  |
| `CountryCode` | 59 | `string` | country_code,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Source` | 60 | `string` | source,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.SongLinkURLs`

Go `backend.SongLinkURLs`, defined at `backend/songlink.go:29`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `TidalURL` | 30 | `string` | tidal_url | `string` |  |
| `AmazonURL` | 31 | `string` | amazon_url | `string` |  |
| `ISRC` | 32 | `string` | isrc | `string` |  |

### `models.backend.TrackAvailability`

Go `backend.TrackAvailability`, defined at `backend/songlink.go:35`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 36 | `string` | spotify_id | `string` |  |
| `Tidal` | 37 | `bool` | tidal | `boolean` |  |
| `Amazon` | 38 | `bool` | amazon | `boolean` |  |
| `Qobuz` | 39 | `bool` | qobuz | `boolean` |  |
| `Deezer` | 40 | `bool` | deezer | `boolean` |  |
| `TidalURL` | 41 | `string` | tidal_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AmazonURL` | 42 | `string` | amazon_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `QobuzURL` | 43 | `string` | qobuz_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `DeezerURL` | 44 | `string` | deezer_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.RecentFetchItem`

Go `backend.RecentFetchItem`, defined at `backend/recent_fetches.go:13`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 14 | `string` | id | `string` |  |
| `URL` | 15 | `string` | url | `string` |  |
| `Type` | 16 | `string` | type | `string` |  |
| `Name` | 17 | `string` | name | `string` |  |
| `Artist` | 18 | `string` | artist | `string` |  |
| `Image` | 19 | `string` | image | `string` |  |
| `IsExplicit` | 20 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Timestamp` | 21 | `int64` | timestamp | `number` |  |

### `models.backend.TrackResponse`

Go `backend.TrackResponse`, defined at `backend/spotify_metadata.go:96`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Track` | 97 | `TrackMetadata` | track | `models.backend.TrackMetadata` |  |

### `models.backend.TrackMetadata`

Go `backend.TrackMetadata`, defined at `backend/spotify_metadata.go:35`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 36 | `string` | spotify_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Artists` | 37 | `string` | artists | `string` |  |
| `Name` | 38 | `string` | name | `string` |  |
| `AlbumName` | 39 | `string` | album_name | `string` |  |
| `AlbumArtist` | 40 | `string` | album_artist,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `DurationMS` | 41 | `int` | duration_ms | `number` |  |
| `Images` | 42 | `string` | images | `string` |  |
| `ReleaseDate` | 43 | `string` | release_date | `string` |  |
| `TrackNumber` | 44 | `int` | track_number | `number` |  |
| `TotalTracks` | 45 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `DiscNumber` | 46 | `int` | disc_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalDiscs` | 47 | `int` | total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `ExternalURL` | 48 | `string` | external_urls | `string` |  |
| `AlbumID` | 49 | `string` | album_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumURL` | 50 | `string` | album_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistID` | 51 | `string` | artist_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistURL` | 52 | `string` | artist_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistsData` | 53 | `[]ArtistSimple` | artists_data,omitempty | `models.backend.ArtistSimple[]` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 54 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Copyright` | 55 | `string` | copyright,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Publisher` | 56 | `string` | publisher,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Composer` | 57 | `string` | composer,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Plays` | 58 | `string` | plays,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `PreviewURL` | 59 | `string` | preview_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `IsExplicit` | 60 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ArtistSimple`

Go `backend.ArtistSimple`, defined at `backend/spotify_metadata.go:63`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 64 | `string` | id | `string` |  |
| `Name` | 65 | `string` | name | `string` |  |
| `ExternalURL` | 66 | `string` | external_urls | `string` |  |

### Dev harness stubs (`frontend/src/preview.tsx`)

The preview entry replaces `window.runtime` (`preview.tsx:37`) and `window.go.main.App` (`preview.tsx:46`) with proxies. Every `name === "..."` branch in that file is listed below. `EventsOnMultiple` and `ClipboardGetText` belong to the runtime proxy. `GetHistory` and `GetPlatform` are not exported `App` methods. The App proxy returns `undefined` for every other name (`preview.tsx:107`). These branches are not production call sites.

`EventsOnMultiple`, `ClipboardGetText`, `GetTasteSettings`, `SetForYouEnabled`, `GetTasteShelves`, `GetTasteSummary`, `DismissTasteItem`, `BanTasteArtist`, `PinTasteArtist`, `UnpinTasteArtist`, `SetSpotifyClientID`, `ConnectSpotify`, `DisconnectSpotify`, `SetLastFMCredentials`, `DisconnectLastFM`, `SelectSpotifyExportFile`, `SelectFolder`, `ImportSpotifyExport`, `SyncTasteNow`, `CancelTasteSync`, `LoadPersistentDownloadQueue`, `ReplacePersistentDownloadQueue`, `GetRecentFetches`, `CheckFFmpegInstalled`, `LoadSettings`, `GetDefaults`, `LoadFonts`, `ListCommunitySources`, `GetCommunitySourceChecks`, `GetDownloadProgress`, `GetSpotifyMetadata`, `SearchSpotify`, `CheckFilesExistence`, `GetStreamingURLs`, `GetHistory`, `GetDownloadHistory`, `GetFetchHistory`, `DownloadTrack`, `ForceStopDownloads`, `AddToDownloadQueue`, `GetPlatform`

## 3. Types that cross the boundary

Every named type that appears in an exported `App` method parameter or result, and every named type reachable through an exported field of those types (including `json:"-"` fields and embedded structs). Standard-library types are described on the field instead of getting a section. JSON is the serde shape Rust must match. TS is the Wails models.ts field type.

Named types: **83**.

### `models.backend.AnalysisDecodeResponse`

Go `backend.AnalysisDecodeResponse`, defined at `backend/analysis.go:30`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `PCMBase64` | 31 | `string` | pcm_base64 | `string` |  |
| `SampleRate` | 32 | `uint32` | sample_rate | `number` |  |
| `Channels` | 33 | `uint8` | channels | `number` |  |
| `BitsPerSample` | 34 | `uint8` | bits_per_sample | `number` |  |
| `Duration` | 35 | `float64` | duration | `number` |  |
| `BitrateKbps` | 36 | `int` | bitrate_kbps,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `BitDepth` | 37 | `string` | bit_depth,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.AudioMetadata`

Go `backend.AudioMetadata`, defined at `backend/filemanager.go:27`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Title` | 28 | `string` | title | `string` |  |
| `Artist` | 29 | `string` | artist | `string` |  |
| `Album` | 30 | `string` | album | `string` |  |
| `AlbumArtist` | 31 | `string` | album_artist | `string` |  |
| `TrackNumber` | 32 | `int` | track_number | `number` |  |
| `DiscNumber` | 33 | `int` | disc_number | `number` |  |
| `Year` | 34 | `string` | year | `string` |  |
| `ISRC` | 35 | `string` | isrc | `string` |  |
| `UPC` | 36 | `string` | upc | `string` |  |

### `models.backend.AvatarDownloadResponse`

Go `backend.AvatarDownloadResponse`, defined at `backend/cover.go:466`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 467 | `bool` | success | `boolean` |  |
| `Message` | 468 | `string` | message | `string` |  |
| `File` | 469 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 470 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 471 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.CommunitySource`

Go `backend.CommunitySource`, defined at `backend/community_sources.go:16`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 17 | `string` | id | `string` |  |
| `Name` | 18 | `string` | name | `string` |  |
| `Service` | 19 | `string` | service | `string` |  |
| `Protocol` | 20 | `string` | protocol | `string` |  |
| `BaseURL` | 21 | `string` | base_url | `string` |  |
| `Enabled` | 22 | `bool` | enabled | `boolean` |  |
| `CredentialEnv` | 23 | `string` | credential_env,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `CredentialType` | 24 | `string` | credential_type,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.CommunitySourceCheck`

Go `backend.CommunitySourceCheck`, defined at `backend/community_source_checks.go:22`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 23 | `string` | id | `string` |  |
| `State` | 24 | `string` | state | `string` |  |
| `Message` | 25 | `string` | message | `string` |  |
| `LatencyMS` | 26 | `float64` | latency_ms | `number` |  |
| `AudioVerified` | 27 | `bool` | audio_verified | `boolean` |  |
| `CheckedAt` | 28 | `string` | checked_at | `string` |  |
| `Action` | 31 | `string` | action,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ConvertAudioResult`

Go `backend.ConvertAudioResult`, defined at `backend/ffmpeg.go:904`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `InputFile` | 905 | `string` | input_file | `string` |  |
| `OutputFile` | 906 | `string` | output_file | `string` |  |
| `Success` | 907 | `bool` | success | `boolean` |  |
| `Error` | 908 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.CoverDownloadResponse`

Go `backend.CoverDownloadResponse`, defined at `backend/cover.go:47`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 48 | `bool` | success | `boolean` |  |
| `Message` | 49 | `string` | message | `string` |  |
| `File` | 50 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 51 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 52 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.DownloadItem`

Go `backend.DownloadItem`, defined at `backend/progress.go:20`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 21 | `string` | id | `string` |  |
| `TrackName` | 22 | `string` | track_name | `string` |  |
| `ArtistName` | 23 | `string` | artist_name | `string` |  |
| `AlbumName` | 24 | `string` | album_name | `string` |  |
| `SpotifyID` | 25 | `string` | spotify_id | `string` |  |
| `Status` | 26 | `DownloadStatus` | status | `string` |  |
| `Progress` | 27 | `float64` | progress | `number` |  |
| `TotalSize` | 28 | `float64` | total_size | `number` |  |
| `Speed` | 29 | `float64` | speed | `number` |  |
| `StartTime` | 30 | `int64` | start_time | `number` |  |
| `EndTime` | 31 | `int64` | end_time | `number` |  |
| `ErrorMessage` | 32 | `string` | error_message | `string` |  |
| `FilePath` | 33 | `string` | file_path | `string` |  |

### `models.backend.DownloadQueueInfo`

Go `backend.DownloadQueueInfo`, defined at `backend/progress.go:73`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `IsDownloading` | 74 | `bool` | is_downloading | `boolean` |  |
| `Queue` | 75 | `[]DownloadItem` | queue | `models.backend.DownloadItem[]` |  |
| `CurrentSpeed` | 76 | `float64` | current_speed | `number` |  |
| `TotalDownloaded` | 77 | `float64` | total_downloaded | `number` |  |
| `SessionStartTime` | 78 | `int64` | session_start_time | `number` |  |
| `QueuedCount` | 79 | `int` | queued_count | `number` |  |
| `CompletedCount` | 80 | `int` | completed_count | `number` |  |
| `FailedCount` | 81 | `int` | failed_count | `number` |  |
| `SkippedCount` | 82 | `int` | skipped_count | `number` |  |

### `models.backend.DownloadSource`

Go `backend.DownloadSource`, defined at `backend/source_priority.go:31`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 32 | `string` | id | `string` |  |
| `Service` | 33 | `string` | service | `string` |  |
| `Name` | 34 | `string` | name | `string` |  |
| `Role` | 35 | `string` | role | `string` |  |
| `URL` | 36 | `string` | url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.DownloadStatus`

Go `backend.DownloadStatus`, defined at `backend/progress.go:10`.

Not a struct. Underlying type: `string`.

Wails uses this name in a method signature (`models.backend.DownloadStatus`) and uses the underlying type for a struct field (`string`).

### `models.backend.EmbeddedLyrics`

Go `backend.EmbeddedLyrics`, defined at `backend/lyrics_reader.go:19`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 20 | `string` | path | `string` |  |
| `Name` | 21 | `string` | name | `string` |  |
| `Lyrics` | 22 | `string` | lyrics | `string` |  |
| `Source` | 23 | `string` | source | `string` |  |
| `Synced` | 24 | `bool` | synced | `boolean` |  |
| `Error` | 25 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.EnrichMetadataPreview`

Go `backend.EnrichMetadataPreview`, defined at `backend/enrich.go:20`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `FilePath` | 21 | `string` | file_path | `string` |  |
| `Title` | 22 | `string` | title | `string` |  |
| `Artist` | 23 | `string` | artist | `string` |  |
| `Album` | 24 | `string` | album | `string` |  |
| `AlbumArtist` | 25 | `string` | album_artist | `string` |  |
| `Date` | 26 | `string` | date | `string` |  |
| `TrackNumber` | 27 | `int` | track_number | `number` |  |
| `TotalTracks` | 28 | `int` | total_tracks | `number` |  |
| `DiscNumber` | 29 | `int` | disc_number | `number` |  |
| `TotalDiscs` | 30 | `int` | total_discs | `number` |  |
| `Copyright` | 31 | `string` | copyright | `string` |  |
| `Publisher` | 32 | `string` | publisher | `string` |  |
| `Composer` | 33 | `string` | composer | `string` |  |
| `ISRC` | 34 | `string` | isrc | `string` |  |
| `UPC` | 35 | `string` | upc | `string` |  |
| `Genre` | 36 | `string` | genre | `string` |  |
| `Comment` | 37 | `string` | comment | `string` |  |
| `LyricsPresent` | 38 | `bool` | lyrics_present | `boolean` |  |
| `Eligible` | 39 | `bool` | eligible | `boolean` |  |
| `MissingISRC` | 40 | `bool` | missing_isrc | `boolean` |  |
| `MissingURL` | 41 | `bool` | missing_url | `boolean` |  |
| `InspectionErr` | 42 | `string` | inspection_error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.EnrichResult`

Go `backend.EnrichResult`, defined at `backend/enrich.go:12`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `FilePath` | 13 | `string` | file_path | `string` |  |
| `Status` | 14 | `string` | status | `string` |  |
| `Source` | 15 | `string` | source,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Message` | 16 | `string` | message,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ISRC` | 17 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ExtractLyricsResult`

Go `backend.ExtractLyricsResult`, defined at `backend/lyrics_reader.go:201`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 202 | `string` | path | `string` |  |
| `OutputPath` | 203 | `string` | output_path,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Success` | 204 | `bool` | success | `boolean` |  |
| `Error` | 205 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 206 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.FetchHistoryItem`

Go `backend.FetchHistoryItem`, defined at `backend/history.go:189`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 190 | `string` | id | `string` |  |
| `URL` | 191 | `string` | url | `string` |  |
| `Type` | 192 | `string` | type | `string` |  |
| `Name` | 193 | `string` | name | `string` |  |
| `Info` | 194 | `string` | info | `string` |  |
| `Image` | 195 | `string` | image | `string` |  |
| `Data` | 196 | `string` | data | `string` |  |
| `IsExplicit` | 197 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Timestamp` | 198 | `int64` | timestamp | `number` |  |

### `models.backend.FileInfo`

Go `backend.FileInfo`, defined at `backend/filemanager.go:19`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Name` | 20 | `string` | name | `string` |  |
| `Path` | 21 | `string` | path | `string` |  |
| `IsDir` | 22 | `bool` | is_dir | `boolean` |  |
| `Size` | 23 | `int64` | size | `number` |  |
| `Children` | 24 | `[]FileInfo` | children,omitempty | `models.backend.FileInfo[]` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.FlacInfo`

Go `backend.FlacInfo`, defined at `backend/resample.go:16`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 17 | `string` | path | `string` |  |
| `SampleRate` | 18 | `uint32` | sample_rate | `number` |  |
| `BitsPerSample` | 19 | `uint8` | bits_per_sample | `number` |  |

### `models.backend.GalleryImageDownloadResponse`

Go `backend.GalleryImageDownloadResponse`, defined at `backend/cover.go:370`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 371 | `bool` | success | `boolean` |  |
| `Message` | 372 | `string` | message | `string` |  |
| `File` | 373 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 374 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 375 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.HeaderDownloadResponse`

Go `backend.HeaderDownloadResponse`, defined at `backend/cover.go:61`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 62 | `bool` | success | `boolean` |  |
| `Message` | 63 | `string` | message | `string` |  |
| `File` | 64 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 65 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 66 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.HistoryItem`

Go `backend.HistoryItem`, defined at `backend/history.go:15`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 16 | `string` | id | `string` |  |
| `SpotifyID` | 17 | `string` | spotify_id | `string` |  |
| `Title` | 18 | `string` | title | `string` |  |
| `Artists` | 19 | `string` | artists | `string` |  |
| `Album` | 20 | `string` | album | `string` |  |
| `DurationStr` | 21 | `string` | duration_str | `string` |  |
| `CoverURL` | 22 | `string` | cover_url | `string` |  |
| `Quality` | 23 | `string` | quality | `string` |  |
| `Format` | 24 | `string` | format | `string` |  |
| `Path` | 25 | `string` | path | `string` |  |
| `Source` | 26 | `string` | source | `string` |  |
| `Timestamp` | 27 | `int64` | timestamp | `number` |  |

### `models.backend.LyricsDownloadResponse`

Go `backend.LyricsDownloadResponse`, defined at `backend/lyrics.go:66`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 67 | `bool` | success | `boolean` |  |
| `Message` | 68 | `string` | message | `string` |  |
| `File` | 69 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 70 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 71 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ProgressInfo`

Go `backend.ProgressInfo`, defined at `backend/progress.go:61`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `IsDownloading` | 62 | `bool` | is_downloading | `boolean` |  |
| `MBDownloaded` | 63 | `float64` | mb_downloaded | `number` |  |
| `SpeedMBps` | 64 | `float64` | speed_mbps | `number` |  |
| `RateLimited` | 65 | `bool` | rate_limited | `boolean` |  |
| `RateLimitSecs` | 66 | `int` | rate_limit_secs | `number` |  |
| `Cooldown` | 67 | `bool` | cooldown | `boolean` |  |
| `CooldownSecs` | 68 | `int` | cooldown_secs | `number` |  |
| `CooldownMessage` | 69 | `string` | cooldown_message | `string` |  |
| `CooldownEventID` | 70 | `int64` | cooldown_event_id | `number` |  |

### `models.backend.RenamePreview`

Go `backend.RenamePreview`, defined at `backend/filemanager.go:39`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `OldPath` | 40 | `string` | old_path | `string` |  |
| `OldName` | 41 | `string` | old_name | `string` |  |
| `NewName` | 42 | `string` | new_name | `string` |  |
| `NewPath` | 43 | `string` | new_path | `string` |  |
| `Error` | 44 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Metadata` | 45 | `AudioMetadata` | metadata | `models.backend.AudioMetadata` |  |

### `models.backend.RenameResult`

Go `backend.RenameResult`, defined at `backend/filemanager.go:48`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `OldPath` | 49 | `string` | old_path | `string` |  |
| `NewPath` | 50 | `string` | new_path | `string` |  |
| `Success` | 51 | `bool` | success | `boolean` |  |
| `Error` | 52 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ReplayGainAnalysisResult`

Go `backend.ReplayGainAnalysisResult`, defined at `backend/replaygain.go:25`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `FilePath` | 26 | `string` | file_path | `string` |  |
| `FileName` | 27 | `string` | file_name | `string` |  |
| `Duration` | 28 | `float64` | duration | `number` |  |
| `IntegratedLoudness` | 29 | `float64` | integrated_loudness | `number` |  |
| `TruePeak` | 30 | `float64` | true_peak | `number` |  |
| `SamplePeak` | 31 | `float64` | sample_peak | `number` |  |
| `LoudnessRange` | 32 | `float64` | loudness_range | `number` |  |
| `Threshold` | 33 | `float64` | threshold | `number` |  |
| `Success` | 34 | `bool` | success | `boolean` |  |
| `Error` | 35 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ReplayGainTagWrite`

Go `backend.ReplayGainTagWrite`, defined at `backend/replaygain.go:38`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `FilePath` | 39 | `string` | file_path | `string` |  |
| `TrackGainDB` | 40 | `float64` | track_gain_db | `number` |  |
| `TrackPeak` | 41 | `float64` | track_peak | `number` |  |
| `AlbumGainDB` | 42 | `*float64` | album_gain_db,omitempty | `number` | pointer; omitempty; nil omits the key; Wails marks the property optional |
| `AlbumPeak` | 43 | `*float64` | album_peak,omitempty | `number` | pointer; omitempty; nil omits the key; Wails marks the property optional |

### `models.backend.ReplayGainWriteResult`

Go `backend.ReplayGainWriteResult`, defined at `backend/replaygain.go:46`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `FilePath` | 47 | `string` | file_path | `string` |  |
| `Success` | 48 | `bool` | success | `boolean` |  |
| `Error` | 49 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ResampleResult`

Go `backend.ResampleResult`, defined at `backend/resample.go:91`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `InputFile` | 92 | `string` | input_file | `string` |  |
| `OutputFile` | 93 | `string` | output_file | `string` |  |
| `Success` | 94 | `bool` | success | `boolean` |  |
| `Error` | 95 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.SaveLyricsResult`

Go `backend.SaveLyricsResult`, defined at `backend/lyrics_reader.go:314`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 315 | `string` | path | `string` |  |
| `Success` | 316 | `bool` | success | `boolean` |  |
| `Error` | 317 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.SearchResponse`

Go `backend.SearchResponse`, defined at `backend/spotify_metadata.go:359`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Tracks` | 360 | `[]SearchResult` | tracks | `models.backend.SearchResult[]` |  |
| `Albums` | 361 | `[]SearchResult` | albums | `models.backend.SearchResult[]` |  |
| `Artists` | 362 | `[]SearchResult` | artists | `models.backend.SearchResult[]` |  |
| `Playlists` | 363 | `[]SearchResult` | playlists | `models.backend.SearchResult[]` |  |

### `models.backend.SearchResult`

Go `backend.SearchResult`, defined at `backend/spotify_metadata.go:344`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 345 | `string` | id | `string` |  |
| `Name` | 346 | `string` | name | `string` |  |
| `Type` | 347 | `string` | type | `string` |  |
| `Artists` | 348 | `string` | artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumName` | 349 | `string` | album_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Images` | 350 | `string` | images | `string` |  |
| `ReleaseDate` | 351 | `string` | release_date,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ExternalURL` | 352 | `string` | external_urls | `string` |  |
| `Duration` | 353 | `int` | duration_ms,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalTracks` | 354 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `Owner` | 355 | `string` | owner,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `IsExplicit` | 356 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.SourceBenchmark`

Go `backend.SourceBenchmark`, defined at `backend/source_priority.go:19`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 20 | `string` | id | `string` |  |
| `Service` | 21 | `string` | service | `string` |  |
| `Quality` | 22 | `string` | quality | `string` |  |
| `State` | 23 | `string` | state | `string` |  |
| `Attempts` | 24 | `int` | attempts | `number` |  |
| `Successes` | 25 | `int` | successes | `number` |  |
| `MedianMS` | 26 | `float64` | median_ms | `number` |  |
| `SamplesMS` | 27 | `[]float64` | samples_ms,omitempty | `number[]` | omitempty; zero value omits the key; Wails marks the property optional |
| `CheckedAt` | 28 | `time.Time` | checked_at | `any` | serde string RFC3339; Wails models.ts types time.Time as any |

### `models.backend.SourceConnection`

Go `backend.SourceConnection`, defined at `backend/source_connections.go:15`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 16 | `string` | id | `string` |  |
| `Name` | 17 | `string` | name | `string` |  |
| `Service` | 18 | `string` | service | `string` |  |
| `Role` | 19 | `string` | role | `string` |  |
| `State` | 20 | `string` | state | `string` |  |
| `Message` | 21 | `string` | message | `string` |  |
| `CanVerify` | 22 | `bool` | can_verify | `boolean` |  |
| `CheckedAt` | 23 | `string` | checked_at,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.TempoKeyDecodeResponse`

Go `backend.TempoKeyDecodeResponse`, defined at `backend/analysis.go:42`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `PCMBase64` | 43 | `string` | pcm_base64 | `string` |  |
| `SampleRate` | 44 | `uint32` | sample_rate | `number` |  |
| `Duration` | 45 | `float64` | duration | `number` |  |
| `FileSize` | 46 | `int64` | file_size | `number` |  |

### `models.backend.VerificationPresentation`

Go `backend.VerificationPresentation`, defined at `backend/in_app_verification.go:15`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 16 | `uint64` | id | `number` |  |
| `Active` | 17 | `bool` | active | `boolean` |  |
| `Title` | 18 | `string` | title | `string` |  |
| `Host` | 19 | `string` | host | `string` |  |
| `Ready` | 20 | `bool` | ready | `boolean` |  |
| `Error` | 21 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.devices.DeviceProfile`

Go `devices.DeviceProfile`, defined at `backend/devices/profile.go:9`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `DeviceID` | 10 | `string` | device_id | `string` |  |
| `FriendlyName` | 11 | `string` | friendly_name | `string` |  |
| `TargetFolder` | 12 | `string` | target_folder | `string` |  |
| `ProfileID` | 13 | `string` | profile_id | `string` |  |
| `FormatPolicy` | 14 | `syncengine.FormatPolicy` | format_policy | `models.syncengine.FormatPolicy` |  |
| `SelectionRules` | 15 | `syncengine.Selection` | selection_rules | `models.syncengine.Selection` |  |
| `LastManifestVersion` | 16 | `int` | last_manifest_version | `number` |  |
| `LastSyncTime` | 17 | `int64` | last_sync_time | `number` |  |

### `models.devices.SyncTargetView`

Go `devices.SyncTargetView`, defined at `backend/devices/profile.go:28`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 29 | `string` | id | `string` |  |
| `Name` | 30 | `string` | name | `string` |  |
| `Kind` | 31 | `string` | kind | `string` |  |
| `Model` | 32 | `string` | model | `string` |  |
| `Root` | 33 | `string` | root | `string` |  |
| `Connected` | 34 | `bool` | connected | `boolean` |  |
| `FreeBytes` | 35 | `int64` | freeBytes | `number` |  |
| `TotalBytes` | 36 | `int64` | totalBytes | `number` |  |
| `Profile` | 37 | `DeviceProfile` | profile | `models.devices.DeviceProfile` |  |

### `models.ipod.Device`

Go `ipod.Device`, defined at `backend/devices/ipod/device.go:11`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 12 | `string` | id | `string` |  |
| `Name` | 13 | `string` | name | `string` |  |
| `Mount` | 14 | `string` | mount | `string` |  |
| `Mode` | 15 | `string` | mode | `string` |  |
| `CanSend` | 16 | `bool` | canSend | `boolean` |  |
| `Warning` | 17 | `string` | warning | `string` |  |
| `HasRockbox` | 18 | `bool` | hasRockbox | `boolean` |  |
| `Generation` | 19 | `string` | generation | `string` |  |
| `Checksum` | 20 | `string` | checksum | `string` |  |
| `FirewireID` | 21 | `string` | firewireId | `string` |  |
| `TotalBytes` | 22 | `uint64` | totalBytes | `number` |  |
| `FreeBytes` | 23 | `uint64` | freeBytes | `number` |  |
| `Tracks` | 24 | `[]DeviceTrack` | tracks | `models.ipod.DeviceTrack[]` |  |
| `TrackCount` | 25 | `int` | trackCount | `number` |  |

### `models.ipod.DeviceTrack`

Go `ipod.DeviceTrack`, defined at `backend/devices/ipod/device.go:29`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 30 | `int` | id | `number` |  |
| `Title` | 31 | `string` | title | `string` |  |
| `Artist` | 32 | `string` | artist | `string` |  |
| `Album` | 33 | `string` | album | `string` |  |
| `Path` | 34 | `string` | path | `string` |  |
| `Size` | 35 | `uint64` | size | `number` |  |

### `models.ipod.DoctorReport`

Go `ipod.DoctorReport`, defined at `backend/devices/ipod/sync.go:36`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Orphans` | 37 | `[]DeviceTrack` | orphans | `models.ipod.DeviceTrack[]` |  |
| `Missing` | 38 | `[]DeviceTrack` | missing | `models.ipod.DeviceTrack[]` |  |
| `Changed` | 39 | `bool` | changed | `boolean` |  |

### `models.ipod.SendResult`

Go `ipod.SendResult`, defined at `backend/devices/ipod/sync.go:30`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Sent` | 31 | `int` | sent | `number` |  |
| `Skipped` | 32 | `int` | skipped | `number` |  |

### `models.library.ApplyResult`

Go `library.ApplyResult`, defined at `backend/library/plan.go:41`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `BatchID` | 42 | `string` | batch_id | `string` |  |
| `Applied` | 43 | `int` | applied | `number` |  |
| `Skipped` | 44 | `int` | skipped | `number` |  |
| `Errors` | 45 | `[]string` | errors | `string[]` |  |

### `models.library.ArtistMode`

Go `library.ArtistMode`, defined at `backend/library/profiles.go:13`.

Not a struct. Underlying type: `string`.

Wails uses this name in a method signature (`models.library.ArtistMode`) and uses the underlying type for a struct field (`string`).

### `models.library.Issue`

Go `library.Issue`, defined at `backend/library/doctor.go:20`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 21 | `string` | id | `string` |  |
| `RuleID` | 22 | `string` | rule_id | `string` |  |
| `Severity` | 23 | `Severity` | severity | `string` |  |
| `Path` | 24 | `string` | path | `string` |  |
| `AlbumDir` | 25 | `string` | album_dir | `string` |  |
| `Message` | 26 | `string` | message | `string` |  |
| `Fixable` | 27 | `bool` | fixable | `boolean` |  |

### `models.library.Operation`

Go `library.Operation`, defined at `backend/library/plan.go:17`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Type` | 18 | `string` | type | `string` |  |
| `Path` | 19 | `string` | path | `string` |  |
| `NewPath` | 20 | `string` | new_path,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Set` | 21 | `map[string]string` | set,omitempty | `Record<string, string>` | omitempty; zero value omits the key; Wails marks the property optional |
| `Old` | 25 | `map[string]string` | old,omitempty | `Record<string, string>` | omitempty; zero value omits the key; Wails marks the property optional |
| `Delete` | 26 | `[]string` | delete,omitempty | `string[]` | omitempty; zero value omits the key; Wails marks the property optional |
| `Reason` | 27 | `string` | reason | `string` |  |
| `Error` | 28 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Size` | 31 | `int64` | size | `number` |  |
| `ModTime` | 32 | `int64` | mod_time_unix_nano | `number` |  |

### `models.library.Plan`

Go `library.Plan`, defined at `backend/library/plan.go:36`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Operations` | 37 | `[]Operation` | operations | `models.library.Operation[]` |  |

### `models.library.PlaylistResult`

Go `library.PlaylistResult`, defined at `backend/library/playlists.go:45`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 46 | `string` | path | `string` |  |
| `Written` | 47 | `int` | written | `number` |  |
| `Skipped` | 48 | `[]PlaylistSkip` | skipped | `models.library.PlaylistSkip[]` |  |

### `models.library.PlaylistSkip`

Go `library.PlaylistSkip`, defined at `backend/library/playlists.go:39`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 40 | `string` | path | `string` |  |
| `Reason` | 41 | `string` | reason | `string` |  |

### `models.library.Profile`

Go `library.Profile`, defined at `backend/library/profiles.go:24`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 25 | `string` | id | `string` |  |
| `Label` | 26 | `string` | label | `string` |  |
| `FolderTemplate` | 29 | `string` | folder_template | `string` |  |
| `FileTemplate` | 30 | `string` | file_template | `string` |  |
| `MaxCoverPx` | 32 | `int` | max_cover_px | `number` |  |
| `BaselineJPEG` | 34 | `bool` | baseline_jpeg | `boolean` |  |
| `FolderCover` | 35 | `bool` | folder_cover | `boolean` |  |
| `LRCSidecars` | 36 | `bool` | lrc_sidecars | `boolean` |  |
| `ArtistMode` | 37 | `ArtistMode` | artist_mode | `string` |  |
| `ArtistSeparator` | 38 | `string` | artist_separator | `string` |  |
| `PlaylistDir` | 40 | `string` | playlist_dir | `string` |  |
| `FATSafe` | 42 | `bool` | fat_safe | `boolean` |  |
| `MaxComponentBytes` | 44 | `int` | max_component_bytes | `number` |  |

### `models.library.Severity`

Go `library.Severity`, defined at `backend/library/doctor.go:11`.

Not a struct. Underlying type: `string`.

Wails uses this name in a method signature (`models.library.Severity`) and uses the underlying type for a struct field (`string`).

### `models.main.APIStatusReport`

Go `main.APIStatusReport`, defined at `app.go:70`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Type` | 71 | `string` | type | `string` |  |
| `Online` | 72 | `bool` | online | `boolean` |  |
| `RequireAll` | 73 | `bool` | require_all | `boolean` |  |
| `Details` | 74 | `[]APIStatusTargetResult` | details | `models.main.APIStatusTargetResult[]` |  |

### `models.main.APIStatusTargetResult`

Go `main.APIStatusTargetResult`, defined at `app.go:63`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Target` | 64 | `string` | target | `string` |  |
| `Label` | 65 | `string` | label | `string` |  |
| `Online` | 66 | `bool` | online | `boolean` |  |
| `Message` | 67 | `string` | message,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.AvatarDownloadRequest`

Go `main.AvatarDownloadRequest`, defined at `app.go:2607`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `AvatarURL` | 2608 | `string` | avatar_url | `string` |  |
| `ArtistName` | 2609 | `string` | artist_name | `string` |  |
| `OutputDir` | 2610 | `string` | output_dir | `string` |  |

### `models.main.CheckFileExistenceRequest`

Go `main.CheckFileExistenceRequest`, defined at `app.go:3040`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 3041 | `string` | spotify_id | `string` |  |
| `TrackName` | 3042 | `string` | track_name | `string` |  |
| `ArtistName` | 3043 | `string` | artist_name | `string` |  |
| `Artists` | 3044 | `string` | artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumName` | 3045 | `string` | album_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumArtist` | 3046 | `string` | album_artist,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumArtists` | 3047 | `string` | album_artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Category` | 3048 | `string` | category,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 3049 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ReleaseDate` | 3050 | `string` | release_date,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ISRC` | 3051 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackNumber` | 3052 | `int` | track_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `DiscNumber` | 3053 | `int` | disc_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalTracks` | 3054 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalDiscs` | 3055 | `int` | total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `Position` | 3056 | `int` | position,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `UseAlbumTrackNumber` | 3057 | `bool` | use_album_track_number,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `FilenameFormat` | 3058 | `string` | filename_format,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `IncludeTrackNumber` | 3059 | `bool` | include_track_number,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `AudioFormat` | 3060 | `string` | audio_format,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `RelativePath` | 3061 | `string` | relative_path,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Duration` | 3064 | `int` | duration,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.CheckFileExistenceResult`

Go `main.CheckFileExistenceResult`, defined at `app.go:3067`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 3068 | `string` | spotify_id | `string` |  |
| `Exists` | 3069 | `bool` | exists | `boolean` |  |
| `FilePath` | 3070 | `string` | file_path,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackName` | 3071 | `string` | track_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistName` | 3072 | `string` | artist_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.CommunityBreakStatus`

Go `main.CommunityBreakStatus`, defined at `app.go:77`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Enabled` | 78 | `bool` | enabled | `boolean` |  |
| `IsBreak` | 79 | `bool` | is_break | `boolean` |  |
| `RemainingMinutes` | 80 | `int` | remaining_minutes | `number` |  |
| `Available` | 81 | `bool` | available | `boolean` |  |
| `Error` | 82 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.ConvertAudioRequest`

Go `main.ConvertAudioRequest`, defined at `app.go:2738`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `InputFiles` | 2739 | `[]string` | input_files | `string[]` |  |
| `OutputFormat` | 2740 | `string` | output_format | `string` |  |
| `Bitrate` | 2741 | `string` | bitrate | `string` |  |
| `Codec` | 2742 | `string` | codec | `string` |  |

### `models.main.CoverDownloadRequest`

Go `main.CoverDownloadRequest`, defined at `app.go:2463`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `CoverURL` | 2464 | `string` | cover_url | `string` |  |
| `TrackName` | 2465 | `string` | track_name | `string` |  |
| `ArtistName` | 2466 | `string` | artist_name | `string` |  |
| `Artists` | 2467 | `string` | artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumName` | 2468 | `string` | album_name | `string` |  |
| `AlbumArtist` | 2469 | `string` | album_artist | `string` |  |
| `ReleaseDate` | 2470 | `string` | release_date | `string` |  |
| `OutputDir` | 2471 | `string` | output_dir | `string` |  |
| `FilenameFormat` | 2472 | `string` | filename_format | `string` |  |
| `PlaylistName` | 2473 | `string` | playlist_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Category` | 2474 | `string` | category,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 2475 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ISRC` | 2476 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackNumber` | 2477 | `bool` | track_number | `boolean` |  |
| `Position` | 2478 | `int` | position | `number` |  |
| `DiscNumber` | 2479 | `int` | disc_number | `number` |  |
| `TotalTracks` | 2480 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalDiscs` | 2481 | `int` | total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `UseAlbumTrackNumber` | 2482 | `bool` | use_album_track_number,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.DownloadFFmpegResponse`

Go `main.DownloadFFmpegResponse`, defined at `app.go:2677`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 2678 | `bool` | success | `boolean` |  |
| `Message` | 2679 | `string` | message | `string` |  |
| `Error` | 2680 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.DownloadRequest`

Go `main.DownloadRequest`, defined at `app.go:412`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Service` | 413 | `string` | service | `string` |  |
| `Query` | 414 | `string` | query,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackName` | 415 | `string` | track_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistName` | 416 | `string` | artist_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumName` | 417 | `string` | album_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumArtist` | 418 | `string` | album_artist,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ReleaseDate` | 419 | `string` | release_date,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `CoverURL` | 420 | `string` | cover_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TidalAPIURL` | 421 | `string` | tidal_api_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `QobuzAPIURL` | 422 | `string` | qobuz_api_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `OutputDir` | 423 | `string` | output_dir,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `LibraryRoot` | 424 | `string` | library_root,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AudioFormat` | 425 | `string` | audio_format,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `FilenameFormat` | 426 | `string` | filename_format,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackNumber` | 427 | `bool` | track_number,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Position` | 428 | `int` | position,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `UseAlbumTrackNumber` | 429 | `bool` | use_album_track_number,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyID` | 430 | `string` | spotify_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `EmbedLyrics` | 431 | `bool` | embed_lyrics,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsTranslationMode` | 432 | `string` | lyrics_translation_mode,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsTranslationLang` | 433 | `string` | lyrics_translation_lang,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsAutoFallback` | 434 | `*bool` | lyrics_translation_auto_fallback,omitempty | `boolean` | pointer; omitempty; nil omits the key; Wails marks the property optional |
| `LRCLibTitleFallback` | 435 | `*bool` | lrclib_title_fallback,omitempty | `boolean` | pointer; omitempty; nil omits the key; Wails marks the property optional |
| `EmbedMaxQualityCover` | 436 | `bool` | embed_max_quality_cover,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `ServiceURL` | 437 | `string` | service_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Duration` | 438 | `int` | duration,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `ItemID` | 439 | `string` | item_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyTrackNumber` | 440 | `int` | spotify_track_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyDiscNumber` | 441 | `int` | spotify_disc_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyTotalTracks` | 442 | `int` | spotify_total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyTotalDiscs` | 443 | `int` | spotify_total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `ISRC` | 444 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Copyright` | 445 | `string` | copyright,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Publisher` | 446 | `string` | publisher,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Composer` | 447 | `string` | composer,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `PlaylistName` | 448 | `string` | playlist_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `PlaylistOwner` | 449 | `string` | playlist_owner,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AllowFallback` | 450 | `bool` | allow_fallback | `boolean` |  |
| `AllowAtmosFallback` | 451 | `bool` | allow_atmos_fallback | `boolean` |  |
| `AtmosFallbackQuality` | 452 | `string` | atmos_fallback_quality,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `UseFirstArtistOnly` | 453 | `bool` | use_first_artist_only,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `UseSingleGenre` | 454 | `bool` | use_single_genre,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `EmbedGenre` | 455 | `bool` | embed_genre,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Separator` | 456 | `string` | separator,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `SaveCover` | 457 | `bool` | save_cover,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Artists` | 458 | `string` | artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Category` | 459 | `string` | category,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 460 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoConvertAudio` | 461 | `bool` | auto_convert_audio,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoConvertFormat` | 462 | `string` | auto_convert_format,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoConvertBitrate` | 463 | `string` | auto_convert_bitrate,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoConvertDeleteOriginal` | 464 | `bool` | auto_convert_delete_original,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoResampleAudio` | 465 | `bool` | auto_resample_audio,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoResampleSampleRate` | 466 | `string` | auto_resample_sample_rate,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoResampleBitDepth` | 467 | `string` | auto_resample_bit_depth,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AutoResampleDeleteOriginal` | 468 | `bool` | auto_resample_delete_original,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.DownloadResponse`

Go `main.DownloadResponse`, defined at `app.go:471`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 472 | `bool` | success | `boolean` |  |
| `Message` | 473 | `string` | message | `string` |  |
| `File` | 474 | `string` | file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Error` | 475 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlreadyExists` | 476 | `bool` | already_exists,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Cancelled` | 477 | `bool` | cancelled,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `ItemID` | 478 | `string` | item_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `SourceURL` | 479 | `string` | source_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `SourceLabel` | 480 | `string` | source_label,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `OriginalFile` | 481 | `string` | original_file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ConvertedFile` | 482 | `string` | converted_file,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.GalleryImageDownloadRequest`

Go `main.GalleryImageDownloadRequest`, defined at `app.go:2566`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ImageURL` | 2567 | `string` | image_url | `string` |  |
| `ArtistName` | 2568 | `string` | artist_name | `string` |  |
| `ImageIndex` | 2569 | `int` | image_index | `number` |  |
| `OutputDir` | 2570 | `string` | output_dir | `string` |  |

### `models.main.HeaderDownloadRequest`

Go `main.HeaderDownloadRequest`, defined at `app.go:2527`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `HeaderURL` | 2528 | `string` | header_url | `string` |  |
| `ArtistName` | 2529 | `string` | artist_name | `string` |  |
| `OutputDir` | 2530 | `string` | output_dir | `string` |  |

### `models.main.InstallFFmpegWithBrewResponse`

Go `main.InstallFFmpegWithBrewResponse`, defined at `app.go:2711`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Success` | 2712 | `bool` | success | `boolean` |  |
| `Message` | 2713 | `string` | message | `string` |  |
| `Error` | 2714 | `string` | error,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.LibraryReport`

Go `main.LibraryReport`, defined at `app_library.go:17`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Root` | 18 | `string` | root | `string` |  |
| `ProfileID` | 19 | `string` | profile_id | `string` |  |
| `HealthScore` | 20 | `int` | health_score | `number` |  |
| `TotalTracks` | 21 | `int` | total_tracks | `number` |  |
| `TotalAlbums` | 22 | `int` | total_albums | `number` |  |
| `Issues` | 23 | `[]library.Issue` | issues | `models.library.Issue[]` |  |

### `models.main.LyricsDownloadRequest`

Go `main.LyricsDownloadRequest`, defined at `app.go:2391`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 2392 | `string` | spotify_id | `string` |  |
| `TrackName` | 2393 | `string` | track_name | `string` |  |
| `ArtistName` | 2394 | `string` | artist_name | `string` |  |
| `Artists` | 2395 | `string` | artists,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumName` | 2396 | `string` | album_name | `string` |  |
| `AlbumArtist` | 2397 | `string` | album_artist | `string` |  |
| `ReleaseDate` | 2398 | `string` | release_date | `string` |  |
| `ISRC` | 2399 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `OutputDir` | 2400 | `string` | output_dir | `string` |  |
| `FilenameFormat` | 2401 | `string` | filename_format | `string` |  |
| `PlaylistName` | 2402 | `string` | playlist_name,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Category` | 2403 | `string` | category,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 2404 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `TrackNumber` | 2405 | `bool` | track_number | `boolean` |  |
| `Position` | 2406 | `int` | position | `number` |  |
| `UseAlbumTrackNumber` | 2407 | `bool` | use_album_track_number | `boolean` |  |
| `DiscNumber` | 2408 | `int` | disc_number | `number` |  |
| `TotalTracks` | 2409 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalDiscs` | 2410 | `int` | total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsTranslationMode` | 2411 | `string` | lyrics_translation_mode,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsTranslationLang` | 2412 | `string` | lyrics_translation_lang,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `LyricsAutoFallback` | 2413 | `*bool` | lyrics_translation_auto_fallback,omitempty | `boolean` | pointer; omitempty; nil omits the key; Wails marks the property optional |
| `LRCLibTitleFallback` | 2414 | `*bool` | lrclib_title_fallback,omitempty | `boolean` | pointer; omitempty; nil omits the key; Wails marks the property optional |

### `models.main.ResampleAudioRequest`

Go `main.ResampleAudioRequest`, defined at `app.go:2755`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `InputFiles` | 2756 | `[]string` | input_files | `string[]` |  |
| `SampleRate` | 2757 | `string` | sample_rate | `string` |  |
| `BitDepth` | 2758 | `string` | bit_depth | `string` |  |

### `models.main.SourceVerificationView`

Go `main.SourceVerificationView`, defined at `verification_ui.go:5`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 16 | `uint64` | id | `number` | promoted from backend.VerificationPresentation |
| `Active` | 17 | `bool` | active | `boolean` | promoted from backend.VerificationPresentation |
| `Title` | 18 | `string` | title | `string` | promoted from backend.VerificationPresentation |
| `Host` | 19 | `string` | host | `string` | promoted from backend.VerificationPresentation |
| `Ready` | 20 | `bool` | ready | `boolean` | promoted from backend.VerificationPresentation |
| `Error` | 21 | `string` | error,omitempty | `string` | promoted from backend.VerificationPresentation; omitempty; zero value omits the key; Wails marks the property optional |
| `CanConfirm` | 7 | `bool` | can_confirm | `boolean` |  |

### `models.main.SpotifyMetadataRequest`

Go `main.SpotifyMetadataRequest`, defined at `app.go:403`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `URL` | 404 | `string` | url | `string` |  |
| `Batch` | 405 | `bool` | batch | `boolean` |  |
| `Delay` | 406 | `float64` | delay | `number` |  |
| `Timeout` | 407 | `float64` | timeout | `number` |  |
| `Separator` | 408 | `string` | separator,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ClientRequestID` | 409 | `string` | client_request_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.main.SpotifySearchByTypeRequest`

Go `main.SpotifySearchByTypeRequest`, defined at `app.go:1045`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Query` | 1046 | `string` | query | `string` |  |
| `SearchType` | 1047 | `string` | search_type | `string` |  |
| `Limit` | 1048 | `int` | limit | `number` |  |
| `Offset` | 1049 | `int` | offset | `number` |  |

### `models.main.SpotifySearchRequest`

Go `main.SpotifySearchRequest`, defined at `app.go:1025`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Query` | 1026 | `string` | query | `string` |  |
| `Limit` | 1027 | `int` | limit | `number` |  |

### `models.syncengine.AlbumGroup`

Go `syncengine.AlbumGroup`, defined at `backend/syncengine/planner.go:46`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Album` | 47 | `string` | album | `string` |  |
| `Adds` | 48 | `int` | adds | `number` |  |
| `Updates` | 49 | `int` | updates | `number` |  |
| `Moves` | 50 | `int` | moves | `number` |  |
| `Deletes` | 51 | `int` | deletes | `number` |  |
| `Keeps` | 52 | `int` | keeps | `number` |  |
| `AddBytes` | 53 | `int64` | add_bytes | `number` |  |

### `models.syncengine.FormatPolicy`

Go `syncengine.FormatPolicy`, defined at `backend/syncengine/format.go:6`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Mode` | 8 | `string` | mode | `string` |  |

### `models.syncengine.OpKind`

Go `syncengine.OpKind`, defined at `backend/syncengine/planner.go:15`.

Not a struct. Underlying type: `string`.

Wails uses this name in a method signature (`models.syncengine.OpKind`) and uses the underlying type for a struct field (`string`).

### `models.syncengine.Operation`

Go `syncengine.Operation`, defined at `backend/syncengine/planner.go:26`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Kind` | 27 | `OpKind` | kind | `string` |  |
| `Album` | 29 | `string` | album | `string` |  |
| `Artist` | 30 | `string` | artist | `string` |  |
| `Source` | 32 | `string` | source | `string` |  |
| `Remote` | 34 | `string` | remote | `string` |  |
| `Size` | 36 | `int64` | size | `number` |  |
| `Track` | 40 | `*SourceTrack` | json:"-" | `omitted` | not serialized |
| `TrackSourcePath` | 42 | `string` | track_source_path,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.syncengine.Plan`

Go `syncengine.Plan`, defined at `backend/syncengine/planner.go:57`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Ops` | 58 | `[]Operation` | ops | `models.syncengine.Operation[]` |  |
| `Groups` | 59 | `[]AlbumGroup` | groups | `models.syncengine.AlbumGroup[]` |  |
| `Adds` | 60 | `int` | adds | `number` |  |
| `Updates` | 61 | `int` | updates | `number` |  |
| `Moves` | 62 | `int` | moves | `number` |  |
| `Deletes` | 63 | `int` | deletes | `number` |  |
| `Keeps` | 64 | `int` | keeps | `number` |  |
| `FreeNeeded` | 67 | `int64` | free_needed | `number` |  |
| `FreeAvailable` | 69 | `int64` | free_available | `number` |  |
| `Insufficient` | 70 | `bool` | insufficient | `boolean` |  |
| `ProfileID` | 75 | `string` | profile_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ProfileVersion` | 76 | `int` | profile_version,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `Policy` | 77 | `FormatPolicy` | policy,omitempty | `models.syncengine.FormatPolicy` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.syncengine.Selection`

Go `syncengine.Selection`, defined at `backend/syncengine/selection.go:60`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `WholeLibrary` | 61 | `bool` | whole_library | `boolean` |  |
| `Artists` | 63 | `[]string` | artists | `string[]` |  |
| `Albums` | 65 | `[]string` | albums | `string[]` |  |
| `Playlists` | 67 | `[]string` | playlists | `string[]` |  |
| `RecentDays` | 69 | `int` | recent_days | `number` |  |

### `models.syncengine.SourceTrack`

Go `syncengine.SourceTrack`, defined at `backend/syncengine/selection.go:9`.

Custom `MarshalJSON` at `backend/syncengine/persistence.go:12`. It marshals an inner wire struct at `backend/syncengine/persistence.go:13`, so the JSON keys are the wire tags, not the outer struct tags.

Custom `UnmarshalJSON` at `backend/syncengine/persistence.go:35`.

`syncengine.Operation.Track` is `*SourceTrack` with `json:"-"` (`backend/syncengine/planner.go:40`). `encoding/json` omits that field and does not call `MarshalJSON`, so a `Plan` object has no track. The wire shape applies when a `SourceTrack` value is marshalled on its own. `PlanSnapshot.Tracks` (`backend/syncengine/persistence.go:76`) is persistence, not an `App` method type.

Wire JSON from `MarshalJSON`:

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 14 | `string` | path | `string` |  |
| `Size` | 15 | `int64` | size | `number` |  |
| `ModTime` | 16 | `time.Time` | mod_time | `any` | serde string RFC3339; Wails models.ts types time.Time as any |
| `Hash` | 17 | `string` | hash | `string` |  |
| `Title` | 18 | `string` | title | `string` |  |
| `Artist` | 19 | `string` | artist | `string` |  |
| `AlbumArtist` | 20 | `string` | album_artist | `string` |  |
| `Album` | 21 | `string` | album | `string` |  |
| `Year` | 22 | `string` | year | `string` |  |
| `TrackNumber` | 23 | `int` | track_number | `number` |  |
| `DiscNumber` | 24 | `int` | disc_number | `number` |  |
| `DiscTotal` | 25 | `int` | disc_total | `number` |  |
| `Ext` | 26 | `string` | ext | `string` |  |
| `DurationSec` | 27 | `int` | duration_sec | `number` |  |
| `AddedAt` | 28 | `time.Time` | added_at | `any` | serde string RFC3339; Wails models.ts types time.Time as any |
| `Lossless` | 29 | `bool` | lossless,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Codec` | 30 | `string` | codec,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

Go struct fields (ignored on the wire while `MarshalJSON` is in effect):

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Path` | 10 | `string` | Path (untagged) | `string` |  |
| `Size` | 11 | `int64` | Size (untagged) | `number` |  |
| `ModTime` | 12 | `time.Time` | ModTime (untagged) | `any` | serde string RFC3339; Wails models.ts types time.Time as any |
| `Hash` | 13 | `string` | Hash (untagged) | `string` |  |
| `Title` | 14 | `string` | Title (untagged) | `string` |  |
| `Artist` | 15 | `string` | Artist (untagged) | `string` |  |
| `AlbumArtist` | 16 | `string` | AlbumArtist (untagged) | `string` |  |
| `Album` | 17 | `string` | Album (untagged) | `string` |  |
| `Year` | 18 | `string` | Year (untagged) | `string` |  |
| `TrackNumber` | 19 | `int` | TrackNumber (untagged) | `number` |  |
| `DiscNumber` | 20 | `int` | DiscNumber (untagged) | `number` |  |
| `DiscTotal` | 21 | `int` | DiscTotal (untagged) | `number` |  |
| `Ext` | 22 | `string` | Ext (untagged) | `string` |  |
| `DurationSec` | 23 | `int` | DurationSec (untagged) | `number` |  |
| `AddedAt` | 24 | `time.Time` | AddedAt (untagged) | `any` | serde string RFC3339; Wails models.ts types time.Time as any |
| `Lossless` | 31 | `bool` | Lossless (untagged) | `boolean` |  |
| `Codec` | 34 | `string` | Codec (untagged) | `string` |  |

### `models.taste.Item`

Go `taste.Item`, defined at `backend/taste/shelves.go:22`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 23 | `string` | id | `string` |  |
| `Kind` | 24 | `string` | kind | `string` |  |
| `Title` | 25 | `string` | title | `string` |  |
| `Artist` | 26 | `string` | artist | `string` |  |
| `Album` | 27 | `string` | album,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Image` | 28 | `string` | image,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `SpotifyID` | 29 | `string` | spotify_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumID` | 30 | `string` | album_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistID` | 31 | `string` | artist_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ISRC` | 32 | `string` | isrc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Reason` | 35 | `string` | reason | `string` |  |
| `ReasonCode` | 36 | `string` | reason_code | `string` |  |
| `ReasonArg` | 37 | `string` | reason_arg,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Score` | 38 | `float64` | score | `number` |  |

### `models.taste.Settings`

Go `taste.Settings`, defined at `backend/taste/service.go:24`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ForYouEnabled` | 25 | `bool` | for_you_enabled | `boolean` |  |
| `SpotifyClientID` | 26 | `string` | spotify_client_id | `string` |  |
| `SpotifyRedirectURI` | 28 | `string` | spotify_redirect_uri | `string` |  |
| `SpotifyConnected` | 29 | `bool` | spotify_connected | `boolean` |  |
| `LastFMUsername` | 30 | `string` | lastfm_username | `string` |  |
| `LastFMConfigured` | 31 | `bool` | lastfm_configured | `boolean` |  |
| `LastSync` | 32 | `string` | last_sync | `string` |  |
| `EventCount` | 33 | `int` | event_count | `number` |  |

### `models.taste.Shelf`

Go `taste.Shelf`, defined at `backend/taste/shelves.go:54`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 55 | `string` | id | `string` |  |
| `Title` | 56 | `string` | title | `string` |  |
| `Seed` | 57 | `string` | seed,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Items` | 58 | `[]Item` | items | `models.taste.Item[]` |  |

### `models.taste.Summary`

Go `taste.Summary`, defined at `backend/taste/profile.go:157`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `TopArtists` | 158 | `[]string` | top_artists | `string[]` |  |
| `TopGenres` | 159 | `[]string` | top_genres | `string[]` |  |
| `EventCount` | 160 | `int` | event_count | `number` |  |
| `LastSync` | 161 | `string` | last_sync | `string` |  |
| `PinnedArtists` | 163 | `[]string` | pinned_artists | `string[]` |  |

## 4. Runtime events

Wails `runtime.EventsEmit` deliveries. Payload JSON is whatever `encoding/json` produces for the Go value. Listeners are `EventsOn` / `EventsOff` / `EventsOnce` string literals under `frontend/src`.

### Every `EventsEmit` text hit

This list is every line in a `.go` file that contains `EventsEmit`, including comments, so it covers `rg -n EventsEmit -g "*.go"`.

- `app.go:345: runtime.EventsEmit(ctx, "source-verification", sourceVerificationView(p))`
- `app.go:999: runtime.EventsEmit(a.ctx, "metadata-stream-begin", streamID)`
- `app.go:1001: runtime.EventsEmit(a.ctx, "metadata-stream-begin", map[string]interface{}{`
- `app.go:1007: runtime.EventsEmit(a.ctx, "metadata-stream", map[string]interface{}{`
- `app.go:2684: runtime.EventsEmit(a.ctx, "ffmpeg:status", "starting")`
- `app.go:2686: runtime.EventsEmit(a.ctx, "ffmpeg:progress", progress)`
- `app.go:2689: runtime.EventsEmit(a.ctx, "ffmpeg:status", "failed")`
- `app.go:2696: runtime.EventsEmit(a.ctx, "ffmpeg:status", "completed")`
- `app.go:2718: runtime.EventsEmit(a.ctx, "ffmpeg:status", "Installing FFmpeg via Homebrew...")`
- `app.go:2720: runtime.EventsEmit(a.ctx, "ffmpeg:progress", progress)`
- `app.go:2721: runtime.EventsEmit(a.ctx, "ffmpeg:status", status)`
- `app.go:2724: runtime.EventsEmit(a.ctx, "ffmpeg:status", "failed")`
- `app.go:2731: runtime.EventsEmit(a.ctx, "ffmpeg:status", "completed")`
- `app_ipod.go:43: runtime.EventsEmit(a.ctx, "ipod:devices", list)`
- `app_ipod.go:108: runtime.EventsEmit(a.ctx, "ipod:progress", progress)`
- `app_library.go:196: runtime.EventsEmit(a.ctx, name, payload)`
- `app_library_test.go:130: app := &App{} // nil ctx in unit tests so Wails EventsEmit is bypassed`
- `app_sync.go:37: runtime.EventsEmit(a.ctx, "devices:changed", views)`
- `app_sync.go:438: runtime.EventsEmit(a.ctx, "sync:progress", p)`
- `app_sync.go:493: runtime.EventsEmit(a.ctx, "platform-tools:progress", map[string]interface{}{`
- `app_taste.go:214: runtime.EventsEmit(a.ctx, "taste:import-progress", p)`
- `app_taste.go:231: runtime.EventsEmit(a.ctx, "taste:sync-progress", p)`
- `close_lifecycle.go:21: runtime.EventsEmit(ctx, "app:before-close")`

### Events

| event | emit | payload | listeners |
| --- | --- | --- | --- |
| `source-verification` | `app.go:345` | `main.SourceVerificationView` (see §3). Flattened JSON of `backend.VerificationPresentation` plus `can_confirm`. | `frontend/src/components/SourceVerificationPane.tsx:25` `EventsOn` |
| `metadata-stream-begin` | `app.go:999`, `app.go:1001` | Two shapes. Empty `SpotifyMetadataRequest.client_request_id`: a JSON number, the `uint64` stream id (`app.go:997-999`). Otherwise an object `{"id": number, "request_id": string}` (`app.go:1001-1004`). `id` is the same `uint64` and is not exact in JS above 2^53−1. | `frontend/src/hooks/useMetadata.ts:140` `EventsOn` |
| `metadata-stream` | `app.go:1007` | Object `{"id": number, "request_id": string, "payload": <value>}` (`app.go:1007-1011`). `payload` is the value passed to `MetadataCallback` in `backend/spotify_metadata.go`. Track URLs do not stream (`fetchTrack` has no callback, `spotify_metadata.go:394-395`). Album, playlist, and artist runs do. See the payload types below. | `frontend/src/hooks/useMetadata.ts:187` `EventsOn` |
| `ffmpeg:status` | `app.go:2684`, `app.go:2689`, `app.go:2696`, `app.go:2718`, `app.go:2721`, `app.go:2724`, `app.go:2731` | JSON string. `DownloadFFmpeg` emits `starting`, then `failed` or `completed` (`app.go:2684-2696`). `InstallFFmpegWithBrew` emits `Installing FFmpeg via Homebrew...`, then each brew status string, then `failed` or `completed` (`app.go:2718-2731`). | `frontend/src/App.tsx:473` `EventsOn` |
| `ffmpeg:progress` | `app.go:2686`, `app.go:2720` | JSON number. `DownloadFFmpeg` passes `progress int` (`app.go:2685-2686`). `InstallFFmpegWithBrew` passes `progress int` (`app.go:2719-2720`). | `frontend/src/App.tsx:464` `EventsOn` |
| `ipod:devices` | `app_ipod.go:43` | `[]ipod.Device` (see §3 `models.ipod.Device`). Emitted when the device list signature changes (`app_ipod.go:33-43`). | `frontend/src/App.tsx:288` `EventsOn` |
| `ipod:progress` | `app_ipod.go:108` | `ipod.Progress` (see §3). Emitted from the `SendToIPod` copy callback (`app_ipod.go:106-109`). | `frontend/src/components/DevicesPage.tsx:145` `EventsOn` |
| `devices:changed` | `app_sync.go:37` | `[]devices.SyncTargetView` (see §3). Emitted by the sync watch (`app_sync.go:35-38`). | `frontend/src/components/AndroidSyncPanel.tsx:165` `EventsOn` |
| `sync:progress` | `app_sync.go:438` | `syncengine.Progress` (see §3), via `emitSyncProgress` (`app_sync.go:436-438`). | `frontend/src/components/AndroidSyncPanel.tsx:167` `EventsOn` |
| `platform-tools:progress` | `app_sync.go:493` | Object `{"percent": number, "status": string}` (`app_sync.go:493-496`). `percent` is a Go `int`. | `frontend/src/components/AndroidSyncPanel.tsx:187` `EventsOn` |
| `taste:import-progress` | `app_taste.go:214` | `taste.ImportProgress` (see §3), from `ImportSpotifyExport` (`app_taste.go:212-215`). | `frontend/src/components/ListeningConnectionsSettings.tsx:112` `EventsOn` |
| `taste:sync-progress` | `app_taste.go:231` | `taste.SyncProgress` (see §3), from `SyncTasteNow` (`app_taste.go:229-232`). | `frontend/src/components/ForYouPage.tsx:117` `EventsOn`, `frontend/src/components/ListeningConnectionsSettings.tsx:96` `EventsOn`, `frontend/src/hooks/useTasteSync.ts:26` `EventsOn` |
| `app:before-close` | `close_lifecycle.go:21` | No payload. Emitted only when `beforeClose` returns `flushBeforeClose` (`close_lifecycle.go:20-22`). The frontend answers with `FinishClose`. | `frontend/src/main.tsx:20` `EventsOn` |
| `library:scan-progress` | `app_library.go:73` through `emitLibraryEvent` at `app_library.go:196` | `library.ScanProgress`. Not a method argument, so its fields are below. | `frontend/src/components/LibraryHealthPage.tsx:141` `EventsOn` |
| `library:scan-complete` | `app_library.go:81` through `app_library.go:196` | `*main.LibraryReport` (see §3). Nil is not used on this path; the report is the scan result. | `frontend/src/components/LibraryHealthPage.tsx:142` `EventsOn` |
| `library:scan-error` | `app_library.go:79` through `app_library.go:196` | JSON string, `err.Error()` (`app_library.go:79`). | `frontend/src/components/LibraryHealthPage.tsx:143` `EventsOn` |
| `library:scan-cancelled` | `app_library.go:77` through `app_library.go:196` | No payload (`nil`, `app_library.go:77`). | `frontend/src/components/LibraryHealthPage.tsx:144` `EventsOn` |

`app_library_test.go:130` contains the word EventsEmit in a comment ("nil ctx in unit tests so Wails EventsEmit is bypassed"). It is not an emission.

`emitLibraryEvent` (`app_library.go:194-197`) is the only dynamic `EventsEmit`. Its names are the four `library:scan-*` events above (`app_library.go:73`, `app_library.go:77`, `app_library.go:79`, `app_library.go:81`). The comment at `app_library.go:42-44` names the same four.

### `metadata-stream` payload values

The callback argument is JSON-marshalled as `payload`. Concrete values:

- `*backend.AlbumResponsePayload` from the early album callback (`spotify_metadata.go:681-682`) and `AlbumResponsePayload` with an empty track list (`spotify_metadata.go:1161-1164`).
- `[]backend.AlbumTrackMetadata` after the album is formatted (`spotify_metadata.go:1217`), after each playlist page (`spotify_metadata.go:819`), after the playlist is formatted (`spotify_metadata.go:1289`), and after each artist album (`spotify_metadata.go:1426`).
- `backend.PlaylistResponsePayload` with an empty track list (`spotify_metadata.go:795-796` and `spotify_metadata.go:1237-1240`).
- `*backend.ArtistDiscographyPayload` (`spotify_metadata.go:899-900` and `spotify_metadata.go:1349-1353`).

These types are not `App` method parameters or results (`GetSpotifyMetadata` returns the final document as a JSON string). Their serde shape:

### `models.backend.AlbumResponsePayload`

Go `backend.AlbumResponsePayload`, defined at `backend/spotify_metadata.go:113`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `AlbumInfo` | 114 | `AlbumInfoMetadata` | album_info | `models.backend.AlbumInfoMetadata` |  |
| `TrackList` | 115 | `[]AlbumTrackMetadata` | track_list | `models.backend.AlbumTrackMetadata[]` |  |

### `models.backend.AlbumInfoMetadata`

Go `backend.AlbumInfoMetadata`, defined at `backend/spotify_metadata.go:100`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `TotalTracks` | 101 | `int` | total_tracks | `number` |  |
| `Name` | 102 | `string` | name | `string` |  |
| `ReleaseDate` | 103 | `string` | release_date | `string` |  |
| `Artists` | 104 | `string` | artists | `string` |  |
| `Images` | 105 | `string` | images | `string` |  |
| `IsExplicit` | 106 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 107 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Batch` | 108 | `string` | batch,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistID` | 109 | `string` | artist_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistURL` | 110 | `string` | artist_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.AlbumTrackMetadata`

Go `backend.AlbumTrackMetadata`, defined at `backend/spotify_metadata.go:69`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `SpotifyID` | 70 | `string` | spotify_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Artists` | 71 | `string` | artists | `string` |  |
| `Name` | 72 | `string` | name | `string` |  |
| `AlbumName` | 73 | `string` | album_name | `string` |  |
| `AlbumArtist` | 74 | `string` | album_artist,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `DurationMS` | 75 | `int` | duration_ms | `number` |  |
| `Images` | 76 | `string` | images | `string` |  |
| `ReleaseDate` | 77 | `string` | release_date | `string` |  |
| `TrackNumber` | 78 | `int` | track_number | `number` |  |
| `TotalTracks` | 79 | `int` | total_tracks,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `DiscNumber` | 80 | `int` | disc_number,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `TotalDiscs` | 81 | `int` | total_discs,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `ExternalURL` | 82 | `string` | external_urls | `string` |  |
| `AlbumType` | 83 | `string` | album_type,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumID` | 84 | `string` | album_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `AlbumURL` | 85 | `string` | album_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistID` | 86 | `string` | artist_id,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistURL` | 87 | `string` | artist_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `ArtistsData` | 88 | `[]ArtistSimple` | artists_data,omitempty | `models.backend.ArtistSimple[]` | omitempty; zero value omits the key; Wails marks the property optional |
| `UPC` | 89 | `string` | upc,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Plays` | 90 | `string` | plays,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Status` | 91 | `string` | status,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `PreviewURL` | 92 | `string` | preview_url,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `IsExplicit` | 93 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ArtistSimple`

Go `backend.ArtistSimple`, defined at `backend/spotify_metadata.go:63`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 64 | `string` | id | `string` |  |
| `Name` | 65 | `string` | name | `string` |  |
| `ExternalURL` | 66 | `string` | external_urls | `string` |  |

### `models.backend.PlaylistResponsePayload`

Go `backend.PlaylistResponsePayload`, defined at `backend/spotify_metadata.go:135`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `PlaylistInfo` | 136 | `PlaylistInfoMetadata` | playlist_info | `models.backend.PlaylistInfoMetadata` |  |
| `TrackList` | 137 | `[]AlbumTrackMetadata` | track_list | `models.backend.AlbumTrackMetadata[]` |  |

### `models.backend.PlaylistInfoMetadata`

Go `backend.PlaylistInfoMetadata`, defined at `backend/spotify_metadata.go:118`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Tracks` | 119 | `struct { 	Total int `json:"total"` }` | tracks | `any` | anonymous struct; inner JSON keys are in the Go type column; Wails models.ts types the field as any |
| `Followers` | 122 | `struct { 	Total int `json:"total"` }` | followers | `any` | anonymous struct; inner JSON keys are in the Go type column; Wails models.ts types the field as any |
| `Owner` | 125 | `struct { 	DisplayName	string	`json:"display_name"` 	Name		string	`json:"name"` 	Images		string	`json:"images"` }` | owner | `any` | anonymous struct; inner JSON keys are in the Go type column; Wails models.ts types the field as any |
| `Cover` | 130 | `string` | cover,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Description` | 131 | `string` | description,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Batch` | 132 | `string` | batch,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.ArtistDiscographyPayload`

Go `backend.ArtistDiscographyPayload`, defined at `backend/spotify_metadata.go:169`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ArtistInfo` | 170 | `ArtistInfoMetadata` | artist_info | `models.backend.ArtistInfoMetadata` |  |
| `AlbumList` | 171 | `[]DiscographyAlbumMetadata` | album_list | `models.backend.DiscographyAlbumMetadata[]` |  |
| `TrackList` | 172 | `[]AlbumTrackMetadata` | track_list | `models.backend.AlbumTrackMetadata[]` |  |

### `models.backend.ArtistInfoMetadata`

Go `backend.ArtistInfoMetadata`, defined at `backend/spotify_metadata.go:140`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Name` | 141 | `string` | name | `string` |  |
| `Followers` | 142 | `int` | followers | `number` |  |
| `Genres` | 143 | `[]string` | genres | `string[]` |  |
| `Images` | 144 | `string` | images | `string` |  |
| `Header` | 145 | `string` | header,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Gallery` | 146 | `[]string` | gallery,omitempty | `string[]` | omitempty; zero value omits the key; Wails marks the property optional |
| `ExternalURL` | 147 | `string` | external_urls | `string` |  |
| `DiscographyType` | 148 | `string` | discography_type | `string` |  |
| `TotalAlbums` | 149 | `int` | total_albums | `number` |  |
| `Biography` | 150 | `string` | biography,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |
| `Verified` | 151 | `bool` | verified,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |
| `Listeners` | 152 | `int` | listeners,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `Rank` | 153 | `int` | rank,omitempty | `number` | omitempty; zero value omits the key; Wails marks the property optional |
| `Batch` | 154 | `string` | batch,omitempty | `string` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.backend.DiscographyAlbumMetadata`

Go `backend.DiscographyAlbumMetadata`, defined at `backend/spotify_metadata.go:157`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `ID` | 158 | `string` | id | `string` |  |
| `Name` | 159 | `string` | name | `string` |  |
| `AlbumType` | 160 | `string` | album_type | `string` |  |
| `ReleaseDate` | 161 | `string` | release_date | `string` |  |
| `TotalTracks` | 162 | `int` | total_tracks | `number` |  |
| `Artists` | 163 | `string` | artists | `string` |  |
| `Images` | 164 | `string` | images | `string` |  |
| `ExternalURL` | 165 | `string` | external_urls | `string` |  |
| `IsExplicit` | 166 | `bool` | is_explicit,omitempty | `boolean` | omitempty; zero value omits the key; Wails marks the property optional |

### `models.library.ScanProgress`

Go `library.ScanProgress`, defined at `backend/library/scan.go:63`.

| field | line | Go type | JSON | TS | notes |
| --- | ---: | --- | --- | --- | --- |
| `Phase` | 64 | `string` | phase | `string` |  |
| `Current` | 65 | `int` | current | `number` |  |
| `Total` | 66 | `int` | total | `number` |  |
| `Path` | 67 | `string` | path | `string` |  |

### Listeners

| file:line | API | event |
| --- | --- | --- |
| `frontend/src/App.tsx:288` | `EventsOn` | `ipod:devices` |
| `frontend/src/App.tsx:310` | `EventsOff` | `ipod:devices` |
| `frontend/src/App.tsx:464` | `EventsOn` | `ffmpeg:progress` |
| `frontend/src/App.tsx:473` | `EventsOn` | `ffmpeg:status` |
| `frontend/src/App.tsx:491` | `EventsOff` | `ffmpeg:progress` |
| `frontend/src/App.tsx:492` | `EventsOff` | `ffmpeg:status` |
| `frontend/src/components/AndroidSyncPanel.tsx:165` | `EventsOn` | `devices:changed` |
| `frontend/src/components/AndroidSyncPanel.tsx:167` | `EventsOn` | `sync:progress` |
| `frontend/src/components/AndroidSyncPanel.tsx:187` | `EventsOn` | `platform-tools:progress` |
| `frontend/src/components/DevicesPage.tsx:145` | `EventsOn` | `ipod:progress` |
| `frontend/src/components/DevicesPage.tsx:152` | `EventsOff` | `ipod:progress` |
| `frontend/src/components/ForYouPage.tsx:117` | `EventsOn` | `taste:sync-progress` |
| `frontend/src/components/LibraryHealthPage.tsx:141` | `EventsOn` | `library:scan-progress` |
| `frontend/src/components/LibraryHealthPage.tsx:142` | `EventsOn` | `library:scan-complete` |
| `frontend/src/components/LibraryHealthPage.tsx:143` | `EventsOn` | `library:scan-error` |
| `frontend/src/components/LibraryHealthPage.tsx:144` | `EventsOn` | `library:scan-cancelled` |
| `frontend/src/components/LibraryHealthPage.tsx:147` | `EventsOff` | `library:scan-progress` |
| `frontend/src/components/LibraryHealthPage.tsx:148` | `EventsOff` | `library:scan-complete` |
| `frontend/src/components/LibraryHealthPage.tsx:149` | `EventsOff` | `library:scan-error` |
| `frontend/src/components/LibraryHealthPage.tsx:150` | `EventsOff` | `library:scan-cancelled` |
| `frontend/src/components/ListeningConnectionsSettings.tsx:96` | `EventsOn` | `taste:sync-progress` |
| `frontend/src/components/ListeningConnectionsSettings.tsx:112` | `EventsOn` | `taste:import-progress` |
| `frontend/src/components/SourceVerificationPane.tsx:25` | `EventsOn` | `source-verification` |
| `frontend/src/hooks/useMetadata.ts:140` | `EventsOn` | `metadata-stream-begin` |
| `frontend/src/hooks/useMetadata.ts:187` | `EventsOn` | `metadata-stream` |
| `frontend/src/hooks/useMetadata.ts:189` | `EventsOff` | `metadata-stream` |
| `frontend/src/hooks/useMetadata.ts:190` | `EventsOff` | `metadata-stream-begin` |
| `frontend/src/hooks/useTasteSync.ts:26` | `EventsOn` | `taste:sync-progress` |
| `frontend/src/main.tsx:20` | `EventsOn` | `app:before-close` |

Emitted but never listened to:

None. Every emitted name has an `EventsOn` listener.

Listened to but never emitted:

None.

`EventsOnce` is not used. `frontend/src/preview.tsx:38` stubs `EventsOnMultiple` on the dev harness proxy and is not a production listener. The harness also `emit`s `taste:import-progress` and `taste:sync-progress` locally (`preview.tsx:67`, `preview.tsx:69`, `preview.tsx:72`, `preview.tsx:76`).

## 5. Other Wails runtime calls

Tauri needs a plugin or API for each of these. `EventsEmit` sites are in §4 and are not repeated here. Imports are `github.com/wailsapp/wails/v2/pkg/runtime` unless noted. `backend/folder.go` aliases that import as `wailsRuntime` because the file also imports Go's `runtime`.

| file:line | call | arguments |
| --- | --- | --- |
| `app.go:353` | `BrowserOpenURL` |  |
| `app.go:355` | `WindowShow` |  |
| `app.go:356` | `WindowUnminimise` |  |
| `app.go:1617` | `SaveFileDialog` | DefaultFilename=fmt.Sprintf("Auralis_Settings_%s.json", time.Now().Format("20060102_150405")); Title="Backup Settings"; Filters |
| `app.go:1639` | `OpenFileDialog` | Title="Restore Settings"; Filters |
| `app.go:1768` | `SaveFileDialog` | DefaultFilename=defaultFilename; Title="Export Failed Downloads"; Filters |
| `app_sync.go:508` | `OpenFileDialog` | Title=title; Filters |
| `app_taste.go:41` | `BrowserOpenURL` |  |
| `app_taste.go:193` | `OpenFileDialog` | Title="Select Spotify Export"; Filters |
| `backend/file_dialog.go:10` | `OpenMultipleFilesDialog` | Title="Select Audio Files"; Filters |
| `backend/file_dialog.go:54` | `OpenDirectoryDialog` | Title="Select Output Directory" |
| `backend/folder.go:39` | `OpenDirectoryDialog` |  |
| `backend/folder.go:82` | `OpenFileDialog` |  |
| `backend/folder.go:109` | `OpenMultipleFilesDialog` |  |
| `backend/lyrics_reader.go:256` | `OpenMultipleFilesDialog` | Title="Select Lyrics or Audio Files"; Filters |
| `backend/lyrics_reader.go:280` | `OpenDirectoryDialog` | Title="Select Folder with Lyrics or Audio Files" |
| `close_lifecycle.go:13` | `Quit` |  |

What the dialogs are for:

- `app.go:1617` `SaveFileDialog` backs `BackupSettings`: JSON file, default name `Auralis_Settings_<timestamp>.json`, title "Backup Settings".
- `app.go:1639` `OpenFileDialog` backs `RestoreSettings`: JSON file, title "Restore Settings".
- `app.go:1768` `SaveFileDialog` backs `ExportFailedDownloads`: `.txt` file, title "Export Failed Downloads".
- `app_sync.go:508` `OpenFileDialog` backs `SelectSyncPlaylist`: title and filter label are arguments, pattern `*.m3u8`.
- `app_taste.go:193` `OpenFileDialog` backs `SelectSpotifyExportFile`: title "Select Spotify Export", patterns `*.zip;*.json` and `*.*`.
- `app.go:353` `BrowserOpenURL` opens a community verification URL. `app.go:355` `WindowShow` and `app.go:356` `WindowUnminimise` bring the window forward when verification finishes.
- `close_lifecycle.go:13` `Quit` runs from `FinishClose` after the close gate accepts the save (`close_gate.go:47-58` returns true only when `saved` is true).
- `backend/folder.go:39` `OpenDirectoryDialog` backs `SelectFolder` (title "Select Download Folder"). `backend/folder.go:82` `OpenFileDialog` backs `SelectFile` (audio filters, title "Select Audio File for Analysis"). `backend/folder.go:109` `OpenMultipleFilesDialog` backs `SelectImageVideo`.
- `backend/file_dialog.go:10` `OpenMultipleFilesDialog` backs `SelectAudioFiles` (title "Select Audio Files"). `backend/file_dialog.go:54` `OpenDirectoryDialog` backs the output-directory picker (title "Select Output Directory").
- `backend/lyrics_reader.go:256` `OpenMultipleFilesDialog` backs `SelectLyricsFiles`. `backend/lyrics_reader.go:280` `OpenDirectoryDialog` backs `SelectLyricsFolder`.

No Go call sites were found for `WindowSetTitle`, `MessageDialog`, `WindowHide`, `WindowMinimise`, `WindowMaximise`, `ClipboardGetText`, `ClipboardSetText`, `LogPrint`, or `EventsOn`.

### Frontend imports from `wailsjs/runtime/runtime`

| symbol | import | calls |
| --- | --- | --- |
| `EventsOn` | `frontend/src/App.tsx:12` | frontend/src/App.tsx:288, frontend/src/App.tsx:464, frontend/src/App.tsx:473 |
| `EventsOff` | `frontend/src/App.tsx:12` | frontend/src/App.tsx:310, frontend/src/App.tsx:491, frontend/src/App.tsx:492 |
| `Quit` | `frontend/src/App.tsx:12` | frontend/src/App.tsx:1095 |
| `EventsOn` | `frontend/src/components/AndroidSyncPanel.tsx:27` | frontend/src/components/AndroidSyncPanel.tsx:165, frontend/src/components/AndroidSyncPanel.tsx:167, frontend/src/components/AndroidSyncPanel.tsx:187 |
| `OnFileDrop` | `frontend/src/components/AudioAnalysisPage.tsx:17` | frontend/src/components/AudioAnalysisPage.tsx:426 |
| `OnFileDropOff` | `frontend/src/components/AudioAnalysisPage.tsx:17` | frontend/src/components/AudioAnalysisPage.tsx:434 |
| `OnFileDrop` | `frontend/src/components/AudioConverterPage.tsx:10` | frontend/src/components/AudioConverterPage.tsx:206 |
| `OnFileDropOff` | `frontend/src/components/AudioConverterPage.tsx:10` | frontend/src/components/AudioConverterPage.tsx:210 |
| `OnFileDrop` | `frontend/src/components/AudioResamplerPage.tsx:10` | frontend/src/components/AudioResamplerPage.tsx:228 |
| `OnFileDropOff` | `frontend/src/components/AudioResamplerPage.tsx:10` | frontend/src/components/AudioResamplerPage.tsx:232 |
| `EventsOff` | `frontend/src/components/DevicesPage.tsx:11` | frontend/src/components/DevicesPage.tsx:152 |
| `EventsOn` | `frontend/src/components/DevicesPage.tsx:11` | frontend/src/components/DevicesPage.tsx:145 |
| `EventsOn` | `frontend/src/components/ForYouPage.tsx:34` | frontend/src/components/ForYouPage.tsx:117 |
| `EventsOff` | `frontend/src/components/LibraryHealthPage.tsx:38` | frontend/src/components/LibraryHealthPage.tsx:147, frontend/src/components/LibraryHealthPage.tsx:148, frontend/src/components/LibraryHealthPage.tsx:149, frontend/src/components/LibraryHealthPage.tsx:150 |
| `EventsOn` | `frontend/src/components/LibraryHealthPage.tsx:38` | frontend/src/components/LibraryHealthPage.tsx:141, frontend/src/components/LibraryHealthPage.tsx:142, frontend/src/components/LibraryHealthPage.tsx:143, frontend/src/components/LibraryHealthPage.tsx:144 |
| `EventsOn` | `frontend/src/components/ListeningConnectionsSettings.tsx:35` | frontend/src/components/ListeningConnectionsSettings.tsx:96, frontend/src/components/ListeningConnectionsSettings.tsx:112 |
| `OnFileDrop` | `frontend/src/components/LyricsManagerPage.tsx:10` | frontend/src/components/LyricsManagerPage.tsx:201 |
| `OnFileDropOff` | `frontend/src/components/LyricsManagerPage.tsx:10` | frontend/src/components/LyricsManagerPage.tsx:205 |
| `OnFileDrop` | `frontend/src/components/ReplayGainPage.tsx:17` | frontend/src/components/ReplayGainPage.tsx:289 |
| `OnFileDropOff` | `frontend/src/components/ReplayGainPage.tsx:17` | frontend/src/components/ReplayGainPage.tsx:293 |
| `EventsOn` | `frontend/src/components/SourceVerificationPane.tsx:5` | frontend/src/components/SourceVerificationPane.tsx:25 |
| `OnFileDrop` | `frontend/src/components/TempoKeyAnalyzerPage.tsx:14` | frontend/src/components/TempoKeyAnalyzerPage.tsx:302 |
| `OnFileDropOff` | `frontend/src/components/TempoKeyAnalyzerPage.tsx:14` | frontend/src/components/TempoKeyAnalyzerPage.tsx:306 |
| `WindowMinimise` | `frontend/src/components/TitleBar.tsx:20` | frontend/src/components/TitleBar.tsx:165 |
| `WindowToggleMaximise` | `frontend/src/components/TitleBar.tsx:20` | frontend/src/components/TitleBar.tsx:87, frontend/src/components/TitleBar.tsx:168 |
| `Quit` | `frontend/src/components/TitleBar.tsx:20` | frontend/src/components/TitleBar.tsx:171 |
| `ClipboardGetText` | `frontend/src/components/ui/input-with-context.tsx:6` | frontend/src/components/ui/input-with-context.tsx:25, frontend/src/components/ui/input-with-context.tsx:86 |
| `ClipboardSetText` | `frontend/src/components/ui/input-with-context.tsx:6` | frontend/src/components/ui/input-with-context.tsx:41, frontend/src/components/ui/input-with-context.tsx:72 |
| `EventsOff` | `frontend/src/hooks/useMetadata.ts:8` | frontend/src/hooks/useMetadata.ts:189, frontend/src/hooks/useMetadata.ts:190 |
| `EventsOn` | `frontend/src/hooks/useMetadata.ts:8` | frontend/src/hooks/useMetadata.ts:140, frontend/src/hooks/useMetadata.ts:187 |
| `EventsOn` | `frontend/src/hooks/useTasteSync.ts:3` | frontend/src/hooks/useTasteSync.ts:26 |
| `BrowserOpenURL` | `frontend/src/lib/utils.ts:3` | frontend/src/lib/utils.ts:36 |
| `EventsOn` | `frontend/src/main.tsx:9` | frontend/src/main.tsx:20 |

File-drop CSS: `main.go:60-64` sets `DragAndDrop.CSSDropProperty` to `--wails-drop-target` and `CSSDropValue` to `drop`. The pages that call `OnFileDrop` also set that variable on the drop element (`AudioAnalysisPage.tsx:747`, `AudioConverterPage.tsx:322`, `AudioResamplerPage.tsx:342`, `LyricsManagerPage.tsx:418`, `ReplayGainPage.tsx:466`, `TempoKeyAnalyzerPage.tsx:494`).

`preview.tsx:37-44` stubs `window.runtime` in the dev harness: `EventsOnMultiple` records a listener, `ClipboardGetText` resolves to `""`, and every other runtime function is a no-op. It does not import `wailsjs/runtime/runtime`.

## 6. Lifecycle hooks (`main.go`)

`main` (`main.go:22`) reads `wails.json` (`main.go:19-31`) and, when `info.productVersion` is set, assigns `backend.AppVersion`. It builds `*App` with `NewApp()` (`main.go:34`) and a WebView2 user-data directory from `backend.IsolatedWebviewProfilePath()` (`main.go:35-38`). `wails.Run` is `main.go:40-76`. There is no `OnDomReady` hook.

| option | where | behavior |
| --- | --- | --- |
| `Title` | `main.go:42` | `"Auralis"`. |
| `Width`, `Height` | `main.go:43-44` | 1280 by 800. |
| `MinWidth`, `MinHeight` | `main.go:45-46` | 1200 by 720. |
| `Frameless` | `main.go:47` | `true`. The React title bar calls `WindowMinimise`, `WindowToggleMaximise`, and `Quit` (§5). |
| `AssetServer` | `main.go:48-50` | Embedded `frontend/dist` (`main.go:16-17`). |
| `BackgroundColour` | `main.go:51` | Opaque black (`R:0 G:0 B:0 A:255`). |
| `OnStartup` | `main.go:52` → `app.startup` `app.go:343` | Registers the verification presenter, which emits `source-verification` (`app.go:344-346`). Stores `ctx` (`app.go:347`). `backend.RegisterAuralisProtocol()` (`app.go:348`). `backend.HandleProtocolArgs(os.Args[1:])` (`app.go:351`). Installs community verification handlers that call `BrowserOpenURL`, `WindowShow`, and `WindowUnminimise` (`app.go:352-358`). Opens the history, queue, library-index, and ISRC databases (`app.go:360-371`). `CleanupLegacyTidalPublicAPIState` (`app.go:372`). `MigratePersistedConfigSettings` (`app.go:375`). `startIPodWatch` (`app.go:378`, `app_ipod.go:11`) and `startSyncWatch` (`app.go:379`, `app_sync.go:33`). |
| `OnShutdown` | `main.go:53` → `app.shutdown` `app.go:382` | Cancels a running library scan (`app.go:383-388`). `closeTaste` (`app.go:389`, `app_taste.go:115`). `stopIPodWatch` (`app.go:390`) and `stopSyncWatch` (`app.go:391`), which also calls `CancelSync` (`app_sync.go:46`). `backend.StopAcceptingDownloadsAndDrain` (`app.go:392`). Closes the library index, queue, history, and ISRC databases (`app.go:393-400`). |
| `OnBeforeClose` | `main.go:54` → `app.beforeClose` `close_lifecycle.go:16` | `closeGate.request` (`close_gate.go:34`). `closeCold` or `closeApproved` returns `allowClose`, and `beforeClose` returns `false` so Wails closes the window (`close_lifecycle.go:18-19`, `close_gate.go:37-39`). `closeReady` moves to `closeSaving`, returns `flushBeforeClose`, emits `app:before-close`, and `beforeClose` returns `true` to cancel the close (`close_lifecycle.go:20-23`, `close_gate.go:40-42`). Any later request while saving returns `waitForCloseFlush`, and `beforeClose` returns `true` without a second event (`close_gate.go:43-44`). |
| `SingleInstanceLock` | `main.go:55-59` | `UniqueId` `"auralis-desktop"`. `OnSecondInstanceLaunch` calls `backend.HandleProtocolArgs(data.Args)` (`main.go:57`). |
| `DragAndDrop` | `main.go:60-65` | `EnableFileDrop: true`, `DisableWebViewDrop: false`, CSS property `--wails-drop-target`, value `drop`. Paired with frontend `OnFileDrop` (§5). |
| `Bind` | `main.go:66-68` | Binds the one `*App` value. That is the entire IPC surface in §2. |
| `Windows` | `main.go:69-75` | `WebviewUserDataPath` is the isolated profile from `main.go:35`. `WebviewIsTransparent` false, `WindowIsTranslucent` false, `DisableWindowIcon` false, `DisableFramelessWindowDecorations` false. |

The close handshake on the frontend is part of the same contract. `main.tsx:20` listens for `app:before-close`, flushes the queue (`main.tsx:23`), and calls `FinishClose(saved)` (`main.tsx:34`). `FrontendReady` (`close_lifecycle.go:8`, called from `main.tsx:36`) moves the gate from `closeCold` to `closeReady` (`close_gate.go:27-32`). Until that call, a close is allowed immediately. `FinishClose` (`close_lifecycle.go:11`) calls `complete` (`close_gate.go:47`). If `saved` is true the gate becomes `closeApproved` and `runtime.Quit` runs (`close_lifecycle.go:12-13`). If `saved` is false the gate returns to `closeReady` and the window stays open, so the next close attempt emits `app:before-close` again.
