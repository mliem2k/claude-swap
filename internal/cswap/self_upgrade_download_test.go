package cswap

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestReleaseAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "cswap_darwin_arm64.tar.gz"},
		{"darwin", "amd64", "cswap_darwin_amd64.tar.gz"},
		{"linux", "amd64", "cswap_linux_amd64.tar.gz"},
		{"linux", "arm64", "cswap_linux_arm64.tar.gz"},
		{"windows", "amd64", "cswap_windows_amd64.zip"},
	}
	for _, tt := range tests {
		if got := releaseAssetName(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("releaseAssetName(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestReleaseAssetURL(t *testing.T) {
	want := "https://github.com/mliem2k/claude-swap/releases/latest/download/cswap_linux_amd64.tar.gz"
	if got := releaseAssetURL("cswap_linux_amd64.tar.gz"); got != want {
		t.Errorf("releaseAssetURL(...) = %q, want %q", got, want)
	}
}

func TestDownloadReleaseAsset(t *testing.T) {
	body := []byte("fake archive bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	path, err := downloadReleaseAsset(srv.URL)
	if err != nil {
		t.Fatalf("downloadReleaseAsset: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
}

func TestDownloadReleaseAssetHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := downloadReleaseAsset(srv.URL); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func makeTestTarGz(t *testing.T, entryName string, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return f.Name()
}

func makeTestZip(t *testing.T, entryName string, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return f.Name()
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	content := []byte("fake binary content")
	archivePath := makeTestTarGz(t, "cswap", content)

	binPath, err := extractBinaryFromArchive(archivePath, "cswap_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("extractBinaryFromArchive: %v", err)
	}
	defer os.Remove(binPath)

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("extracted binary is not executable: mode %v", info.Mode())
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	content := []byte("fake windows binary content")
	archivePath := makeTestZip(t, "cswap.exe", content)

	binPath, err := extractBinaryFromArchive(archivePath, "cswap_windows_amd64.zip")
	if err != nil {
		t.Fatalf("extractBinaryFromArchive: %v", err)
	}
	defer os.Remove(binPath)

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromArchiveMissingEntry(t *testing.T) {
	archivePath := makeTestTarGz(t, "some-other-file", []byte("irrelevant"))
	if _, err := extractBinaryFromArchive(archivePath, "cswap_linux_amd64.tar.gz"); err == nil {
		t.Fatal("expected an error when the archive has no cswap entry")
	}
}

var _ = io.Discard // keep io imported if unused by a given edit pass
