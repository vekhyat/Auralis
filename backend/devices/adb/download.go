package adb

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/vekhyat/Auralis/backend"
)

const (
	maxPlatformToolsSize = 128 << 20 // 128 MB max archive size
	googlePlatformToolsWin = "https://dl.google.com/android/repository/platform-tools-latest-windows.zip"
	googlePlatformToolsMac = "https://dl.google.com/android/repository/platform-tools-latest-darwin.zip"
	googlePlatformToolsLin = "https://dl.google.com/android/repository/platform-tools-latest-linux.zip"
)

// PlatformToolsDownloadURL returns the official Google download URL for the host OS.
func PlatformToolsDownloadURL() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return googlePlatformToolsWin, nil
	case "darwin":
		return googlePlatformToolsMac, nil
	case "linux":
		return googlePlatformToolsLin, nil
	default:
		return "", fmt.Errorf("unsupported operating system for platform-tools: %s", runtime.GOOS)
	}
}

// IsPlatformToolsInstalled returns true if adb executable is found.
func IsPlatformToolsInstalled() bool {
	_, err := GetADBExecutable()
	return err == nil
}

// DownloadPlatformTools downloads and extracts platform-tools from the official Google URL.
func DownloadPlatformTools(ctx context.Context, progressCallback func(percent int, status string)) error {
	appDir, err := backend.GetAppDir()
	if err != nil {
		return err
	}
	targetDir := filepath.Join(appDir, "platform-tools")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create platform-tools directory: %w", err)
	}

	url, err := PlatformToolsDownloadURL()
	if err != nil {
		return err
	}

	if progressCallback != nil {
		progressCallback(0, "Connecting to download server...")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Auralis")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download platform-tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if totalSize > maxPlatformToolsSize {
		return fmt.Errorf("platform-tools archive exceeds maximum expected size (%d bytes)", totalSize)
	}

	tmpFile, err := os.CreateTemp("", "platform-tools-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	var downloaded int64
	buf := make([]byte, 32*1024)
	limitReader := io.LimitReader(resp.Body, maxPlatformToolsSize+1)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := limitReader.Read(buf)
		if n > 0 {
			if _, werr := tmpFile.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			if downloaded > maxPlatformToolsSize {
				return fmt.Errorf("platform-tools download exceeded maximum size limit")
			}
			if totalSize > 0 && progressCallback != nil {
				pct := int(float64(downloaded) / float64(totalSize) * 80)
				progressCallback(pct, fmt.Sprintf("Downloading platform-tools (%.1f MB / %.1f MB)...",
					float64(downloaded)/(1024*1024), float64(totalSize)/(1024*1024)))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if progressCallback != nil {
		progressCallback(85, "Extracting platform-tools...")
	}

	if err := extractPlatformToolsZip(tmpFile.Name(), targetDir); err != nil {
		return fmt.Errorf("failed to extract platform-tools: %w", err)
	}

	if progressCallback != nil {
		progressCallback(100, "Platform-tools installed successfully")
	}
	return nil
}

func extractPlatformToolsZip(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue
		}

		// Strip top-level "platform-tools" directory if present in zip
		rel := strings.TrimPrefix(cleanName, "platform-tools"+string(filepath.Separator))
		rel = strings.TrimPrefix(rel, "platform-tools/")
		if rel == "" || rel == "platform-tools" {
			continue
		}

		destPath := filepath.Join(targetDir, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, copyErr := io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if copyErr != nil {
			return copyErr
		}

		if runtime.GOOS != "windows" {
			_ = os.Chmod(destPath, 0o755)
		}
	}
	return nil
}
