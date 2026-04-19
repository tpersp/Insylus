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

func TestGitHubDownloadURLsUseConfiguredRepo(t *testing.T) {
	client := &GitHubClient{owner: "tpersp", repo: "Insylus"}

	if got, want := client.GetDownloadURL("v1.2.3"), "https://github.com/tpersp/Insylus/releases/download/v1.2.3/insylus-server"; got != want {
		t.Fatalf("GetDownloadURL = %q, want %q", got, want)
	}
	if got, want := client.GetChecksumURL("v1.2.3"), "https://github.com/tpersp/Insylus/releases/download/v1.2.3/insylus-server-v1.2.3.sha256"; got != want {
		t.Fatalf("GetChecksumURL = %q, want %q", got, want)
	}
}
