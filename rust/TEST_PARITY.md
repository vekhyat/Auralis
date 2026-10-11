# Test Parity Checklist: Go to Rust Port

This document tracks every Go test function in the Auralis codebase and its corresponding Rust test in the Rust workspace (`rust/crates/**`). Every Go test case must eventually get a Rust test; this document serves as the complete migration checklist.

## Target Crate Parity Summary

| Target Crate | Go Test Count | Already Ported Count | Live-Network Count | Notes / Status |
| :--- | :---: | :---: | :---: | :--- |
| `auralis-core` | 2 | 2 | 0 | Crate implemented |
| `auralis-config` | 10 | 7 | 0 | Crate implemented |
| `auralis-store` | 3 | 3 | 0 | Crate implemented |
| `auralis-ffmpeg` | 3 | 2 | 0 | Crate implemented |
| `auralis-tagger` | 0 | 0 | 0 | 6 Rust tests exist (new crate; Go tests were in download/analysis) |
| `auralis-providers` | 27 | 7 | 1 | Crate implemented |
| `auralis-sources` | 107 | 19 | 6 | Crate not created yet; ported tests currently live in `auralis-providers` (zarz, antra) |
| `auralis-download` | 16 | 3 | 0 | Crate not created yet; ported tests currently live in existing crates |
| `auralis-library` | 51 | 1 | 0 | Crate not created yet; ported test currently lives in `auralis-store` |
| `auralis-sync` | 59 | 0 | 0 | Pending crate rewrite |
| `auralis-device-ipod` | 38 | 0 | 0 | Pending crate rewrite |
| `auralis-taste` | 20 | 0 | 0 | Pending crate rewrite |
| `auralis-verify` | 53 | 0 | 0 | Pending crate rewrite |
| `auralis-platform` | 0 | 0 | 0 | Platform abstraction shims (Tauri v2 OS shims) |
| `auralis-app` | 8 | 0 | 0 | Pending crate rewrite |
| **Total** | **397** | **44** | **7** | **93 test files cataloged** |

---

## Go Test Files and Parity Tables

### `app_library_test.go`

- **Target Crate**: `auralis-app`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAppLibraryProfiles` | `auralis-app` | no (unit test) | — | `missing` | — |
| `TestAppCreateM3U8File` | `auralis-app` | no (local fs) | — | `missing` | — |
| `TestAppLibraryScanAndDoctorWorkflow` | `auralis-app` | no (local fs) | — | `missing` | — |
| `TestAppLibraryAsyncScanAndCancel` | `auralis-app` | no (local fs) | — | `missing` | — |
| `TestAppExportLibraryPlaylist` | `auralis-app` | no (local fs) | — | `missing` | — |

### `app_sync_test.go`

- **Target Crate**: `auralis-app`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSyncRequiresStartedJobToResume` | `auralis-app` | no (unit test) | — | `missing` | — |
| `TestSyncRejectsChangedRootAndCorruptManifest` | `auralis-app` | no (local fs) | — | `missing` | — |
| `TestFolderSyncWritesSelectedPlaylist` | `auralis-app` | no (local fs) | — | `missing` | — |

### `backend/amazon_stream_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestIsTransientAmazonStreamError` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestAmazonStreamClientTimeout` | `auralis-providers` | no (unit test) | — | `missing` | — |

### `backend/analysis_test.go`

- **Target Crate**: `auralis-download`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRejectLongAnalysis` | `auralis-download` | no (unit test) | — | `missing` | — |

### `backend/antra_amazon_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 6

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAntraTextSearchContracts` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestAntraAmazonResolvePassesGrantedKey` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestAntraAmazonRemuxDecodesWhenFFmpegPresent` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestAntraAmazonMalformedKeyDoesNotLeak` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestAntraAmazonTrackRejectsAudioBody`<br>• subtests (3): `audio content type`, `json labeled audio`, `asin route` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestAntraAmazonAtmosRejectsNonSpatialCodec`<br>• subtests (4): `streamUrl`, `decryptionKey`, `asin`, `codec` | `auralis-sources` | no (httptest only) | — | `missing` | — |

### `backend/antra_qobuz_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestQobuzSearchUsesAntraBeforeCatalog` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestQobuzDirectIDSkipsCatalog` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestQobuzDownloadUsesAntraBeforeGateways` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestQobuzCustomInstancePrecedesAntra` | `auralis-sources` | no (httptest only) | — | `missing` | — |

### `backend/antra_sources_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 10

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParseAntraSearchHit` | `auralis-sources` | no (unit test) | `auralis-providers::antra::tests::parse_mock_manifest_json` | `ported` | — |
| `TestParseAntraSearchHitNumericID` | `auralis-sources` | no (unit test) | `auralis-providers::antra::tests::parse_mock_manifest_json` | `ported` | — |
| `TestExtractAppleTrackID` | `auralis-sources` | no (unit test) | `auralis-providers::apple::tests::test_extract_apple_track_id` | `ported` | — |
| `TestDeezerBlowfishRoundTrip` | `auralis-sources` | no (unit test) | `auralis-providers::deezer_crypto::tests::test_get_blowfish_key_derivation_vectors` | `ported` | — |
| `TestDecryptJioSaavnURL` | `auralis-sources` | no (unit test) | `auralis-providers::jiosaavn::tests::test_decrypt_jiosaavn_url_round_trip` | `ported` | — |
| `TestJioSaavnQueriesPreferCleanTitle` | `auralis-sources` | no (unit test) | `auralis-providers::jiosaavn::tests::test_jiosaavn_queries_variations` | `ported` | — |
| `TestReplaceAudioExtension` | `auralis-sources` | no (unit test) | `auralis-providers::deezer::tests::test_detect_audio_extension` | `ported` | — |
| `TestSanitizeDownloaderIncludesNewSources` | `auralis-sources` | no (unit test) | `auralis-config::sanitizer::tests::test_sanitization_rules` | `ported` | — |
| `TestSanitizeAutoOrderDropsRetiredSources` | `auralis-sources` | no (unit test) | `auralis-config::sanitizer::tests::test_sanitization_rules` | `ported` | — |
| `TestAntraQualityQuery` | `auralis-sources` | no (unit test) | `auralis-providers::amazon::tests::test_normalize_amazon_quality` | `ported` | — |

### `backend/app_paths_test.go`

- **Target Crate**: `auralis-config`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAlternateAppDirectoryAlsoIsolatesWebviewProfile` | `auralis-config` | no (local fs) | `auralis-config::paths::tests::test_app_dir_override` | `partial` | Windows WebView2 |

### `backend/community_adapters_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 14

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCommunityQobuzRESTUsesKeyAndMapsQuality` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityQobuzDLShapesAndFailedResponses`<br>• subtests (5): `success`, `url`, `success`, `url`, `track_id` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityHiFiRejectsPreviewWrongTrackAndProtectedManifest`<br>• subtests (1): `PREVIEW` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityHiFiPollsQueueAndReturnsManifest` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityHiFiCancelsQueuedRequestWithoutLosingAuth` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityDABMatchesRecordingBeforeResolvingItsOwnID` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityLucidaParsesPageAndRunsHandoff` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityQobuzRouteDownloadsThroughNormalPipelineWithoutLeakingKey` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityRouteRejectsTranscodedAudio` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunitySourceSettingsValidateAndPreserveCorruption` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunitySubsonicMatchesAndAuthenticatesWithoutSendingPassword` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityHTMLInterstitialIsReportedAsAuthenticationRequired` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCommunityAdaptersRefuseAtmosInsteadOfDowngrading` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestCommunitySourceChangeDropsStaleChecksAndPerformance` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/community_check_semantics_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestExpectSourceJSONDistinguishesChallengeFromBroken` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestCheckCommunitySourceBrokenEndpointIsGenericFailure`<br>• subtests (4): `DABGenericHTML`, `DABEmpty`, `QobuzGenericHTML`, `DABChallengeStaysAuthentication` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestProbeLucidaRejectsWrongIdentity`<br>• subtests (5): `Matching`, `SongKindMatching`, `Wrong`, `Missing`, `CheckIntegration` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestCheckCommunitySourceCredentialActionSemantics`<br>• subtests: 7 subtests | `auralis-sources` | no (httptest only) | — | `missing` | — |

### `backend/community_session_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestImportCommunitySessionKeepsValidAuralis` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestImportCommunitySessionCopiesSpotiFLAC` | `auralis-sources` | no (unit test) | — | `missing` | — |

### `backend/community_source_audit_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCommunityAuditAudioOutcomeReplacesAvailableStatus` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunityAuditOuterFailureRedactsSecretsAndURLs` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunityAuditReportPersistsFinalAudioStatus` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/community_source_candidates_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 0

> [!NOTE]
> Test fixture file: defines test-only candidate catalog helper `testCommunityCandidateCatalog()`. Contains no `func Test*` functions.

### `backend/community_sources_live_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLiveCommunitySourceAdapters` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |

### `backend/community_sources_policy_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 7

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCleanInstallCommunitySourcesDefaultsDisabled`<br>• subtests (3): `tidal`, `qobuz`, `amazon` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunitySourcesSavedOptInRetainsRoute` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunitySourcesSettingsResetRevertsToDisabled` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestCommunitySourcesEvidencePreservedOnToggle` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestRetiredPublicPresetsExcludedFromPersistedSettings` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestRepointedLegacyIDPreservedAsCustom` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestEmptyRegistryProducesNoAttemptsAfterReset` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/community_sources_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRetiredSourcesDoNotReturnFromSavedSettings` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/credentials/store_other_test.go`

- **Target Crate**: `auralis-config`
- **Build Tags**: `//go:build !windows`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestStoreFailsClosedOffWindows` | `auralis-config` | no (local fs) | — | `missing` | //go:build !windows |

### `backend/credentials/store_windows_test.go`

- **Target Crate**: `auralis-config`
- **Build Tags**: `//go:build windows`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestStoreRoundTripWindows` | `auralis-config` | no (local fs) | — | `missing` | //go:build windows; Windows DPAPI |
| `TestStoreOpenAtValidatesDir` | `auralis-config` | no (unit test) | — | `missing` | //go:build windows; Windows DPAPI |

### `backend/devices/adb/adb_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestQuoteArg`<br>• subtests (6): `hello`, `hello world`, `it's cold`, `$VAR `cmd` \`, `/sdcard/Music/Artist Name/01 - Track.mp3`, `日本語の音楽` | `auralis-sync` | no (unit test) | — | `missing` | ADB client |
| `TestParseDfOutput` | `auralis-sync` | no (unit test) | — | `missing` | ADB client |
| `TestClientWireProtocol` | `auralis-sync` | no (local fs) | — | `missing` | ADB client |
| `TestTargetSyncTargetInterface` | `auralis-sync` | no (local fs) | — | `missing` | ADB client |
| `TestTargetPutRenamesAndShellFailuresSurface` | `auralis-sync` | no (local fs) | — | `missing` | ADB client |

### `backend/devices/adb/download_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParsePlatformToolsPin` | `auralis-sync` | no (unit test) | — | `missing` | ADB client |
| `TestInstallPlatformToolsVerifiesAndSwaps` | `auralis-sync` | no (httptest only) | — | `missing` | runtime.GOOS == windows; ADB client |
| `TestInstallPlatformToolsRejectsBadChecksum` | `auralis-sync` | no (httptest only) | — | `missing` | ADB client |
| `TestInstallPlatformToolsRejectsSizeMismatch` | `auralis-sync` | no (httptest only) | — | `missing` | ADB client |
| `TestDownloadPlatformToolsRequiresOfficialMetadata` | `auralis-sync` | no (httptest only) | — | `missing` | ADB client |

### `backend/devices/adb/fake_server_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 0

> [!NOTE]
> Test fixture file: defines fake ADB server test fixture `newFakeADBServer()`. Contains no `func Test*` functions.

### `backend/devices/ipod/db_regression_test.go`

- **Target Crate**: `auralis-device-ipod`
- **Go Test Functions**: 7

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRegressionRefusesIncompleteLibraries`<br>• subtests: 1 dynamic cases | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestRegressionPlaylistSections`<br>• subtests (1): `type 3 alone` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestRegressionMasterIndexes`<br>• subtests (1): `album disc and track` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestRegressionSortKeys`<br>• subtests (4): `case fold`, `the artist`, `overrides`, `parsed library` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestRegressionRefusesUnsafeLayouts`<br>• subtests (4): `unsupported sort`, `short index`, `grouped`, `unreadable sort override` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestRegressionParserBounds`<br>• subtests: 7 subtests | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestRegressionSourceHashRoundTrip`<br>• subtests (1): `comments` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |

### `backend/devices/ipod/db_test.go`

- **Target Crate**: `auralis-device-ipod`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestHeaderSizes` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestHash58MatchesLibgpod` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestHash58RoundTrip` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestAddRemovePreservesUnknownSection` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestShuffleRoundTrip` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |

### `backend/devices/ipod/device_test.go`

- **Target Crate**: `auralis-device-ipod`
- **Go Test Functions**: 9

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestInspectClassicAndRefusals` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestRockboxCopiesOriginalAndLeavesStockDB` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestStockSendKeepsFilenameAndWritesBackup` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestFailedSignRemovesCopiedFiles` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestRemoveKeepsFileWhenDatabaseCannotBeSigned` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestBrokenDatabaseIsNotReplacedUntilRebuild` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestRemoveDeletesFileOnlyAfterDatabaseCommit` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestMacFormattedPodIsExplained` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestSafeInsideRejectsEscape` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |

### `backend/devices/ipod/transfer_test.go`

- **Target Crate**: `auralis-device-ipod`
- **Go Test Functions**: 17

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestDeviceFormatAndCodecSupport` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestDuplicatePrefersSourceHash` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestShuffleSDFailureRestoresBothDatabases` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestShuffleFirstSendRollsBackDatabaseAndAudio` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestShuffleSendRollsBackNewAudioWhenSDFails` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestRollbackFailureKeepsReferencedAudio` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestSendKeepsNewAudioWhenRollbackFails` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestCancelDuringDatabasePhaseDoesNotPublish` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestCommitBytesRefusesInaccessibleOriginal` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestCancelLastFilePreventsCommitAndRemovesCopies` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestCopyAndTranscodeRemoveIncompleteOutput` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestCancelledTranscodeRemovesOutput` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestShuffleRecoverySkipsUnsupportedAudio` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestSourceHashSkipsRepeatAfterReopen` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |
| `TestShuffle2TranscodesToAAC` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestTranscodeStoresOutputSampleRate` | `auralis-device-ipod` | no (unit test) | — | `missing` | iPod device |
| `TestShuffleTranscodesExistingALAC` | `auralis-device-ipod` | no (local fs) | — | `missing` | iPod device |

### `backend/devices/manager_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestDeviceManagerFoldersAndProfiles` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestDeviceProfileValidationAndPersistenceErrors` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestCorruptDeviceProfilesArePreserved` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/devices/massstorage/massstorage_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestMassStorageTarget` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestPutFailureKeepsPreviousCopy` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestMoveRefusesToReplace` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/devices/playlists_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestPlaylistSelectionResolvesRelativePaths` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestPlaylistAndScanErrorsStopPlanning` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/download_cancel_test.go`

- **Target Crate**: `auralis-download`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestBeginDownloadCancellationScopeDoesNotReuseCancelledContext` | `auralis-download` | no (unit test) | — | `missing` | — |
| `TestDownloadScopeFinishDoesNotCancelSibling` | `auralis-download` | no (unit test) | — | `missing` | — |
| `TestStopAcceptingDownloadsCancelsAndRejectsNewWork` | `auralis-download` | no (unit test) | — | `missing` | — |

### `backend/download_copy_test.go`

- **Target Crate**: `auralis-download`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCopyDownloadBodyCompletes` | `auralis-download` | no (unit test) | — | `missing` | — |
| `TestCopyDownloadBodyStalls` | `auralis-download` | no (unit test) | — | `missing` | — |
| `TestCopyDownloadBodyCancel` | `auralis-download` | no (unit test) | — | `missing` | — |
| `TestRetryableDownloadError` | `auralis-download` | no (unit test) | — | `missing` | — |

### `backend/download_validation_test.go`

- **Target Crate**: `auralis-download`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestValidateDownloadedTrackDurationRejectsUnreadableFile` | `auralis-download` | no (local fs) | — | `missing` | — |
| `TestAcceptExistingMediaRequiresReadableAudioAndDoesNotDelete` | `auralis-download` | no (local fs) | — | `missing` | — |
| `TestValidateDownloadedTrackDurationSkipsWhenNoExpectedDuration` | `auralis-download` | no (unit test) | — | `missing` | — |

### `backend/ffmpeg_integrity_test.go`

- **Target Crate**: `auralis-ffmpeg`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestFFmpegArchiveRejectsModifiedContents` | `auralis-ffmpeg` | no (local fs) | `auralis-ffmpeg::download::tests::test_verify_archive_sha256` | `ported` | — |
| `TestExecutableInstallPreservesOldFileOnReadFailure` | `auralis-ffmpeg` | no (local fs) | `auralis-ffmpeg::locator::tests::test_validate_executable_invalid_name` | `partial` | — |
| `TestBoundedResponseAcceptsLimitAndRejectsOverflow` | `auralis-ffmpeg` | no (unit test) | — | `missing` | — |

### `backend/file_acl_test.go`

- **Target Crate**: `auralis-config`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRestrictPrivateFile` | `auralis-config` | no (local fs) | `auralis-config::file_acl::tests::test_restrict_private_file` | `ported` | runtime.GOOS == windows; Windows file ACL |
| `TestRestrictPrivateFileSurvivesGC` | `auralis-config` | no (local fs) | `auralis-config::file_acl::tests::test_restrict_private_file` | `ported` | Windows file ACL |

### `backend/filename_integrity_test.go`

- **Target Crate**: `auralis-download`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestResolveOutputPathForDownloadIgnoresTinyLeftovers` | `auralis-download` | no (local fs) | `auralis-config::sanitizer::tests::test_sanitization_rules` | `partial` | — |
| `TestSanitizeManualRenameNameRejectsTraversal` | `auralis-download` | no (unit test) | `auralis-config::sanitizer::tests::test_sanitization_rules` | `ported` | — |
| `TestManualRenamePathStaysInFolder` | `auralis-download` | no (local fs) | `auralis-config::sanitizer::tests::test_sanitization_rules` | `partial` | — |
| `TestExistingAudioDownloadFindsSiblingM4A` | `auralis-download` | no (local fs) | — | `missing` | — |
| `TestExistingAudioDownloadIgnoresTinyLeftovers` | `auralis-download` | no (local fs) | — | `missing` | — |

### `backend/history_persistence_test.go`

- **Target Crate**: `auralis-store`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestHistoryWriteIsPromptAndRejectedAfterClose` | `auralis-store` | no (unit test) | `auralis-store::history_store::tests::test_history_store_crud` | `ported` | — |
| `TestPersistentQueueUsesAppDirOverride` | `auralis-store` | no (unit test) | `auralis-store::queue_store::tests::test_queue_store_crud_and_apply` | `ported` | — |

### `backend/http_headers_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSameHostCheckRedirectBlocksCrossOrigin` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestSameHostCheckRedirectAllowsSameHost` | `auralis-providers` | no (unit test) | — | `missing` | — |

### `backend/in_app_verification_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 10

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestVerificationPresentationDoesNotReviveClosedOrOlderRun` | `auralis-verify` | no (local fs) | — | `missing` | native verification window |
| `TestVerificationDoesNotUseTopLevelWindow` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestVerificationChildStyleStripsPopupChrome` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestVerificationFrameInsetsFrom`<br>• subtests (5): `edge chrome`, `no chrome`, `empty page`, `page outside window`, `mostly chrome` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestValidateVerificationTargetURL` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestPresentVerificationAllowsConfiguredOrigin` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestValidateVerificationNavigation`<br>• subtests (4): `https://dabmusic.xyz/start`, `https://dabmusic.xyz/start`, `https://monochrome-api.samidy.com/session`, `https://monochrome-api.samidy.com/session` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestVerificationPresentationOmitsSecrets` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestCancelInAppVerificationIsScoped` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |
| `TestSetVerificationViewportIsScoped` | `auralis-verify` | no (unit test) | — | `missing` | native verification window |

### `backend/in_app_verification_windows_test.go`

- **Target Crate**: `auralis-verify`
- **Build Tags**: `//go:build windows`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestNativeConfiguredVerificationConfirmsSession` | `auralis-verify` | no (httptest only) | — | `missing` | //go:build windows; native verification window |
| `TestPlaceVerificationWindowStaysChild` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestFindAuralisMainWindowIgnoresBrowserWindows` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestSuppressVerificationPopupDoesNotShowIt` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestNativeVerificationEdgeEmbedsInParent` | `auralis-verify` | no (local fs) | — | `missing` | //go:build windows; native verification window |

### `backend/jiosaavn_matching_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 10

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestJioSaavnOriginalArtistBeatsExactTitleCover` | `auralis-providers` | no (unit test) | `auralis-providers::jiosaavn::tests::test_jiosaavn_queries_variations` | `partial` | — |
| `TestJioSaavnTitleOnlyFallbackStillUsesRequestedArtist` | `auralis-providers` | no (unit test) | `auralis-providers::jiosaavn::tests::test_jiosaavn_queries_variations` | `partial` | — |
| `TestJioSaavnWrongArtistOnlyIsNoMatch` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnUnrelatedTitleIsNoMatch` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnVersionFallbackMatchesEitherSide` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnUnicodeCollaborationArtists` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnCommunityNestedArtists` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnDescriptionOnlyArtistStillMatches` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnDurationAndISRCAreEvidence` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestJioSaavnLiveComeTogetherSelection` | `auralis-providers` | yes (skipped unless AURALIS_JIOSAAVN_LIVE) | — | `missing` | — |

### `backend/jiosaavn_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestJioSaavnPickBestPrefersCloseTitleArtist` | `auralis-providers` | no (unit test) | `auralis-providers::jiosaavn::tests::test_jiosaavn_queries_variations` | `partial` | — |

### `backend/library/apply_journal_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 10

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestApplyTagsPreservesOtherTagsAndJournal` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestApplyRefusesChangedFileAndUndoRestores` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestMoveThroughIndexAndUndo` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestJournalRoundTrip` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestUndoLastEndToEndWithJournal` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestApplyRejectsPathsOutsideRoot` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestMoveCarriesLyricsSidecar` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestUndoRestoresMultiValueTag` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestBuildPlanCarriesOldTagValues` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPlanRetagsAndMovesOneFileInOneBatch` | `auralis-library` | no (local fs) | — | `missing` | — |

### `backend/library/fixtures_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestMain` | `auralis-library` | no (unit test) | — | `missing` | — |

### `backend/library/playlists_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 6

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestPlaylistRelativeMode` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPlaylistRootModeSkipsOutsideRoot` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPlaylistDeviceModePrefixesAndSkipsForeignVolume` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPlaylistRelativeDifferentVolumeSkipped` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestPlaylistLocalKeepsOtherDriveAbsolute` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestPlaylistUTF8AndForwardSlashes` | `auralis-library` | no (local fs) | — | `missing` | — |

### `backend/library/profiles_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 8

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRelativePathPoweramp` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestRelativePathMultiDiscAndMissingTrack` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestMediaServerYearOmittedWhenUnknown` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestSanitizeComponentFAT` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestSanitizeComponentTruncatesOnRuneBoundary` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestSanitizeRelativePathCannotEscape` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestPrimaryArtist` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestUnknownProfileFallsBackToPoweramp` | `auralis-library` | no (unit test) | — | `missing` | — |

### `backend/library/rules_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 16

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestDetectAlbumArtistMissingAndInconsistent` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestAlbumArtistFixSetsOneValue` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestCompilationDetectAndFix` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestTrackNumberGapIsReportedNotRenumbered` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestTrackNumberFixes` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestAlbumWideRulesSkipLooseFiles` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestLooksMultiDisc` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestIssueIDsAreUnique` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestTrackNumberUnparseable` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestDiscNumberInconsistent` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestCoverRules` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestPathRuleIllegalAndTemplate` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestMixedFormatRule` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestDuplicatesByISRCAndDuration` | `auralis-library` | no (unit test) | — | `missing` | — |
| `TestOrphanEmptyFolder` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestBuildPlanKeepsTagOpsFromDifferentRules` | `auralis-library` | no (unit test) | — | `missing` | — |

### `backend/library/scan_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestScanLibraryReadsTagsAndFolders` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestScanLibraryCancellation` | `auralis-library` | no (local fs) | — | `missing` | — |

### `backend/library_staging_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLibraryScanExcludesIncomingDownloads` | `auralis-library` | no (local fs) | — | `missing` | — |

### `backend/native_app_verification_test.go`

- **Target Crate**: `auralis-verify`
- **Build Tags**: `//go:build windows`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestNativeProductionAppVerificationPanel` | `auralis-verify` | no (httptest only) | — | `missing` | //go:build windows; Windows WebView2 |

### `backend/provider_cooldown_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestZarzRateLimitedError` | `auralis-providers` | no (unit test) | — | `missing` | — |

### `backend/qobuz_search_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 6

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestQobuzTitleSearchVariantsStripsMixYear` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestQobuzSearchQueriesPreferCleanTitle` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestLooksLikeQobuzReleaseVersion` | `auralis-providers` | no (unit test) | — | `missing` | — |
| `TestParseQobuzTrackIDFromURL` | `auralis-providers` | no (unit test) | `auralis-providers::qobuz::tests::test_extract_qobuz_track_id` | `ported` | — |
| `TestQobuzZarzSearchPathEncodesQuery` | `auralis-providers` | no (unit test) | `auralis-providers::qobuz::tests::test_qobuz_zarz_search_path` | `ported` | — |
| `TestQobuzTrackFromIDUsesSpotifyMetadata` | `auralis-providers` | no (unit test) | — | `missing` | — |

### `backend/recent_fetches_test.go`

- **Target Crate**: `auralis-store`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSaveRecentFetchesIsAtomic` | `auralis-store` | no (unit test) | `auralis-store::recent_fetches::tests::save_recent_fetches_is_atomic` | `ported` | — |

### `backend/resolver_context_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCatalogResolverSurvivesDownloadStop` | `auralis-providers` | no (httptest only) | — | `missing` | — |
| `TestDownloadResolverHonorsExplicitCancellation` | `auralis-providers` | no (unit test) | — | `missing` | — |

### `backend/session_auth_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestIsGatewaySessionFailure` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestSessionCredentialsValid` | `auralis-sources` | no (unit test) | — | `missing` | — |

### `backend/settings_store_test.go`

- **Target Crate**: `auralis-config`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLoadSettingsDoesNotRewrite` | `auralis-config` | no (local fs) | `auralis-config::settings_store::tests::test_settings_store_lifecycle` | `ported` | — |
| `TestMigrateSettingsIsAtomicAndIdempotent` | `auralis-config` | no (local fs) | `auralis-config::settings_store::tests::test_settings_store_lifecycle` | `ported` | — |
| `TestSaveSettingsRoundTripUnderContention` | `auralis-config` | no (unit test) | `auralis-config::settings_store::tests::test_settings_store_lifecycle` | `partial` | — |
| `TestWriteFileAtomicReplacesExistingFile` | `auralis-config` | no (local fs) | `auralis-config::atomic_file::tests::test_atomic_write_and_replace` | `ported` | — |

### `backend/source_acceptance_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestRankedSourcesRejectPreviewAndWrongCodec`<br>• subtests (3): `preview without catalog duration`, `lossy response to lossless request`, `stereo response to Atmos request` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestRankedSourceCannotSucceedWithoutAFile` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/source_auto_routing_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAutoRoutingExcludesRetiredServices` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestAutoRoutingDefaultsWithoutRetiredServices` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestDownloadSourcesRegistryListsOnlyVerifiedAutoRoutes` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestQobuzAutoAttemptsExcludeRetiredRoutes` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestTidalAutoAttemptsExcludeRetiredRoutes` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/source_connections_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAntraConnectionRequiresMatchingRecording`<br>• subtests (4): `track_id`, `track_id`, `track_id`, `track_id` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestResourceChecksRejectUnrelatedAndErrorPayloads`<br>• subtests: 15 cases | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestConfiguredSourcesNeverInheritBuiltInConnectionBehaviour` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestBuiltInDownloadSourcesAreNotVerifiable` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestSourceConnectionUnknownOrNonInteractiveDoesNotLaunchVerification` | `auralis-sources` | no (local fs) | — | `missing` | — |

### `backend/source_live_diagnostic_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLiveDownloadSources`<br>• subtests (1): `checked_at_utc` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |
| `TestLiveVerificationGatewayAvailability`<br>• subtests (4): `spotbye`, `zarz`, `zarz`, `zarz` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |

### `backend/source_priority_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 8

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSourceRankReliabilityBeforeLatency` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestSourceRankIgnoresOtherQualityAndExpiredResults` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestSourceRankingPersistenceAndCancellation` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestDownloadSourcesRejectInvalidAudioAndTryNext` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestDownloadSourcesStopOnCancellation` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestSourceRegistryOmitsRetiredHosts` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestRankedServicesKeepLossyAfterLossless` | `auralis-sources` | no (local fs) | — | `missing` | — |
| `TestResourceRankingRequiresSuccessfulResponses` | `auralis-sources` | no (unit test) | — | `missing` | — |

### `backend/source_ranked_live_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLiveRankedSourceBenchmark` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |

### `backend/source_resolver_audit_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLiveSourceResolverAudit` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |

### `backend/source_resource_audit_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLiveSourceResourceAudit` | `auralis-sources` | yes (skipped unless AURALIS_LIVE_SOURCE_CHECK) | — | `missing` | — |

### `backend/source_verification_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 12

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCommunityVerificationActionSeparatesBrowserAndConfiguration` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestManualVerificationDoesNotCloseOrStoreBeforeAPIAccepts` | `auralis-verify` | no (httptest only) | — | `missing` | — |
| `TestManualVerificationCancelDoesNotMarkVerified` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestManualVerificationFollowsDownloadContextCancel` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestManualVerificationTimeoutDoesNotMarkVerified` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestCommunitySourceChangeKeepsUnrelatedEvidence` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestBrowserSessionCookiesStayOnTheirOrigin` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestCommunityMediaRedirectDropsCookiesOffOrigin` | `auralis-verify` | no (httptest only) | — | `missing` | — |
| `TestHiFiAndLucidaChallengePagesAreAuthentication` | `auralis-verify` | no (httptest only) | — | `missing` | — |
| `TestVerificationTargetURLIsExactConfiguredOrigin` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestCaptureRequestsOnlyTheSelectedOrigin` | `auralis-verify` | no (local fs) | — | `missing` | — |
| `TestPlaintextBrowserSessionIsRejected` | `auralis-verify` | no (local fs) | — | `missing` | — |

### `backend/spotify_uri_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParseSpotifyCatalogHostsAndPlaylistUserPath`<br>• subtests (5): `https://open.spotify.com/track/abc123`, `https://www.open.spotify.com/album/alb456`, `https://play.spotify.com/playlist/pl789`, `https://open.spotify.com/user/someone/playlist/pluser`, `spotify:track:uri999` | `auralis-providers` | no (unit test) | `auralis-providers::spotify::tests::parses_open_urls_for_all_entity_kinds` | `ported` | — |
| `TestIsSpotifyShareHost` | `auralis-providers` | no (unit test) | `auralis-providers::spotify::tests::parses_open_urls_for_all_entity_kinds` | `ported` | — |

### `backend/syncengine/executor_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 8

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestExecutorCopiesAndManifest` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorResumeAfterCrash` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorDeleteGoesToTrash` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorResumeRebuildsManifestFromJournal` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorNeverOverwritesUnmanagedFiles` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorTrashKeepsRelativePath` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestPlanDisambiguatesCollidingPaths` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestExecutorThrottlesManifestUploads` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/syncengine/manifest_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestManifestRoundTrip` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/syncengine/planner_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestPlannerAddsUpdatesMovesDeletes` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestPlannerFreeSpaceRefusal` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestPlannerProfileChangeForcesUpdate` | `auralis-sync` | no (unit test) | — | `missing` | — |

### `backend/syncengine/playlist_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestBuildM3U8` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestSafePlaylistName` | `auralis-sync` | no (unit test) | — | `missing` | — |

### `backend/syncengine/safety_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 26

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestValidateRemoteRel` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestPlannerIgnoresUnsafeManifest` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestPlannerStaleTranscodeRecopies` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestSnapshotAssociation` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorRejectsUnsafePlan` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorBoundedConcurrency` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorContextCancellation` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorVerifiesSizes` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestManifestCleanUnsafe` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestWriteRemoteManifestRoundTrip` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestManifestSaveAtomic` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestPlaylistConfinesDirAndTracks` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestTranscodeRejectsEmptyHash` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestIsLosslessDecisions`<br>• subtests: 9 cases | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestSourceTrackLosslessSnapshotRoundTrip` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestPlanHashBindsContentAndSettings` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestSnapshotPlanRestoresSettingsAndCounts` | `auralis-sync` | no (unit test) | — | `missing` | — |
| `TestExecutorRefusesRemainingSpace` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorResumeAccountsRemainingSpace` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorPropagatesFreeSpaceError` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorRepairsMissingKeep` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorRepairsMismatchedKeep` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestRunDeletePropagatesStatErrors` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestRunMoveReconcilesCompletedMove` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestJournalTruncatesTornTail` | `auralis-sync` | no (local fs) | — | `missing` | — |
| `TestExecutorSkipsSameSizedUnmanagedFile` | `auralis-sync` | no (unit test) | — | `missing` | — |

### `backend/syncengine/transcode_test.go`

- **Target Crate**: `auralis-sync`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestTranscodeKeepsTagsAndCover`<br>• subtests (1): `TITLE` | `auralis-sync` | no (local fs) | — | `missing` | — |

### `backend/taste/lastfm_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLastFMSource` | `auralis-taste` | no (httptest only) | — | `missing` | — |

### `backend/taste/pkce_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestPKCEVerifier` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestPKCEChallenge` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestRandomStateAndValidState` | `auralis-taste` | no (unit test) | — | `missing` | — |

### `backend/taste/profile_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestProfileAffinityAndDecay` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestProfilePinAndBan` | `auralis-taste` | no (unit test) | — | `missing` | — |

### `backend/taste/shelves_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestLikedNotDownloadedShelf` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestFinishAlbumsShelf` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestSimilarArtistCandidates` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestCappingAndDiversity` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestOwnedAlbumsAreFilteredAndGapsKeepFullReleases` | `auralis-taste` | no (local fs) | — | `missing` | — |

### `backend/taste/spotify_api_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSpotifyAPIRefreshAndRetry` | `auralis-taste` | no (httptest only) | — | `missing` | — |
| `TestSpotifyPagination` | `auralis-taste` | no (httptest only) | — | `missing` | — |

### `backend/taste/spotify_connect_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestSpotifyConnectLoopbackFlow` | `auralis-taste` | no (httptest only) | — | `missing` | — |
| `TestSpotifyRegisteredRedirectUsesPortlessLoopback` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestSpotifyPaginationStaysOnAPIHost` | `auralis-taste` | no (unit test) | — | `missing` | — |

### `backend/taste/spotify_export_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 3

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParseExtended` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestParseLegacy` | `auralis-taste` | no (unit test) | — | `missing` | — |
| `TestImportDirAndZip` | `auralis-taste` | no (local fs) | — | `missing` | — |

### `backend/taste/store_test.go`

- **Target Crate**: `auralis-taste`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestStoreIdempotenceAndFeedback` | `auralis-taste` | no (local fs) | — | `missing` | — |

### `backend/tidal_cleanup_test.go`

- **Target Crate**: `auralis-providers`
- **Go Test Functions**: 1

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCleanupTidalDownloadArtifactsRemovesM4ASibling` | `auralis-providers` | no (local fs) | — | `missing` | — |

### `backend/tidal_source_session_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestHiFiMediaAndManifestUseVerifiedSession` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestHiFiRedirectDoesNotForwardEnvironmentCredentials` | `auralis-sources` | no (httptest only) | — | `missing` | — |

### `backend/verification_gate_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestDownloadVerificationConfirmsItselfAndAsksOnce` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestClosedDownloadVerificationIsNotAskedAgain` | `auralis-verify` | no (unit test) | — | `missing` | — |

### `backend/verify_extension_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestWriteVerifyExtension` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestVerifyExtensionLoadArgs` | `auralis-verify` | no (unit test) | — | `missing` | — |

### `backend/verify_hosts_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAllowedVerificationHost` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestValidateVerificationChallengeURL` | `auralis-verify` | no (unit test) | — | `missing` | — |

### `backend/verify_page_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestVerificationPageIsNetworkError` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestVerificationPageIsReady` | `auralis-verify` | no (unit test) | — | `missing` | — |

### `backend/verify_policy_test.go`

- **Target Crate**: `auralis-verify`
- **Go Test Functions**: 13

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestVerificationBackgroundGraceIsPassiveWindow` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestVerificationShouldReveal` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestClampVerificationPopupSize` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestVerificationWindowOutcome` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestFormatVerificationFailure` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestIsVerificationAppWindowClass` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestSelectVerificationGrantDismissUnblocks` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestSelectVerificationGrantPrefersGrantWhenWindowAlsoEnds` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestSelectVerificationGrantTimesOutWithActionableError` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestWatchVerificationWindowOnlyOnEmbeddedPlatform` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestStartVerificationWindowHonorsCancelledDownload` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestStartVerificationWindowInvalidHostUnwinds` | `auralis-verify` | no (unit test) | — | `missing` | — |
| `TestArmVerificationRunSerializesDuplicateCallers` | `auralis-verify` | no (unit test) | — | `missing` | — |

### `backend/verify_window_windows_test.go`

- **Target Crate**: `auralis-verify`
- **Build Tags**: `//go:build windows`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestVerificationBrowserArgsAvoidStartupNavigation` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestVerificationStyleIndexesUseRuntimeSignedValues` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestOpenVerificationWindowRejectsNonHTTPS` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |
| `TestOpenVerificationWindowHonorsCancelledContext` | `auralis-verify` | no (unit test) | — | `missing` | //go:build windows; native verification window |

### `backend/zarz_download_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestAllowGatewayFetchURL` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestGatewayDialIPsPinsLiteral` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestFetchZarzAtmosManifestFollowsSameHostRedirect` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestFetchZarzAtmosManifestBlocksCrossHostRedirect` | `auralis-sources` | no (httptest only) | — | `missing` | — |

### `backend/zarz_grant_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParseZarzGrantFromProtocolURL` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::parses_grant_from_protocol_url` | `ported` | — |
| `TestParseZarzGrantRejectsBareProtocolWithoutGrant` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::rejects_bare_protocol_without_grant` | `ported` | — |
| `TestParseZarzGrantAcceptsRawToken` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::accepts_raw_token` | `ported` | — |
| `TestDeliverZarzGrantDropsMismatchAndIdleJunk` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::deliver_drops_mismatched_state_and_idle_junk` | `ported` | — |
| `TestWaitForZarzGrantDismissUnblocks` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::wait_with_abort_unblocks` | `ported` | — |

### `backend/zarz_refresh_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 8

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestZarzRetryAfterHeaderSeconds` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestParseZarzAPIErrorPrefersBodyRetryAfter` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestDoZarzSignedRequestCapturesRetryAfter` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestRefreshZarzSessionLockedAppliesNewCredentials` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestEnsureZarzSessionRefreshesWhenDue` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestEnsureZarzSessionKeepsValidSessionWhenNotDue` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestFetchZarzAtmosManifestWrapsBodyAsBase64` | `auralis-sources` | no (httptest only) | — | `missing` | — |
| `TestZarzRefreshDue` | `auralis-sources` | no (unit test) | — | `missing` | — |

### `backend/zarz_resolve_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 5

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestParseZarzResolveLinks` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::parses_resolve_links` | `ported` | — |
| `TestParseZarzResolveRejectsFailure` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::rejects_failure_body` | `ported` | — |
| `TestMergeZarzResolveDoesNotOverwriteExisting` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::rejects_body_without_links` | `partial` | — |
| `TestTidalPublicSearchPathUsesUSCatalog` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestAmazonZarzFallsBackToFlacCodec` | `auralis-sources` | no (unit test) | `auralis-providers::zarz::tests::amazon_normalize_handles_track_asin_and_paths` | `ported` | — |

### `backend/zarz_session_test.go`

- **Target Crate**: `auralis-sources`
- **Go Test Functions**: 4

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestImportZarzStoreUsesAuralisWhenValid` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestImportZarzStoreCopiesSpotiFLACSession` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestImportZarzStoreReplacesEmptyAuralisWithValidLegacy` | `auralis-sources` | no (unit test) | — | `missing` | — |
| `TestParseZarzStoreDataLegacyRecord` | `auralis-sources` | no (unit test) | — | `missing` | — |

### `close_gate_test.go`

- **Target Crate**: `auralis-core`
- **Go Test Functions**: 2

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestCloseWaitsForSaveAndCanRetryAfterFailure` | `auralis-core` | no (unit test) | `auralis-core::close_gate::tests::test_close_waits_for_save_and_can_retry_after_failure` | `ported` | — |
| `TestCloseCannotBeApprovedBeforeARequest` | `auralis-core` | no (unit test) | `auralis-core::close_gate::tests::test_close_cannot_be_approved_before_request` | `ported` | — |

### `existing_media_test.go`

- **Target Crate**: `auralis-library`
- **Go Test Functions**: 7

| Go Test Name | Target Crate | Live Network | Rust Test | Status | Notes |
| :--- | :--- | :--- | :--- | :---: | :--- |
| `TestPublishSuffixSupportsMoreThan99Copies` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestFindExpectedFileRequiresReadableMedia` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPublishStagedDownloadReplacesInvalidAudioOnlyAfterValidation` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPublishKeepsValidExistingAudio` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPublishKeepsSmallInvalidAudioWhenTransferFails` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestPublishSuffixPreservesOccupantAndReturnsNewPaths` | `auralis-library` | no (local fs) | — | `missing` | — |
| `TestRecordDownloadHistoryWritesWithoutDelay` | `auralis-library` | no (local fs) | `auralis-store::history_store::tests::test_history_store_crud` | `ported` | — |

