package update

import "time"

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Prerelease  bool           `json:"prerelease"`
	PublishedAt string         `json:"published_at"`
	Assets      []ReleaseAsset `json:"assets"`
}

// ReleaseAsset represents a release asset.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateInfo is the response for update status.
type UpdateInfo struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version"`
	ReleaseNotes    string    `json:"release_notes"`
	PublishedAt     time.Time `json:"published_at"`
	DownloadURL     string    `json:"download_url"`
	ChecksumURL     string    `json:"checksum_url"`
	UpdateAvailable bool      `json:"update_available"`
}

// UpdateStatus represents the status of an update.
type UpdateStatus struct {
	ID         int64  `json:"id"`
	Version    string `json:"version"`
	ReleasedAt string `json:"released_at"`
	Status     string `json:"status"`
	Notes      string `json:"notes,omitempty"`
	AppliedAt  string `json:"applied_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// UpdateCheckResponse is the response for the check API.
type UpdateCheckResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	Message         string `json:"message,omitempty"`
	ReleaseNotes    string `json:"release_notes,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	ChecksumURL     string `json:"checksum_url,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	SkippedVersion  string `json:"skipped_version,omitempty"`
}

// ApplyUpdateResponse is the response for the apply API.
type ApplyUpdateResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
