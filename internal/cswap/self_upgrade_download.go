package cswap

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// releaseAssetName mirrors the naming convention every cswap release
// publishes: cswap_<goos>_<goarch>.tar.gz for darwin/linux, .zip for
// windows (a running .exe can't be overwritten in place, only replaced
// by moving a new file into position, and Windows ZIP is the more common
// convention there regardless).
func releaseAssetName(goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("cswap_%s_%s.%s", goos, goarch, ext)
}

// releaseAssetURL mirrors the stable, version-free download URL every
// release publishes alongside its versioned archive, so this always
// resolves to whatever is currently latest.
func releaseAssetURL(assetName string) string {
	return "https://github.com/mliem2k/claude-swap/releases/latest/download/" + assetName
}

// downloadReleaseAsset fetches url and writes its body to a fresh temp
// file, returning the path. The caller owns cleanup (os.Remove).
func downloadReleaseAsset(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "claude-swap/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: http %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "cswap-upgrade-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// extractBinaryFromArchive pulls the cswap/cswap.exe entry out of
// archivePath (a .tar.gz or .zip, chosen by assetName's extension) into
// a fresh, 0o755 temp file, returning its path. The caller owns cleanup.
func extractBinaryFromArchive(archivePath, assetName string) (string, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(archivePath)
	}
	return extractFromTarGz(archivePath)
}

func extractFromTarGz(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("archive %s has no cswap entry", archivePath)
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) != "cswap" {
			continue
		}
		return writeExtractedBinary(tr)
	}
}

func extractFromZip(archivePath string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, entry := range zr.File {
		if filepath.Base(entry.Name) != "cswap.exe" {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return writeExtractedBinary(rc)
	}
	return "", fmt.Errorf("archive %s has no cswap.exe entry", archivePath)
}

func writeExtractedBinary(r io.Reader) (string, error) {
	out, err := os.CreateTemp("", "cswap-new-*")
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, r); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	if err := out.Chmod(0o755); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}
