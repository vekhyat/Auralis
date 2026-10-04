# Auralis

Desktop app for fetching lossless audio from streaming links. Paste a Spotify URL or search, then download from Tidal, Qobuz, Amazon Music, Deezer, Apple Music, or JioSaavn.

## Download

Windows builds will live on the [Releases](https://github.com/vekhyat/Auralis/releases) page.

## Build

The checked toolchain is Go 1.26.7, Node.js 24.16.0, pnpm 11.23.0, and [Wails](https://wails.io) 2.16.0. The Wails CLI and Go module should use the same version.

```bash
cd frontend
pnpm install
cd ..
wails build
```

Development:

```bash
wails dev
```

Tests, build checks, and the changes in this upgrade are documented in [UPGRADE_NOTES.md](UPGRADE_NOTES.md).

## Notes

- No account on this app is required.
- Auralis is not affiliated with Spotify, Tidal, Qobuz, Amazon Music, or any other streaming service.
- Auralis is based on SpotiFLAC by afkarxyz. The original MIT copyright remains in [LICENSE](LICENSE).
