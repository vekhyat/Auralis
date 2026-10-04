package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ExtraDownloadParams struct {
	Service              string
	ISRC                 string
	SpotifyID            string
	ServiceURL           string
	OutputDir            string
	FilenameFormat       string
	TrackName            string
	ArtistName           string
	AlbumName            string
	AlbumArtist          string
	ReleaseDate          string
	CoverURL             string
	SpotifyURL           string
	IncludeTrackNumber   bool
	Position             int
	UseAlbumTrackNumber  bool
	SpotifyTrackNumber   int
	SpotifyDiscNumber    int
	SpotifyTotalTracks   int
	SpotifyTotalDiscs    int
	Copyright            string
	Publisher            string
	Composer             string
	MetadataSeparator    string
	EmbedMaxQualityCover bool
	UseFirstArtistOnly   bool
	UseSingleGenre       bool
	EmbedGenre           bool
	DurationSeconds      int
}

func DownloadExtraService(p ExtraDownloadParams) (filename string, sourceURL string, err error) {
	if p.OutputDir == "" {
		p.OutputDir = "."
	}
	if err := os.MkdirAll(p.OutputDir, 0755); err != nil && p.OutputDir != "." {
		return "", "", err
	}
	if p.ISRC == "" && p.SpotifyID != "" {
		p.ISRC = ResolveTrackISRC(p.SpotifyID)
	}
	if p.SpotifyURL == "" && p.SpotifyID != "" {
		p.SpotifyURL = fmt.Sprintf("https://open.spotify.com/track/%s", p.SpotifyID)
	}

	artist := p.ArtistName
	albumArtist := p.AlbumArtist
	if p.UseFirstArtistOnly {
		artist = GetFirstArtist(p.ArtistName)
		albumArtist = GetFirstArtist(p.AlbumArtist)
	}

	expected := BuildExpectedFilename(p.TrackName, artist, p.AlbumName, albumArtist, p.ReleaseDate, p.FilenameFormat, "", "", p.IncludeTrackNumber, p.Position, p.SpotifyDiscNumber, p.UseAlbumTrackNumber, p.ISRC)
	dest := filepath.Join(p.OutputDir, expected)
	if !GetRedownloadWithSuffixSetting() {
		if existing, ok := existingAudioDownload(dest); ok {
			return "EXISTS:" + existing, "", nil
		}
	}
	dest, alreadyExists := ResolveOutputPathForDownload(dest, GetRedownloadWithSuffixSetting())
	if alreadyExists {
		return "EXISTS:" + dest, "", nil
	}

	switch strings.ToLower(strings.TrimSpace(p.Service)) {
	case "deezer":
		filename, sourceURL, err = downloadDeezerTrack(p, dest)
	case "apple":
		filename, sourceURL, err = downloadAppleTrack(p, dest)
	case "jiosaavn":
		filename, sourceURL, err = downloadJioSaavnTrack(p, dest)
	default:
		return "", "", fmt.Errorf("unknown extra service: %s", p.Service)
	}
	if err != nil {
		return "", sourceURL, err
	}
	if strings.HasPrefix(filename, "EXISTS:") {
		return filename, sourceURL, nil
	}
	if err := tagExtraDownload(filename, p); err != nil {
		fmt.Printf("Warning: failed to tag %s download: %v\n", p.Service, err)
	}
	return filename, sourceURL, nil
}

func tagExtraDownload(filePath string, p ExtraDownloadParams) error {
	coverPath := ""
	if p.CoverURL != "" {
		coverPath = filePath + ".cover.jpg"
		coverClient := NewCoverClient()
		if err := coverClient.DownloadCoverToPath(p.CoverURL, coverPath, p.EmbedMaxQualityCover); err != nil {
			fmt.Printf("Warning: failed to download cover: %v\n", err)
			coverPath = ""
		} else {
			defer os.Remove(coverPath)
		}
	}

	isrc := strings.TrimSpace(p.ISRC)
	upc := ""
	genre := ""
	if p.SpotifyURL != "" {
		if identifiers, err := GetSpotifyTrackIdentifiersWithContext(ActiveDownloadContext(), p.SpotifyURL); err == nil || identifiers.ISRC != "" || identifiers.UPC != "" {
			if isrc == "" {
				isrc = strings.TrimSpace(identifiers.ISRC)
			}
			upc = strings.TrimSpace(identifiers.UPC)
		}
	}
	if p.EmbedGenre && isrc != "" && !ShouldSkipMusicBrainzMetadataFetch() {
		if fetched, err := FetchMusicBrainzMetadata(isrc, p.TrackName, p.ArtistName, p.AlbumName, p.UseSingleGenre, p.EmbedGenre); err == nil {
			genre = fetched.Genre
		}
	}

	trackNumber := p.SpotifyTrackNumber
	if trackNumber == 0 {
		trackNumber = 1
	}
	metadata := Metadata{
		Title:       p.TrackName,
		Artist:      p.ArtistName,
		Album:       p.AlbumName,
		AlbumArtist: p.AlbumArtist,
		Date:        p.ReleaseDate,
		TrackNumber: trackNumber,
		TotalTracks: p.SpotifyTotalTracks,
		DiscNumber:  p.SpotifyDiscNumber,
		TotalDiscs:  p.SpotifyTotalDiscs,
		URL:         p.SpotifyURL,
		Comment:     p.SpotifyURL,
		Copyright:   p.Copyright,
		Publisher:   p.Publisher,
		Composer:    p.Composer,
		Separator:   p.MetadataSeparator,
		Description: "https://github.com/vekhyat/Auralis",
		ISRC:        isrc,
		UPC:         upc,
		Genre:       genre,
	}
	return EmbedMetadata(filePath, metadata, coverPath)
}

func resolveISRCOrSearch(p ExtraDownloadParams, service string) (antraSearchHit, error) {
	if p.ISRC != "" {
		hit, err := antraSearchByISRC(service, p.ISRC)
		if err == nil && hit.TrackID != "" {
			return hit, nil
		}
		fmt.Printf("[%s] ISRC search failed: %v\n", service, err)
	}
	query := strings.TrimSpace(p.TrackName + " " + GetFirstArtist(p.ArtistName))
	if query == "" {
		return antraSearchHit{}, fmt.Errorf("%s: no ISRC or search query", service)
	}
	return antraSearchText(service, query)
}
