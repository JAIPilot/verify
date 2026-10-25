package verify

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maxUpdateArchive = 80 << 20
const maxUpdateBinary = 60 << 20

var latestReleaseURL = "https://github.com/JAIPilot/verify/releases/latest"
var releaseDownloadBase = "https://github.com/JAIPilot/verify/releases/download"
var updateHTTPClient = &http.Client{Timeout: 120 * time.Second}
var updateExecutable = os.Executable

type updateResult struct {
	Version   string
	Updated   bool
	Scheduled bool
}

func parseVersion(value string) ([3]int, error) {
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	var version [3]int
	if len(parts) != 3 {
		return version, fmt.Errorf("invalid release version %q", value)
	}
	for i, part := range parts {
		if part == "" || len(part) > 9 {
			return version, fmt.Errorf("invalid release version %q", value)
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return version, fmt.Errorf("invalid release version %q", value)
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return version, err
		}
		version[i] = n
	}
	return version, nil
}

func newerVersion(latest, current string) (bool, error) {
	newParts, err := parseVersion(latest)
	if err != nil {
		return false, err
	}
	oldParts, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	for i := range newParts {
		if newParts[i] != oldParts[i] {
			return newParts[i] > oldParts[i], nil
		}
	}
	return false, nil
}

func latestReleaseTag(ctx context.Context) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	response, err := updateHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup returned HTTP %d", response.StatusCode)
	}
	if !strings.Contains(response.Request.URL.Path, "/releases/tag/") {
		return "", errors.New("release lookup did not resolve to a tagged release")
	}
	tag := path.Base(response.Request.URL.Path)
	if _, err := parseVersion(tag); err != nil || !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("invalid latest release tag %q", tag)
	}
	return tag, nil
}

func updateAssetName(tag string) (string, error) {
	if _, err := parseVersion(tag); err != nil || !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("invalid update tag %q", tag)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("automatic updates do not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	switch runtime.GOOS {
	case "darwin", "linux":
		return fmt.Sprintf("verify_%s_%s_%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH), nil
	case "windows":
		return fmt.Sprintf("verify_%s_windows_%s.zip", tag, runtime.GOARCH), nil
	default:
		return "", fmt.Errorf("automatic updates do not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func downloadUpdateFile(ctx context.Context, address string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("update file exceeds size limit")
	}
	return data, nil
}

func expectedChecksum(data []byte, asset string) (string, error) {
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != asset {
			continue
		}
		if expected != "" {
			return "", errors.New("duplicate release checksum")
		}
		if len(fields[0]) != 64 {
			return "", errors.New("invalid release checksum")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", errors.New("invalid release checksum")
		}
		expected = strings.ToLower(fields[0])
	}
	if expected == "" {
		return "", errors.New("release checksum is missing")
	}
	return expected, nil
}

func extractTarBinary(data []byte) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name != "verify" {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, errors.New("release executable is not a regular file")
		}
		if header.Size < 1 || header.Size > maxUpdateBinary {
			return nil, errors.New("release executable has invalid size")
		}
		binary, err := io.ReadAll(io.LimitReader(reader, maxUpdateBinary+1))
		if err != nil {
			return nil, err
		}
		if int64(len(binary)) != header.Size {
			return nil, errors.New("release executable is incomplete")
		}
		return binary, nil
	}
	return nil, errors.New("release archive has no Verify executable")
}

func extractZipBinary(data []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, entry := range reader.File {
		if entry.Name != "verify.exe" {
			continue
		}
		if entry.FileInfo().IsDir() || entry.UncompressedSize64 < 1 || entry.UncompressedSize64 > maxUpdateBinary {
			return nil, errors.New("release executable has invalid size")
		}
		file, err := entry.Open()
		if err != nil {
			return nil, err
		}
		binary, readErr := io.ReadAll(io.LimitReader(file, maxUpdateBinary+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(binary)) != entry.UncompressedSize64 {
			return nil, errors.New("release executable is incomplete")
		}
		return binary, nil
	}
	return nil, errors.New("release archive has no Verify executable")
}

func stageUpdate(ctx context.Context, executable string, binary []byte, version string) (string, error) {
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("installed Verify executable is not a regular file")
	}
	pattern := ".verify-update-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe"
	}
	temp, err := os.CreateTemp(filepath.Dir(executable), pattern)
	if err != nil {
		return "", err
	}
	staged := temp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(staged)
		}
	}()
	if err = temp.Chmod(info.Mode().Perm() | 0500); err == nil {
		_, err = temp.Write(binary)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, staged, "--version")
	output, err := cmd.Output()
	if err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("downloaded executable failed its version check: %w", err)
	}
	if strings.TrimSpace(string(output)) != version {
		_ = os.Remove(staged)
		return "", fmt.Errorf("downloaded executable reports %q, expected %s", strings.TrimSpace(string(output)), version)
	}
	return staged, nil
}

func installRelease(ctx context.Context, tag string) (bool, error) {
	asset, err := updateAssetName(tag)
	if err != nil {
		return false, err
	}
	base := strings.TrimRight(releaseDownloadBase, "/") + "/" + tag + "/"
	checksums, err := downloadUpdateFile(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return false, fmt.Errorf("download release checksums: %w", err)
	}
	expected, err := expectedChecksum(checksums, asset)
	if err != nil {
		return false, err
	}
	archive, err := downloadUpdateFile(ctx, base+asset, maxUpdateArchive)
	if err != nil {
		return false, fmt.Errorf("download release archive: %w", err)
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return false, errors.New("release checksum mismatch; installed executable is unchanged")
	}
	var binary []byte
	if runtime.GOOS == "windows" {
		binary, err = extractZipBinary(archive)
	} else {
		binary, err = extractTarBinary(archive)
	}
	if err != nil {
		return false, err
	}
	executable, err := updateExecutable()
	if err != nil {
		return false, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return false, err
	}
	staged, err := stageUpdate(ctx, executable, binary, strings.TrimPrefix(tag, "v"))
	if err != nil {
		return false, err
	}
	scheduled, err := replaceExecutable(executable, staged)
	if err != nil {
		_ = os.Remove(staged)
		return false, err
	}
	return scheduled, nil
}

func checkAndInstallUpdate(ctx context.Context, manual bool) (updateResult, error) {
	latest, err := latestReleaseTag(ctx)
	if err != nil {
		if manual {
			return updateResult{}, err
		}
		return updateResult{}, nil // An offline update check must not prevent verification.
	}
	newer, err := newerVersion(latest, Version)
	if err != nil {
		return updateResult{}, err
	}
	result := updateResult{Version: latest}
	if !newer {
		return result, nil
	}
	result.Scheduled, err = installRelease(ctx, latest)
	if err != nil {
		return result, err
	}
	result.Updated = true
	return result, nil
}
