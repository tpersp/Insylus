package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	GitHubOwner       = "Insylus"
	GitHubRepo        = "Insylus"
	GitHubAPIURL      = "https://api.github.com"
	DownloadBaseURL   = "https://github.com/Insylus/Insylus/releases/download"
	InsylusBinaryName = "insylus-server"
	InsylusDir        = "/opt/insylus/dist"
)

// GitHubClient interacts with the GitHub API.
type GitHubClient struct {
	httpClient *http.Client
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchLatestRelease fetches the latest release from GitHub.
func (c *GitHubClient) FetchLatestRelease(ctx context.Context) (*GitHubRelease, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", GitHubAPIURL, GitHubOwner, GitHubRepo)

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := decodeJSON(resp.Body, &release); err != nil {
		return nil, fmt.Errorf("failed to decode release: %w", err)
	}

	return &release, nil
}

// GetDownloadURL returns the download URL for a specific version.
func (c *GitHubClient) GetDownloadURL(version string) string {
	return fmt.Sprintf("%s/%s/%s", DownloadBaseURL, version, InsylusBinaryName)
}

// GetChecksumURL returns the checksum file URL for a specific version.
func (c *GitHubClient) GetChecksumURL(version string) string {
	return fmt.Sprintf("%s/%s/%s-%s.sha256", DownloadBaseURL, version, InsylusBinaryName, version)
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
