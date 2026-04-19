package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestReleaseAssetURLsUsesReleaseAssets(t *testing.T) {
	binaryURL, checksumURL, err := ReleaseAssetURLs(&GitHubRelease{
		TagName: "v1.2.3",
		Assets: []ReleaseAsset{
			{Name: "notes.txt", BrowserDownloadURL: "https://example.invalid/notes.txt"},
			{Name: "insylus-server-v1.2.3.sha256", BrowserDownloadURL: "https://example.invalid/insylus-server-v1.2.3.sha256"},
			{Name: "insylus-server", BrowserDownloadURL: "https://example.invalid/insylus-server"},
		},
	})
	if err != nil {
		t.Fatalf("ReleaseAssetURLs: %v", err)
	}
	if binaryURL != "https://example.invalid/insylus-server" {
		t.Fatalf("binaryURL = %q", binaryURL)
	}
	if checksumURL != "https://example.invalid/insylus-server-v1.2.3.sha256" {
		t.Fatalf("checksumURL = %q", checksumURL)
	}
}

func TestReleaseAssetURLsRequiresBinaryAndChecksum(t *testing.T) {
	_, _, err := ReleaseAssetURLs(&GitHubRelease{
		TagName: "v1.2.3",
		Assets:  []ReleaseAsset{{Name: "insylus-server", BrowserDownloadURL: "https://example.invalid/insylus-server"}},
	})
	if !errors.Is(err, ErrMissingReleaseAssets) {
		t.Fatalf("ReleaseAssetURLs error = %v, want ErrMissingReleaseAssets", err)
	}
}
