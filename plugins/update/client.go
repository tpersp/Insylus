package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	stdruntime "runtime"
	"strings"
	"time"
)

const (
	DefaultGitHubOwner  = "tpersp"
	DefaultGitHubRepo   = "Insylus"
	DefaultGitHubAPIURL = "https://api.github.com"
	InsylusBinaryName   = "insylus-server"
	DefaultBinaryPath   = "/opt/insylus/bin/insylus-server"
	DefaultHelperPath   = "/opt/insylus/bin/insylus-apply-server-update"
	DefaultStagingDir   = "/var/lib/insylus/server-updates"
)

var ErrNoLatestRelease = errors.New("no published GitHub release found")
var ErrMissingReleaseAssets = errors.New("server update package is not available")

// GitHubClient interacts with the GitHub API.
type GitHubClient struct {
	httpClient *http.Client
	apiURL     string
	owner      string
	repo       string
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: strings.TrimRight(envDefault("INSYLUS_UPDATE_GITHUB_API_URL", DefaultGitHubAPIURL), "/"),
		owner:  envDefault("INSYLUS_UPDATE_GITHUB_OWNER", DefaultGitHubOwner),
		repo:   envDefault("INSYLUS_UPDATE_GITHUB_REPO", DefaultGitHubRepo),
	}
}

// FetchLatestRelease fetches the latest release from GitHub.
func (c *GitHubClient) FetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.apiURL, c.owner, c.repo)
	return c.fetchRelease(ctx, apiURL)
}

// FetchReleaseByTag fetches a GitHub release by tag.
func (c *GitHubClient) FetchReleaseByTag(ctx context.Context, tag string) (*GitHubRelease, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.apiURL, c.owner, c.repo, tag)
	return c.fetchRelease(ctx, apiURL)
}

func (c *GitHubClient) fetchRelease(ctx context.Context, apiURL string) (*GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoLatestRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := decodeJSON(resp.Body, &release); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	return &release, nil
}

// ReleaseAssetURLs returns the server binary and checksum URLs from release assets.
func ReleaseAssetURLs(release *GitHubRelease) (string, string, error) {
	if release == nil {
		return "", "", ErrMissingReleaseAssets
	}
	assetNames := serverAssetNameCandidates(stdruntime.GOOS, stdruntime.GOARCH)
	var binaryURL string
	var checksumURL string
	for _, assetName := range assetNames {
		checksumName := fmt.Sprintf("%s-%s.sha256", assetName, release.TagName)
		binaryURL = ""
		checksumURL = ""
		for _, asset := range release.Assets {
			switch asset.Name {
			case assetName:
				binaryURL = strings.TrimSpace(asset.BrowserDownloadURL)
			case checksumName:
				checksumURL = strings.TrimSpace(asset.BrowserDownloadURL)
			}
		}
		if binaryURL != "" && checksumURL != "" {
			return binaryURL, checksumURL, nil
		}
	}
	return "", "", ErrMissingReleaseAssets
}

func serverAssetNameCandidates(goos, goarch string) []string {
	names := []string{fmt.Sprintf("%s-%s-%s", InsylusBinaryName, goos, goarch)}
	if goos == "linux" && goarch == "arm" {
		names = append([]string{InsylusBinaryName + "-linux-armv7"}, names...)
	}
	return append(names, InsylusBinaryName)
}

// DownloadFile downloads a file and returns its content.
func (c *GitHubClient) DownloadFile(ctx context.Context, fileURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// VerifyChecksum verifies that the downloaded file matches the expected checksum.
func (c *GitHubClient) VerifyChecksum(data []byte, expectedChecksum string) error {
	hash := sha256.Sum256(data)
	actualChecksum := hex.EncodeToString(hash[:])

	// Parse the checksum file - format is "checksum  filename"
	expectedParts := strings.Split(expectedChecksum, " ")
	if len(expectedParts) >= 1 {
		// The checksum file might have the format "checksum  filename" or just "checksum"
		checksumFromFile := strings.TrimSpace(expectedParts[0])
		if checksumFromFile == actualChecksum {
			return nil
		}
	}

	if actualChecksum != strings.TrimSpace(expectedChecksum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", strings.TrimSpace(expectedChecksum), actualChecksum)
	}
	return nil
}

// ParseReleaseNotes parses GitHub release body into a cleaner format.
func ParseReleaseNotes(body string) string {
	// The body is already markdown from GitHub, so we just return it as-is
	// The UI will render it properly
	return body
}

// decodeJSON decodes JSON from an io.Reader.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// ExtractVersionFromTag extracts the version string from a GitHub tag (removes 'v' prefix).
func ExtractVersionFromTag(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
