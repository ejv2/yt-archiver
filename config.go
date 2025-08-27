package ytarchiver

import (
	"runtime"
)

var defaultConfig = Config{
	Root:        ".",
	Channels:    []YouTubeChannel{{Handle: "GoogleDevelopers"}},
	APIKey:      "",
	MaxParallel: uint(runtime.GOMAXPROCS(0)),
	Downloader:  "/usr/bin/youtube-dl",
}

// Config contains the runtime configuration for the archiver system.
type Config struct {
	// Archive root.
	// Archived video files will be stored here.
	Root string
	// Channels configured for archive by the system.
	Channels []YouTubeChannel
	// API key for the YouTube public API.
	// Does not require OAuth2.
	// https://console.cloud.google.com/apis/credentials
	APIKey string
	// Maximum number of parallel downloader goroutines.
	MaxParallel uint
	// Path to a YouTube downloader executable.
	// Must be youtube-dl or a fork thereof.
	Downloader string
	// Selectors are critera which must be met in order for a
	// video to be archived.
	Selectors []VideoSelector
}

// DefaultConfig returns the default configuration with the given API key specified.
// This is a helper function. See the defaultConfig field for what the default configuration is.
func DefaultConfig(apiKey string) Config {
	cfg := defaultConfig
	cfg.APIKey = apiKey
	return cfg
}
