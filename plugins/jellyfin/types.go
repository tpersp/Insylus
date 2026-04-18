package jellyfin

import (
	"time"
)

// jellyfinToken stores the API token configuration for a device.
type jellyfinToken struct {
	DeviceID        string
	ServerName      string
	APIURL          string
	APIKey          string
	DefaultUserID   string
	DefaultUsername string
	TLSInsecure     bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeviceName      string
	DeviceHost      string
	DeviceOnline    bool
}

// jellyfinTokenSummary is a lightweight view of a configured Jellyfin server.
type jellyfinTokenSummary struct {
	DeviceID        string    `json:"device_id"`
	DeviceName      string    `json:"device_name"`
	Hostname        string    `json:"hostname,omitempty"`
	ServerName      string    `json:"server_name"`
	APIURL          string    `json:"api_url,omitempty"`
	DefaultUserID   string    `json:"default_user_id,omitempty"`
	DefaultUsername string    `json:"default_username,omitempty"`
	HasToken        bool      `json:"has_token"`
	TLSInsecure     bool      `json:"tls_insecure,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

// JellyfinItem represents a media item from the Jellyfin library.
type JellyfinItem struct {
	ID                string    `json:"Id"`
	Name              string    `json:"Name"`
	Type              string    `json:"Type"` // Movie, Series, Episode, etc.
	ServerID          string    `json:"ServerId"`
	Container         string    `json:"Container,omitempty"`
	PremiereDate      time.Time `json:"PremiereDate,omitempty"`
	ParentId          string    `json:"ParentId,omitempty"`
	GrandParentId     string    `json:"GrandParentId,omitempty"`
	Overview          string    `json:"Overview,omitempty"`
	ImageTags         map[string]string `json:"ImageTags,omitempty"`
	BackdropImageTags []string  `json:"BackdropImageTags,omitempty"`

	// Media sources
	MediaSources []MediaSource `json:"MediaSources,omitempty"`

	// User-specific data (populated when querying with user ID)
	UserData *UserData `json:"UserData,omitempty"`

	// Series-specific fields
	SeriesName         string `json:"SeriesName,omitempty"`
	EpisodeNumber      int    `json:"EpisodeNumber,omitempty"`
	SeasonNumber       int    `json:"SeasonNumber,omitempty"`
	SpecialEpisodeNumber int  `json:"SpecialEpisodeNumber,omitempty"`

	// Counts
	ChildCount     int `json:"ChildCount,omitempty"`
	RecursiveCount int `json:"RecursiveCount,omitempty"`

	// Runtime
	RunTimeTicks          int64 `json:"RunTimeTicks,omitempty"`
	OriginalRunTimeTicks  int64 `json:"OriginalRunTimeTicks,omitempty"`
}

// MediaSource describes a media stream source.
type MediaSource struct {
	ID               string `json:"Id"`
	Name             string `json:"Name"`
	Type             string `json:"Type"`
	Container        string `json:"Container,omitempty"`
	Path             string `json:"Path,omitempty"`
	Protocol         string `json:"Protocol,omitempty"`
	Medium           string `json:"Medium,omitempty"`
	Bitrate          int    `json:"Bitrate,omitempty"`
	Width            int    `json:"Width,omitempty"`
	Height           int    `json:"Height,omitempty"`
	Resolution       string `json:"Resolution,omitempty"`
	AspectRatio      string `json:"AspectRatio,omitempty"`
	AudioCodec       string `json:"AudioCodec,omitempty"`
	VideoCodec       string `json:"VideoCodec,omitempty"`
	VideoTimestamp   string `json:"VideoTimestamp,omitempty"`
	PacketLength     string `json:"PacketLength,omitempty"`
	Timestamp        string `json:"Timestamp,omitempty"`
	VideoProfile     string `json:"VideoProfile,omitempty"`
	VideoLevel       string `json:"VideoLevel,omitempty"`
	VideoBitrate     int    `json:"VideoBitrate,omitempty"`
	AudioBitrate     int    `json:"AudioBitrate,omitempty"`
	AudioChannels    int    `json:"AudioChannels,omitempty"`
	AudioSampleRate   int    `json:"AudioSampleRate,omitempty"`
	AudioAccuracy    string `json:"AudioAccuracy,omitempty"`
	VideoAccuracy    string `json:"VideoAccuracy,omitempty"`
	HeuristicLevel   string `json:"HeuristicLevel,omitempty"`
	Channels         []any  `json:"Channels,omitempty"`
}

// UserData contains playback progress and played status.
type UserData struct {
	Played               bool   `json:"Played"`
	PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
	PlayCount            int    `json:"PlayCount"`
	IsFavorite           bool   `json:"IsFavorite"`
	Likes                bool   `json:"Likes,omitempty"`
	LastPlayedDate        string `json:"LastPlayedDate,omitempty"`
}

// JellyfinItemsResponse is the API response for Items endpoint.
type JellyfinItemsResponse struct {
	Items []JellyfinItem `json:"Items"`
	TotalRecordCount int `json:"TotalRecordCount"`
	StartIndex       int `json:"StartIndex"`
}

// JellyfinLibrary represents a top-level library (parent) item.
type JellyfinLibrary struct {
	ID          string `json:"Id"`
	Name        string `json:"Name"`
	ServerID    string `json:"ServerId"`
	CollectionType string `json:"CollectionType"` // movies, tvshows, music, etc.
	ImageTags   map[string]string `json:"ImageTags,omitempty"`
}

// JellyfinSystemInfo contains server public information.
type JellyfinSystemInfo struct {
	ID          string `json:"Id"`
	ServerName  string `json:"ServerName"`
	Version     string `json:"Version"`
	OperatingSystem string `json:"OperatingSystem"`
}

// JellyfinUser represents a Jellyfin user.
type JellyfinUser struct {
	ID          string `json:"Id"`
	Name        string `json:"Name"`
}

// LibraryItems is a list of items with their library context.
type LibraryItems struct {
	Library jellyfinTokenSummary `json:"library"`
	Items   []JellyfinItem      `json:"items"`
}

// ItemProgress shows playback progress for an item.
type ItemProgress struct {
	ItemID       string  `json:"item_id"`
	Name         string  `json:"name"`
	SeriesName   string  `json:"series_name,omitempty"`
	Type         string  `json:"type"`
	Watched      bool    `json:"watched"`
	Progress     float64 `json:"progress"` // percentage 0-100
	Position     string  `json:"position"` // formatted position
	Duration     string  `json:"duration"` // formatted duration
	RunTimeTicks int64   `json:"run_time_ticks"`
}
