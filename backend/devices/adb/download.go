package adb

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
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
)

// platformToolsRepositoryXML and platformToolsDownloadBase locate Google's
// official SDK repository metadata. The metadata lists, per platform-tools
// revision, the expected archive size and SHA-1 checksum, so the download is
// validated against Google's published numbers rather than anything local.
var platformToolsRepositoryXML = "https://dl.google.com/android/repository/repository2-1.xml"
var platformToolsDownloadBase = "https://dl.google.com/android/repository/"

// PlatformToolsDownloadURL returns the official Google download URL for the host OS.
//
// It prefers the exact archive named by the live SDK repository metadata so
// the later download can be checked against its published size and checksum.
// The "latest" alias is never an acceptable substitute here: it has no
// published checksum, so downloading through it would skip validation.
func PlatformToolsDownloadURL() (string, error) {
	pin, err := fetchPlatformToolsPin(context.Background(), runtime.GOOS)
	if err != nil {
		return "", fmt.Errorf("resolve official platform-tools download: %w", err)
	}
	return pin.url, nil
}

func platformToolsHostOSName(goos string) (string, error) {
	switch goos {
	case "windows":
		return "windows", nil
	case "darwin":
		return "macosx", nil
	case "linux":
		return "linux", nil
	default:
		return "", fmt.Errorf("unsupported operating system for platform-tools: %s", goos)
	}
}

// platformToolsPin is one archive choice from the SDK repository metadata.
type platformToolsPin struct {
	url    string
	size   int64
	sha1   string // lowercase hex SHA-1 of the archive
	revMin string
}

type repoXML struct {
	XMLName string `xml:"sdk-repository"`
	Pkgs    []struct {
		Path     string `xml:"path,attr"`
		Obsolete string `xml:"obsolete,attr"`
		Revision struct {
			Major int `xml:"major"`
			Minor int `xml:"minor"`
			Micro int `xml:"micro"`
		} `xml:"revision"`
		ChannelRef struct {
			Ref string `xml:"ref,attr"`
		} `xml:"channelRef"`
		Archives []struct {
			Complete struct {
				Size     int64  `xml:"size"`
				Checksum string `xml:"checksum"`
				URL      string `xml:"url"`
			} `xml:"complete"`
			HostOS string `xml:"host-os"`
		} `xml:"archives>archive"`
	} `xml:"remotePackage"`
}

// parsePlatformToolsPin selects the newest non-obsolete stable platform-tools
// archive for the given Go OS.
func parsePlatformToolsPin(data []byte, goos string) (*platformToolsPin, error) {
	host, err := platformToolsHostOSName(goos)
	if err != nil {
		return nil, err
	}
	var repo repoXML
	if err := xml.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("parse SDK repository metadata: %w", err)
	}
	var best *platformToolsPin
	var bestMajor, bestMinor, bestMicro int
	for _, pkg := range repo.Pkgs {
		if pkg.Path != "platform-tools" || pkg.Obsolete == "true" || pkg.ChannelRef.Ref != "channel-0" {
			continue
		}
		for _, arc := range pkg.Archives {
			if !strings.EqualFold(arc.HostOS, host) {
				continue
			}
			if arc.Complete.Size <= 0 || arc.Complete.URL == "" {
				continue
			}
			if decoded, err := hex.DecodeString(strings.TrimSpace(arc.Complete.Checksum)); err != nil || len(decoded) != sha1.Size {
				continue
			}
			rev := []int{pkg.Revision.Major, pkg.Revision.Minor, pkg.Revision.Micro}
			if best != nil && (rev[0] < bestMajor ||
				(rev[0] == bestMajor && rev[1] < bestMinor) ||
				(rev[0] == bestMajor && rev[1] == bestMinor && rev[2] < bestMicro)) {
				continue
			}
			best = &platformToolsPin{
				url:    platformToolsDownloadBase + arc.Complete.URL,
				size:   arc.Complete.Size,
				sha1:   strings.ToLower(strings.TrimSpace(arc.Complete.Checksum)),
				revMin: fmt.Sprintf("%d.%d.%d", rev[0], rev[1], rev[2]),
			}
			bestMajor, bestMinor, bestMicro = rev[0], rev[1], rev[2]
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no stable platform-tools package for OS %q in SDK metadata", goos)
	}
	return best, nil
}

func fetchPlatformToolsPin(ctx context.Context, goos string) (*platformToolsPin, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, platformToolsRepositoryXML, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Auralis")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch SDK repository metadata: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	return parsePlatformToolsPin(body, goos)
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
	pin, err := fetchPlatformToolsPin(ctx, runtime.GOOS)
	if err != nil {
		return fmt.Errorf("resolve official platform-tools download: %w", err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}
	return installPlatformTools(ctx, pin, targetDir, progressCallback)
}

// installPlatformTools downloads pin.url, verifies size (and SHA-1 when the
// pin carries one), stages the extraction outside the existing install,
// validates the extracted adb binary, and only then swaps it into targetDir.
func installPlatformTools(ctx context.Context, pin *platformToolsPin, targetDir string, progressCallback func(percent int, status string)) error {
	if pin == nil || pin.size <= 0 || pin.sha1 == "" {
		return fmt.Errorf("install aborted: official platform-tools size and checksum are required")
	}
	if progressCallback != nil {
		progressCallback(0, "Connecting to download server...")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pin.url, nil)
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
	if resp.ContentLength > maxPlatformToolsSize {
		return fmt.Errorf("platform-tools archive exceeds maximum expected size (%d bytes)", resp.ContentLength)
	}
	if pin.size > 0 && resp.ContentLength > 0 && resp.ContentLength != pin.size {
		return fmt.Errorf("platform-tools archive size mismatch: server announced %d bytes, metadata says %d", resp.ContentLength, pin.size)
	}

	tmpFile, err := os.CreateTemp("", "platform-tools-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	hasher := sha1.New()
	var downloaded int64
	buf := make([]byte, 32*1024)
	limitReader := io.LimitReader(resp.Body, maxPlatformToolsSize+1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := limitReader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, werr := tmpFile.Write(chunk); werr != nil {
				return werr
			}
			hasher.Write(chunk)
			downloaded += int64(n)
			if downloaded > maxPlatformToolsSize {
				return fmt.Errorf("platform-tools download exceeded maximum size limit")
			}
			if progressCallback != nil {
				var pct int
				denominator := downloaded
				if pin.size > 0 {
					denominator = pin.size
				} else if resp.ContentLength > 0 {
					denominator = resp.ContentLength
				}
				if denominator > 0 {
					pct = int(float64(downloaded) / float64(denominator) * 80)
				}
				progressCallback(pct, fmt.Sprintf("Downloading platform-tools (%.1f MB / %.1f MB)...",
					float64(downloaded)/(1024*1024), float64(denominator)/(1024*1024)))
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

	// Size validation: what arrived must equal what the server announced
	// and what the official metadata declared.
	if resp.ContentLength > 0 && downloaded != resp.ContentLength {
		return fmt.Errorf("downloaded platform-tools archive is truncated: got %d of %d bytes", downloaded, resp.ContentLength)
	}
	if downloaded != pin.size {
		return fmt.Errorf("platform-tools size mismatch: got %d bytes, expected %d", downloaded, pin.size)
	}
	gotSum := hex.EncodeToString(hasher.Sum(nil))
	if gotSum != pin.sha1 {
		return fmt.Errorf("platform-tools checksum mismatch: got sha1 %s, expected %s", gotSum, pin.sha1)
	}

	if progressCallback != nil {
		progressCallback(85, "Extracting platform-tools...")
	}
	// Stage the extraction beside the real install, never over it: a
	// corrupt archive or an unexpected layout must not destroy a working
	// platform-tools that is already there.
	staging := targetDir + ".auralis-staging"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	if err := extractPlatformToolsZip(tmpFile.Name(), staging); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("failed to extract platform-tools: %w", err)
	}
	if err := validateExtracted(staging); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("extracted platform-tools looks invalid: %w", err)
	}
	// Swap in the validated tree; the previous one goes away only after
	// the new one is fully in place.
	backup := targetDir + ".auralis-old"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(targetDir); err == nil {
		if err := os.Rename(targetDir, backup); err != nil {
			_ = os.RemoveAll(staging)
			return fmt.Errorf("could not move aside previous platform-tools: %w", err)
		}
	}
	if err := os.Rename(staging, targetDir); err != nil {
		_ = os.Rename(backup, targetDir)
		_ = os.RemoveAll(staging)
		return fmt.Errorf("could not install staged platform-tools: %w", err)
	}
	_ = os.RemoveAll(backup)
	if progressCallback != nil {
		progressCallback(100, "Platform-tools installed successfully")
	}
	return nil
}

// validateExtracted checks that the staged directory holds an adb binary
// with a real executable image before it can replace the live install.
func validateExtracted(dir string) error {
	exe := "adb"
	if runtime.GOOS == "windows" {
		exe = "adb.exe"
	}
	f, err := os.Open(filepath.Join(dir, exe))
	if err != nil {
		return fmt.Errorf("adb executable missing: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 256*1024 {
		return fmt.Errorf("adb executable suspiciously small (%d bytes)", info.Size())
	}
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return err
	}
	switch {
	case string(magic[0:2]) == "MZ": // PE: Windows
	case magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F': // Linux
	case magic[0] == 0xcf && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe,
		magic[0] == 0xce && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe,
		magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xcf,
		magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xce: // macOS
	default:
		return fmt.Errorf("adb executable has an unknown image format")
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
		if !strings.HasPrefix(destPath, filepath.Clean(targetDir)+string(filepath.Separator)) {
			continue
		}
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
