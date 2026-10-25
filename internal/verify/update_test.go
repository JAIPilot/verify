package verify

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.1.1", "0.1.0", true},
		{"v0.2.0", "0.1.9", true},
		{"v1.0.0", "0.99.99", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.1.2", false},
	} {
		got, err := newerVersion(tc.latest, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("%s vs %s: got %v, %v", tc.latest, tc.current, got, err)
		}
	}
	for _, value := range []string{"v0.1", "v0.1.0-rc1", "v0.1.x", "v0/1/0", "v999999999999.1.0"} {
		if _, err := parseVersion(value); err == nil {
			t.Errorf("accepted invalid version %q", value)
		}
	}
}

func TestOfflineAutomaticCheckDoesNotBlockVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	oldURL := latestReleaseURL
	latestReleaseURL = server.URL + "/releases/latest"
	t.Cleanup(func() { latestReleaseURL = oldURL })
	result, err := checkAndInstallUpdate(context.Background(), false)
	if err != nil || result.Updated {
		t.Fatalf("offline automatic check blocked: %+v, %v", result, err)
	}
	if _, err := checkAndInstallUpdate(context.Background(), true); err == nil {
		t.Fatal("explicit update hid the release lookup error")
	}
}

func tarUpdateBinary(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "verify", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestUpdaterDownloadsChecksAndReplacesExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a delayed replacement helper")
	}
	dir := t.TempDir()
	installed := filepath.Join(dir, "verify")
	if err := os.WriteFile(installed, []byte("#!/bin/sh\necho 0.1.0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := tarUpdateBinary(t, []byte("#!/bin/sh\necho 0.1.1\n"))
	hash := sha256.Sum256(archive)
	asset, err := updateAssetName("v0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/v0.1.1", http.StatusFound)
		case "/releases/tag/v0.1.1":
			fmt.Fprint(w, "release")
		case "/releases/download/v0.1.1/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(hash[:]), asset)
		case "/releases/download/v0.1.1/" + asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldLatest, oldBase, oldExe, oldVersion := latestReleaseURL, releaseDownloadBase, updateExecutable, Version
	latestReleaseURL = server.URL + "/releases/latest"
	releaseDownloadBase = server.URL + "/releases/download"
	updateExecutable = func() (string, error) { return installed, nil }
	Version = "0.1.0"
	t.Cleanup(func() {
		latestReleaseURL, releaseDownloadBase, updateExecutable, Version = oldLatest, oldBase, oldExe, oldVersion
	})
	result, err := checkAndInstallUpdate(context.Background(), true)
	if err != nil || !result.Updated || result.Scheduled || result.Version != "v0.1.1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	output, err := exec.Command(installed, "--version").Output()
	if err != nil || strings.TrimSpace(string(output)) != "0.1.1" {
		t.Fatalf("installed executable reports %q, %v", output, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".verify-update-*")); len(matches) != 0 {
		t.Fatalf("staged update was left behind: %v", matches)
	}
}

func TestChecksumFailureDoesNotReplaceExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture executable is a POSIX shell script")
	}
	dir := t.TempDir()
	installed := filepath.Join(dir, "verify")
	original := []byte("#!/bin/sh\necho 0.1.0\n")
	if err := os.WriteFile(installed, original, 0755); err != nil {
		t.Fatal(err)
	}
	asset, err := updateAssetName("v0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	archive := tarUpdateBinary(t, []byte("#!/bin/sh\necho 0.1.1\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), asset)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldBase, oldExe := releaseDownloadBase, updateExecutable
	releaseDownloadBase = server.URL + "/releases/download"
	updateExecutable = func() (string, error) { return installed, nil }
	t.Cleanup(func() { releaseDownloadBase, updateExecutable = oldBase, oldExe })
	if _, err := installRelease(context.Background(), "v0.1.1"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum mismatch was accepted: %v", err)
	}
	current, err := os.ReadFile(installed)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("original executable changed: %q, %v", current, err)
	}
}

func TestZipArchiveRejectsWrongExecutable(t *testing.T) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	file, err := writer.Create("../verify.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractZipBinary(output.Bytes()); err == nil {
		t.Fatal("accepted an executable outside the archive root")
	}
}
