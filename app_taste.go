package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/vekhyat/Auralis/backend"
	"github.com/vekhyat/Auralis/backend/credentials"
	"github.com/vekhyat/Auralis/backend/taste"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) getTasteService() (*taste.Service, error) {
	a.tasteMu.Lock()
	defer a.tasteMu.Unlock()
	if a.taste != nil {
		return a.taste, nil
	}

	appDir, err := backend.EnsureAppDataDir()
	if err != nil {
		return nil, fmt.Errorf("app data dir: %w", err)
	}

	creds, err := credentials.Open()
	if err != nil {
		return nil, fmt.Errorf("credentials store: %w", err)
	}

	store, err := taste.OpenStore(filepath.Join(appDir, "taste.db"))
	if err != nil {
		return nil, fmt.Errorf("taste store: %w", err)
	}

	svc := taste.NewService(store, creds)
	svc.Spotify.OpenURL = func(url string) error {
		if a.ctx != nil {
			runtime.BrowserOpenURL(a.ctx, url)
		}
		return nil
	}

	svc.LibraryFilter = func(lookup taste.LibraryLookup) bool {
		root := tasteLibraryRoot()

		var filenames []string
		if lookup.Artist != "" && lookup.Title != "" {
			filenames = append(filenames, fmt.Sprintf("%s - %s", lookup.Artist, lookup.Title))
		}
		req := backend.LibraryIndexLookupRequest{
			Mode:      "hybrid",
			SpotifyID: lookup.SpotifyID,
			ISRC:      lookup.ISRC,
			Filenames: filenames,
		}
		_, found, err := backend.FindExistingLibraryFile(root, req)
		return err == nil && found
	}

	var albumsMu sync.Mutex
	var albums *taste.FolderAlbumIndex
	svc.AlbumOwned = func(artist, album string) bool {
		root := tasteLibraryRoot()
		albumsMu.Lock()
		if albums == nil || albums.Root != root {
			albums = &taste.FolderAlbumIndex{Root: root}
		}
		index := albums
		albumsMu.Unlock()
		return index.Owned(artist, album)
	}

	// FetchArtistDiscography seam: retrieves an artist's full albums using Spotify metadata client
	svc.FetchArtistDiscography = func(ctx context.Context, artistID string) ([]taste.GapAlbum, error) {
		client := backend.NewSpotifyMetadataClient()
		res, err := client.GetFilteredData(ctx, "https://open.spotify.com/artist/"+artistID, false, 0, nil)
		if err != nil {
			return nil, err
		}
		disco, ok := res.(*backend.ArtistDiscographyPayload)
		if !ok || disco == nil {
			return nil, errors.New("invalid discography payload")
		}
		gaps := make([]taste.GapAlbum, 0, len(disco.AlbumList))
		for _, alb := range disco.AlbumList {
			gaps = append(gaps, taste.GapAlbum{
				ID:          alb.ID,
				Name:        alb.Name,
				AlbumType:   alb.AlbumType,
				ReleaseDate: alb.ReleaseDate,
				Image:       alb.Images,
				TotalTracks: alb.TotalTracks,
			})
		}
		return gaps, nil
	}

	a.taste = svc
	return svc, nil
}

// tasteLibraryRoot is the download folder the "already owned?" checks use.
func tasteLibraryRoot() string {
	if settings, err := backend.LoadConfigSettings(); err == nil && settings != nil {
		if root, ok := settings["downloadPath"].(string); ok && root != "" {
			return root
		}
	}
	return backend.GetDefaultMusicPath()
}

func (a *App) closeTaste() {
	a.tasteMu.Lock()
	defer a.tasteMu.Unlock()
	if a.taste != nil {
		_ = a.taste.Close()
		a.taste = nil
	}
}

// GetTasteSettings retrieves connection status and meta.
func (a *App) GetTasteSettings() (taste.Settings, error) {
	svc, err := a.getTasteService()
	if err != nil {
		return taste.Settings{}, err
	}
	return svc.GetSettings(), nil
}

// SetForYouEnabled toggles the "For You" feature on or off.
func (a *App) SetForYouEnabled(enabled bool) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.SetForYouEnabled(enabled)
}

// SetSpotifyClientID configures the BYO Spotify Client ID.
func (a *App) SetSpotifyClientID(clientID string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.Spotify.SetClientID(clientID)
}

// ConnectSpotify initiates the loopback OAuth flow.
func (a *App) ConnectSpotify() error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.Spotify.Connect(ctx)
}

// DisconnectSpotify removes stored Spotify tokens.
func (a *App) DisconnectSpotify() error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.Spotify.Disconnect()
}

// SetLastFMCredentials stores the user's Last.fm API Key and username.
func (a *App) SetLastFMCredentials(apiKey, username string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.LastFM.SetCredentials(apiKey, username)
}

// DisconnectLastFM clears stored Last.fm credentials.
func (a *App) DisconnectLastFM() error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.LastFM.Disconnect()
}

// SelectSpotifyExportFile opens a file dialog to pick a zip or json Spotify export file.
func (a *App) SelectSpotifyExportFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Spotify Export",
		Filters: []runtime.FileFilter{
			{DisplayName: "Spotify Exports (*.zip, *.json)", Pattern: "*.zip;*.json"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// ImportSpotifyExport imports a Spotify JSON dump or zip file.
func (a *App) ImportSpotifyExport(path string) (int, error) {
	svc, err := a.getTasteService()
	if err != nil {
		return 0, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.ImportSpotifyExport(ctx, path, func(p taste.ImportProgress) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "taste:import-progress", p)
		}
	})
}

// SyncTasteNow triggers an asynchronous pull from configured sources.
func (a *App) SyncTasteNow() error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.SyncNow(ctx, func(p taste.SyncProgress) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "taste:sync-progress", p)
		}
	})
}

// CancelTasteSync cancels any active sync job.
func (a *App) CancelTasteSync() {
	svc, err := a.getTasteService()
	if err != nil {
		return
	}
	svc.CancelSync()
}

// GetTasteShelves computes and returns all recommendation shelves.
func (a *App) GetTasteShelves() ([]taste.Shelf, error) {
	svc, err := a.getTasteService()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.GetShelves(ctx)
}

// DismissTasteItem marks an item as dismissed so it doesn't reappear.
func (a *App) DismissTasteItem(id string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.DismissItem(id)
}

// BanTasteArtist marks an artist as not interested, removing them from all shelves.
func (a *App) BanTasteArtist(artist string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.BanArtist(artist)
}

// UnbanTasteArtist removes an artist from the banned list.
func (a *App) UnbanTasteArtist(artist string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.UnbanArtist(artist)
}

// PinTasteArtist gives an artist an affinity boost.
func (a *App) PinTasteArtist(artist string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.PinArtist(artist)
}

// UnpinTasteArtist removes the affinity boost.
func (a *App) UnpinTasteArtist(artist string) error {
	svc, err := a.getTasteService()
	if err != nil {
		return err
	}
	return svc.UnpinArtist(artist)
}

// GetTasteSummary returns the taste overview (top artists and genres).
func (a *App) GetTasteSummary() (taste.Summary, error) {
	svc, err := a.getTasteService()
	if err != nil {
		return taste.Summary{}, err
	}
	return svc.Summary()
}
