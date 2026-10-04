package backend

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type resolvedTrackLinks struct {
	TidalURL  string
	AmazonURL string
	DeezerURL string
	QobuzURL  string
	ISRC      string
}

const (
	linkResolverProviderSongstats      = "songstats"
	linkResolverProviderDeezerSongLink = "deezer-songlink"
)

func (s *SongLinkClient) resolveSpotifyTrackLinks(spotifyTrackID string, region string) (*resolvedTrackLinks, error) {
	links := &resolvedTrackLinks{}
	var attempts []string

	isrc, err := s.lookupSpotifyISRC(spotifyTrackID)
	if err != nil {
		attempts = append(attempts, fmt.Sprintf("spotify isrc: %v", err))
	} else {
		links.ISRC = isrc
	}

	if added, zarzErr := s.resolveLinksViaZarz(links, spotifyTrackID); zarzErr != nil {
		attempts = append(attempts, fmt.Sprintf("zarz resolve: %v", zarzErr))
	} else if added {
		fmt.Println("Using Zarz resolve for Amazon/Qobuz links")
	}

	resolvers := orderedLinkResolvers()
	for _, resolver := range resolvers {
		switch resolver {
		case linkResolverProviderSongstats:
			addedData, songstatsErr := s.resolveLinksViaSongstats(links)
			if songstatsErr != nil {
				attempts = append(attempts, fmt.Sprintf("songstats: %v", songstatsErr))
			} else if addedData {
				fmt.Println("Using Songstats as configured link resolver")
			}
		case linkResolverProviderDeezerSongLink:
			addedData, songLinkErr := s.resolveLinksViaDeezerSongLink(links, spotifyTrackID, region)
			if songLinkErr != nil {
				attempts = append(attempts, fmt.Sprintf("songlink: %v", songLinkErr))
			} else if addedData {
				fmt.Println("Using Songlink as configured link resolver")
			}
		}

		if links.TidalURL != "" && links.AmazonURL != "" {
			return links, nil
		}
	}

	if hasAnySongLinkData(links) {
		return links, nil
	}

	if len(attempts) == 0 {
		attempts = append(attempts, "no streaming URLs found")
	}

	return links, errors.New(strings.Join(attempts, " | "))
}

func orderedLinkResolvers() []string {
	preferred := GetLinkResolverSetting()
	if !GetLinkResolverAllowFallback() {
		if preferred == linkResolverProviderDeezerSongLink {
			return []string{linkResolverProviderDeezerSongLink}
		}
		return []string{linkResolverProviderSongstats}
	}

	resources := []string{"resource-songstats", "resource-songlink"}
	if preferred == linkResolverProviderDeezerSongLink {
		resources[0], resources[1] = resources[1], resources[0]
	}
	ordered := RankedSourceIDs(resources, "resource")
	providers := make([]string, 0, len(ordered))
	for _, id := range ordered {
		if id == "resource-songstats" {
			providers = append(providers, linkResolverProviderSongstats)
		} else {
			providers = append(providers, linkResolverProviderDeezerSongLink)
		}
	}
	return providers
}

func (s *SongLinkClient) resolveLinksViaSongstats(links *resolvedTrackLinks) (matched bool, resultErr error) {
	started := time.Now()
	defer func() {
		if resultErr == nil && !matched {
			resultErr = fmt.Errorf("resolver returned no matching links")
		}
		recordSourceOutcome("resource-songstats", "", "resource", time.Since(started), matched && resultErr == nil, resultErr)
	}()
	if links == nil || links.ISRC == "" {
		return false, fmt.Errorf("ISRC is required for Songstats resolver")
	}

	before := *links

	fmt.Printf("Fetching Songstats links for ISRC %s\n", links.ISRC)
	if err := s.populateLinksFromSongstats(links, links.ISRC); err != nil {
		return false, err
	}

	return *links != before, nil
}

func (s *SongLinkClient) resolveLinksViaDeezerSongLink(links *resolvedTrackLinks, spotifyTrackID string, region string) (matched bool, resultErr error) {
	started := time.Now()
	defer func() {
		if resultErr == nil && !matched {
			resultErr = fmt.Errorf("resolver returned no matching links")
		}
		recordSourceOutcome("resource-songlink", "", "resource", time.Since(started), matched && resultErr == nil, resultErr)
	}()
	if links == nil {
		return false, fmt.Errorf("links is required for song.link resolver")
	}

	before := *links
	var attempts []string

	if trackID, err := extractSpotifyTrackID(spotifyTrackID); err != nil {
		attempts = append(attempts, fmt.Sprintf("spotify track id: %v", err))
	} else {
		fmt.Printf("Scraping song.link via Spotify track %s\n", trackID)
		data, scrapeErr := s.scrapeSongLinkPage(fmt.Sprintf("https://song.link/s/%s", trackID), region)
		if scrapeErr != nil {
			attempts = append(attempts, fmt.Sprintf("song.link spotify: %v", scrapeErr))
		} else {
			mergeSongLinkScrape(links, data)
		}
	}

	if (links.TidalURL == "" || links.AmazonURL == "") && links.ISRC != "" {
		if links.DeezerURL == "" {
			fmt.Printf("Resolving Deezer track from ISRC %s\n", links.ISRC)
			deezerURL, err := s.lookupDeezerTrackURLByISRC(links.ISRC)
			if err != nil {
				attempts = append(attempts, fmt.Sprintf("deezer isrc: %v", err))
			} else {
				links.DeezerURL = deezerURL
				fmt.Printf("Found Deezer URL: %s\n", links.DeezerURL)
			}
		}

		if links.DeezerURL != "" {
			if deezerTrackID, err := extractDeezerTrackID(links.DeezerURL); err != nil {
				attempts = append(attempts, fmt.Sprintf("deezer track id: %v", err))
			} else {
				fmt.Printf("Scraping song.link via Deezer track %s\n", deezerTrackID)
				data, scrapeErr := s.scrapeSongLinkPage(fmt.Sprintf("https://song.link/d/%s", deezerTrackID), region)
				if scrapeErr != nil {
					attempts = append(attempts, fmt.Sprintf("song.link deezer: %v", scrapeErr))
				} else {
					mergeSongLinkScrape(links, data)
				}
			}
		}
	}

	if *links != before {
		if len(attempts) == 0 {
			return true, nil
		}
		return true, errors.New(strings.Join(attempts, " | "))
	}

	if len(attempts) == 0 {
		attempts = append(attempts, "no links found via song.link")
	}

	return false, errors.New(strings.Join(attempts, " | "))
}
