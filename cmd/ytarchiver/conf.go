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

// configSelector-related stuff.
var (
	ErrInvalidRegexType = errors.New("regex selector: invalid match type (want 'title' or 'description')")
	regexMatchTypes     = map[string]int{"title": ytarchiver.SelectorRegexTitle,
		"description": ytarchiver.SelectorRegexDescription}
)

type configSelector struct {
	Regex struct {
		Type    string
		Pattern string
	}
	Playlist string
	Videos   []string
}

func (c configSelector) Selector() (ytarchiver.VideoSelector, error) {
	switch {
	case c.Regex.Pattern != "":
		t, ok := regexMatchTypes[c.Regex.Type]
		if !ok {
			return nil, ErrInvalidRegexType
		}
		return ytarchiver.NewSelectorRegex(t, c.Regex.Pattern)
	case c.Playlist != "":
		return &ytarchiver.PlaylistSelector{PlaylistID: c.Playlist}, nil
	case len(c.Videos) > 0:
		return ytarchiver.NewIDSelector(c.Videos), nil
	default:
		// Ignore empty.
		return nil, nil
	}
}

type Config struct {
	// Fields copied from ytarchiver config.
	Root     string `required:"true"`
	Channels []struct {
		ID       string
		Handle   string
		Username string

		Selectors []configSelector
	}
	APIKey          string `required:"true"`
	MaxParallel     uint
	Downloader      string
	MaxRetries      uint
	Selectors       []configSelector
	DumpVideoInfo   bool
	DumpChannelInfo bool

	// Interval between each refresh of the archives.
	Interval time.Duration
}

func (c Config) ArchiverConfig() (ytarchiver.Config, error) {
	cfg := ytarchiver.Config{
		Root:            c.Root,
		APIKey:          c.APIKey,
		MaxParallel:     c.MaxParallel,
		Downloader:      c.Downloader,
		MaxRetries:      c.MaxRetries,
		DumpVideoInfo:   c.DumpVideoInfo,
		DumpChannelInfo: c.DumpChannelInfo,
	}

	for _, c := range c.Channels {
		ch := ytarchiver.YouTubeChannel{
			ID:       c.ID,
			Handle:   c.Handle,
			Username: c.Username,
		}

		for _, s := range c.Selectors {
			conv, err := s.Selector()
			if err != nil {
				return cfg, err
			}

			ch.Selectors = append(ch.Selectors, conv)
		}

		cfg.Channels = append(cfg.Channels, ch)
	}

	for _, s := range c.Selectors {
		conv, err := s.Selector()
		if err != nil {
			return cfg, err
		}

		cfg.Selectors = append(cfg.Selectors, conv)
	}

	if err := ValidateConfig(c); err != nil {
		return cfg, err
	}

	// Insert the default MaxParallel if zero
	if cfg.MaxParallel == 0 {
		cfg.MaxParallel = ytarchiver.DefaultConfig("").MaxParallel
	}

	return cfg, nil
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
