package main

import (
	"errors"
	"time"

	"github.com/cristalhq/aconfig"
	ytarchiver "github.com/ejv2/yt-archiver"
)

var configSearchPaths = []string{
	"./ytarchive.json",
	"/etc/ytarchive.json",
	"/usr/share/ytarchive/ytarchive.json",
}

var (
	ErrIntervalTooShort = errors.New("interval must be at least 30s")
	ErrBlankAPIKey      = errors.New("blank API key supplied: an API key is required: go to https://console.cloud.google.com")
)

type Config struct {
	ytarchiver.Config
	// Interval between each refresh of the archives.
	Interval time.Duration
}

func NewConfig() (Config, error) {
	cfg := Config{}
	loader := aconfig.LoaderFor(&cfg, aconfig.Config{
		SkipDefaults: true,
		FileFlag:     "config",
		Files:        configSearchPaths,
	})

	err := loader.Load()
	return cfg, err
}

func ValidateConfig(cfg Config) error {
	// Prevents spamming the YouTube API.
	if cfg.Interval.Seconds() < 30 {
		return ErrIntervalTooShort
	}

	// Try to save people who didn't read the manual.
	if cfg.APIKey == "" || cfg.APIKey == "YOUR_KEY_HERE" {
		return ErrBlankAPIKey
	}

	return nil
}
