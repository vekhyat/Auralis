# Auralis

Desktop app for fetching lossless audio from streaming links. Paste a Spotify URL or search, then download from Tidal, Qobuz, or Amazon Music.

Based on MIT-licensed [SpotiFLAC](https://github.com/spotbye/SpotiFLAC) by afkarxyz. See [LICENSE](LICENSE).

## Download

Windows builds will live on the [Releases](https://github.com/vekhyat/Auralis/releases) page.

## Build

Requires Go 1.26+, Node.js, pnpm, and [Wails](https://wails.io).

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

## Notes

- No account on this app is required.
- Auralis is not affiliated with Spotify, Tidal, Qobuz, Amazon Music, or any other streaming service.
