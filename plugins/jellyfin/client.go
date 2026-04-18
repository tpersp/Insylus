package jellyfin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JellyfinClient communicates with a Jellyfin server's API.
type JellyfinClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewJellyfinClient creates a new Jellyfin API client.
func NewJellyfinClient(apiURL, apiKey string, tlsInsecure bool) (*JellyfinClient, error) {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return nil, fmt.Errorf("api_url is required")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("api_url must include host")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsInsecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // User opt-in for self-signed certificates.
	}
	return &JellyfinClient{
		baseURL:    strings.TrimRight(parsed.String(), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

// GetSystemInfo returns public server information.
func (c *JellyfinClient) GetSystemInfo(ctx context.Context) (*JellyfinSystemInfo, error) {
	var info JellyfinSystemInfo
	if err := c.get(ctx, "/System/Info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetUser gets a user by name.
func (c *JellyfinClient) GetUser(ctx context.Context, username string) (*JellyfinUser, error) {
	var users []JellyfinUser
	if err := c.get(ctx, "/Users", &users); err != nil {
		return nil, err
	}
	for _, u := range users {
		if strings.EqualFold(u.Name, username) {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user %q not found", username)
}

// GetUserByID gets a user by ID.
func (c *JellyfinClient) GetUserByID(ctx context.Context, userID string) (*JellyfinUser, error) {
	var user JellyfinUser
	if err := c.get(ctx, "/Users/"+userID, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetLibraries returns top-level library collections.
func (c *JellyfinClient) GetLibraries(ctx context.Context) ([]JellyfinLibrary, error) {
	var items []JellyfinLibrary
	// Use Items endpoint with Recursive=false and search live libraries
	// Jellyfin returns top-level folders as items with Type "CollectionFolder"
	query := url.Values{}
	query.Set("includeItemTypes", "CollectionFolder")
	query.Set("Recursive", "false")
	var resp JellyfinItemsResponse
	if err := c.get(ctx, "/Items?"+query.Encode(), &resp); err != nil {
		return nil, err
	}
	for _, item := range resp.Items {
		if item.Type == "CollectionFolder" {
			items = append(items, JellyfinLibrary{
				ID:             item.ID,
				Name:           item.Name,
				ServerID:       item.ServerID,
				CollectionType: inferCollectionType(item),
				ImageTags:      item.ImageTags,
			})
		}
	}
	return items, nil
}

// GetItems returns items from a library (or the entire library if no parentID).
func (c *JellyfinClient) GetItems(ctx context.Context, parentID string, itemType string, userID string) ([]JellyfinItem, error) {
	query := ""
	if parentID != "" {
		query = "parentId=" + url.QueryEscape(parentID) + "&"
	}
	if itemType != "" {
		query += "IncludeItemTypes=" + url.QueryEscape(itemType) + "&"
	}
	query += "Recursive=true&Fields=MediaSources,UserData"
	basePath := "/Items"
	if userID != "" {
		basePath = "/Users/" + userID + "/Items"
	}
	var resp JellyfinItemsResponse
	if err := c.get(ctx, basePath+"?"+query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetItemsByType returns all items of a specific type from a library.
func (c *JellyfinClient) GetItemsByType(ctx context.Context, parentID, itemType, userID string) ([]JellyfinItem, error) {
	query := ""
	if parentID != "" {
		query = "parentId=" + url.QueryEscape(parentID) + "&"
	}
	query += "IncludeItemTypes=" + url.QueryEscape(itemType) + "&Recursive=true&Fields=MediaSources,UserData"
	basePath := "/Items"
	if userID != "" {
		basePath = "/Users/" + userID + "/Items"
	}
	var resp JellyfinItemsResponse
	if err := c.get(ctx, basePath+"?"+query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetSeriesWithEpisodes returns series with their episodes.
func (c *JellyfinClient) GetSeriesWithEpisodes(ctx context.Context, parentID, userID string) ([]JellyfinItem, error) {
	query := ""
	if parentID != "" {
		query = "parentId=" + url.QueryEscape(parentID) + "&"
	}
	query += "IncludeItemTypes=Series&Recursive=true&Fields=MediaSources,UserData,Episodes"
	basePath := "/Items"
	if userID != "" {
		basePath = "/Users/" + userID + "/Items"
	}
	var resp JellyfinItemsResponse
	if err := c.get(ctx, basePath+"?"+query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetItem returns a single item by ID.
func (c *JellyfinClient) GetItem(ctx context.Context, itemID string) (*JellyfinItem, error) {
	var item JellyfinItem
	if err := c.get(ctx, "/Items/"+itemID, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetItemWithUserData returns an item with user-specific playback data.
func (c *JellyfinClient) GetItemWithUserData(ctx context.Context, itemID, userID string) (*JellyfinItem, error) {
	query := "Fields=MediaSources,UserData"
	basePath := "/Items/" + itemID
	if userID != "" {
		basePath = "/Users/" + userID + "/Items/" + itemID
	}
	var item JellyfinItem
	if err := c.get(ctx, basePath+"?"+query, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetResumeItems returns items with playback progress for a user.
func (c *JellyfinClient) GetResumeItems(ctx context.Context, userID string, itemType string) ([]JellyfinItem, error) {
	query := "Fields=MediaSources,UserData,SeriesName&Limit=50"
	if itemType != "" {
		query += "&IncludeItemTypes=" + url.QueryEscape(itemType)
	}
	var resp JellyfinItemsResponse
	if err := c.get(ctx, "/Users/"+userID+"/Items/Resume?"+query, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetLatestItems returns the most recently added items.
func (c *JellyfinClient) GetLatestItems(ctx context.Context, userID string, limit int) ([]JellyfinItem, error) {
	query := "limit=" + strconv.Itoa(limit) + "&Fields=MediaSources,UserData&sortOrder=Descending"
	var items []JellyfinItem
	if err := c.get(ctx, "/Users/"+userID+"/Items/Latest?"+query, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *JellyfinClient) get(ctx context.Context, endpoint string, out any) error {
	reqURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Emby-Token", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jellyfin request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("jellyfin API returned %s: %s", resp.Status, msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func inferCollectionType(item JellyfinItem) string {
	// Jellyfin's CollectionFolder doesn't always expose CollectionType directly
	// We infer from the name or assume common defaults
	name := strings.ToLower(item.Name)
	if strings.Contains(name, "movie") || strings.Contains(name, "film") {
		return "movies"
	}
	if strings.Contains(name, "series") || strings.Contains(name, "tv") || strings.Contains(name, "anime") {
		return "tvshows"
	}
	if strings.Contains(name, "music") || strings.Contains(name, "audio") {
		return "music"
	}
	return "unknown"
}

// FormatRuntime formats RunTimeTicks into a readable string (e.g., "1h 42m").
func FormatRuntime(ticks int64) string {
	if ticks <= 0 {
		return "-"
	}
	// Jellyfin uses 100-nanosecond ticks
	seconds := ticks / 10_000_000
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// FormatProgress formats playback position ticks into a readable progress string.
func FormatProgress(positionTicks, durationTicks int64) string {
	if durationTicks <= 0 {
		return "-"
	}
	posSeconds := positionTicks / 10_000_000
	durSeconds := durationTicks / 10_000_000
	if durSeconds == 0 {
		return "-"
	}
	percentage := float64(posSeconds) / float64(durSeconds) * 100
	posStr := FormatRuntime(positionTicks)
	durStr := FormatRuntime(durationTicks)
	return fmt.Sprintf("%s / %s (%.0f%%)", posStr, durStr, percentage)
}

// ItemTypeDisplay returns a friendly name for the item type.
func ItemTypeDisplay(itemType string) string {
	switch itemType {
	case "Movie":
		return "Movie"
	case "Series":
		return "Series"
	case "Episode":
		return "Episode"
	case "Season":
		return "Season"
	case "BoxSet":
		return "Box Set"
	case "MusicAlbum":
		return "Album"
	case "MusicArtist":
		return "Artist"
	case "MusicVideo":
		return "Music Video"
	case "Audio":
		return "Audio"
	case "Playlist":
		return "Playlist"
	case "PhotoAlbum":
		return "Photo Album"
	case "Photo":
		return "Photo"
	case "Video":
		return "Video"
	default:
		return itemType
	}
}
