package adb

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeRepoXML = `<?xml version="1.0"?>
<sdk:sdk-repository xmlns:sdk="http://schemas.android.com/sdk/android/repo/repository2/01">
  <remotePackage path="platform-tools">
    <revision><major>36</major></revision>
    <channelRef ref="channel-0"/>
    <archives>
      <archive><complete><size>11</size><checksum>aa335c3e7ececddc0e66b46727a5d60a9cc26239</checksum><url>platform-tools_r36.zip</url></complete><host-os>windows</host-os></archive>
    </archives>
  </remotePackage>
  <remotePackage path="platform-tools" obsolete="true">
    <revision><major>37</major></revision>
    <channelRef ref="channel-0"/>
    <archives>
      <archive><complete><size>22</size><checksum>2aa335c3e7ececddc0e66b46727a5d60a9cc262</checksum><url>x.zip</url></complete><host-os>windows</host-os></archive>
    </archives>
  </remotePackage>
  <remotePackage path="platform-tools">
    <revision><major>37</major><minor>0</minor><micro>1</micro></revision>
    <channelRef ref="channel-0"/>
    <archives>
      <archive><complete><size>101</size><checksum>3333333333333333333333333333333333333333</checksum><url>platform-tools_r37.0.1-windows.zip</url></complete><host-os>windows</host-os></archive>
      <archive><complete><size>102</size><checksum>4444444444444444444444444444444444444444</checksum><url>platform-tools_r37.0.1-linux.zip</url></complete><host-os>linux</host-os></archive>
    </archives>
  </remotePackage>
</sdk:sdk-repository>`

func TestParsePlatformToolsPin(t *testing.T) {
	pin, err := parsePlatformToolsPin([]byte(fakeRepoXML), "windows")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pin.size != 101 || !strings.HasSuffix(pin.url, "platform-tools_r37.0.1-windows.zip") || pin.sha1 == "" {
		t.Fatalf("unexpected pin: %+v", pin)
	}
	if pin.revMin != "37.0.1" {
		t.Fatalf("revMin = %q", pin.revMin)
	}
	if _, err := parsePlatformToolsPin([]byte(fakeRepoXML), "plan9"); err == nil {
		t.Fatal("expected unsupported OS to fail")
	}
}

func makePlatformToolsZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	payload := make([]byte, 300*1024)
	copy(payload, []byte("MZfake-pe"))
	w, err := zw.Create("platform-tools/adb.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	w2, err := zw.Create("platform-tools/fastboot.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w2.Write([]byte("MZfastboot-image-not-executable-payload padding for realism")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallPlatformToolsVerifiesAndSwaps(t *testing.T) {
	zipBytes := makePlatformToolsZip(t)
	sum := sha1.Sum(zipBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(zipBytes)))
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	pin := &platformToolsPin{url: srv.URL, size: int64(len(zipBytes)), sha1: hex.EncodeToString(sum[:])}
	target := filepath.Join(t.TempDir(), "platform-tools")
	if err := installPlatformTools(context.Background(), pin, target, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "adb.exe")); err != nil && runtime.GOOS == "windows" {
		t.Fatalf("adb.exe not installed: %v", err)
	}
	// away with an old tree; next install succeeds and replaces contents.
	bad := filepath.Join(target, "platform-tools", "adb")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err == nil {
		_ = os.WriteFile(bad, []byte("junk"), 0o644)
	}
	if err := installPlatformTools(context.Background(), pin, target, nil); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(target + ".auralis-staging"); !os.IsNotExist(err) {
		t.Fatal("staging directory left behind")
	}
}

func TestInstallPlatformToolsRejectsBadChecksum(t *testing.T) {
	zipBytes := makePlatformToolsZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	pin := &platformToolsPin{url: srv.URL, size: int64(len(zipBytes)), sha1: "0000000000000000000000000000000000000000"}
	target := filepath.Join(t.TempDir(), "platform-tools")
	if err := installPlatformTools(context.Background(), pin, target, nil); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("target dir must not exist after a rejected archive")
	}
	if _, err := os.Stat(target + ".auralis-staging"); !os.IsNotExist(err) {
		t.Fatal("staging dir must be cleaned up after rejection")
	}
}

func TestInstallPlatformToolsRejectsSizeMismatch(t *testing.T) {
	zipBytes := makePlatformToolsZip(t)
	sum := sha1.Sum(zipBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	pin := &platformToolsPin{url: srv.URL, size: int64(len(zipBytes)) + 1, sha1: hex.EncodeToString(sum[:])}
	target := filepath.Join(t.TempDir(), "platform-tools")
	if err := installPlatformTools(context.Background(), pin, target, nil); err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestDownloadPlatformToolsRequiresOfficialMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	oldXML, oldBase := platformToolsRepositoryXML, platformToolsDownloadBase
	platformToolsRepositoryXML = srv.URL + "/repo.xml"
	platformToolsDownloadBase = srv.URL + "/"
	defer func() { platformToolsRepositoryXML, platformToolsDownloadBase = oldXML, oldBase }()
	if err := DownloadPlatformTools(context.Background(), nil); err == nil {
		t.Fatal("expected metadata failure to abort the download")
	}
}
