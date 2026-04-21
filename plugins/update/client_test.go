package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchLatestReleaseReturnsNoLatestReleaseForGitHub404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/tpersp/Insylus/releases/latest" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &GitHubClient{
		httpClient: server.Client(),
		apiURL:     server.URL,
		owner:      "tpersp",
		repo:       "Insylus",
	}

	_, err := client.FetchLatestRelease(context.Background())
	if !errors.Is(err, ErrNoLatestRelease) {
		t.Fatalf("FetchLatestRelease error = %v, want ErrNoLatestRelease", err)
	}
}

func TestFetchReleaseByTagUsesConfiguredRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/tpersp/Insylus/releases/tags/v1.2.3" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer server.Close()

	client := &GitHubClient{
		httpClient: server.Client(),
		apiURL:     server.URL,
		owner:      "tpersp",
		repo:       "Insylus",
	}

	release, err := client.FetchReleaseByTag(context.Background(), "v1.2.3")
	if err != nil {
		t.Fatalf("FetchReleaseByTag: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("TagName = %q, want v1.2.3", release.TagName)
	}
}

func TestReleasePackageAssetsUsesBundleAssets(t *testing.T) {
	pkg, err := ReleasePackageAssets(&GitHubRelease{
		TagName: "v1.2.3",
		Assets: []ReleaseAsset{
			{Name: "notes.txt", BrowserDownloadURL: "https://example.invalid/notes.txt"},
			{Name: "insylus-update-linux-amd64.tar.gz-v1.2.3.sha256", BrowserDownloadURL: "https://example.invalid/insylus-update-linux-amd64.tar.gz-v1.2.3.sha256"},
			{Name: "insylus-update-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.invalid/insylus-update-linux-amd64.tar.gz"},
		},
	})
	if err != nil {
		t.Fatalf("ReleasePackageAssets: %v", err)
	}
	if pkg.DownloadURL != "https://example.invalid/insylus-update-linux-amd64.tar.gz" {
		t.Fatalf("DownloadURL = %q", pkg.DownloadURL)
	}
	if pkg.ChecksumURL != "https://example.invalid/insylus-update-linux-amd64.tar.gz-v1.2.3.sha256" {
		t.Fatalf("ChecksumURL = %q", pkg.ChecksumURL)
	}
}

func TestUpdateBundleAssetNameCandidatesPreferPlatformAsset(t *testing.T) {
	got := updateBundleAssetNameCandidates("linux", "amd64")
	want := []string{"insylus-update-linux-amd64.tar.gz"}
	if len(got) != len(want) {
		t.Fatalf("updateBundleAssetNameCandidates len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("updateBundleAssetNameCandidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUpdateBundleAssetNameCandidatesPreferArmV7Asset(t *testing.T) {
	got := updateBundleAssetNameCandidates("linux", "arm")
	want := []string{"insylus-update-linux-armv7.tar.gz", "insylus-update-linux-arm.tar.gz"}
	if len(got) != len(want) {
		t.Fatalf("updateBundleAssetNameCandidates len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("updateBundleAssetNameCandidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReleasePackageAssetsRequiresBundleAndChecksum(t *testing.T) {
	_, err := ReleasePackageAssets(&GitHubRelease{
		TagName: "v1.2.3",
		Assets:  []ReleaseAsset{{Name: "insylus-update-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.invalid/insylus-update-linux-amd64.tar.gz"}},
	})
	if !errors.Is(err, ErrMissingReleaseAssets) {
		t.Fatalf("ReleasePackageAssets error = %v, want ErrMissingReleaseAssets", err)
	}
}

func TestExtractTarGzExtractsBundleFiles(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "insylus-server", Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("Close gzip: %v", err)
	}

	dst := t.TempDir()
	if err := extractTarGz(buf.Bytes(), dst); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "insylus-server"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("bundle content = %q, want hello", string(got))
	}
}

func TestExtractTarGzAcceptsRootDotDirectoryEntry(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("WriteHeader root dir: %v", err)
	}
	payload := []byte("ok")
	if err := tw.WriteHeader(&tar.Header{Name: "./insylus-server", Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatalf("WriteHeader file: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("Write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("Close gzip: %v", err)
	}

	dst := t.TempDir()
	if err := extractTarGz(buf.Bytes(), dst); err != nil {
		t.Fatalf("extractTarGz with root ./ entry: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "insylus-server"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("bundle content = %q, want ok", string(got))
	}
}
