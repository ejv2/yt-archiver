package ytarchiver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"google.golang.org/api/youtube/v3"
)

// ErrRegexInvalidPattern is the only regex matcher error.
var ErrRegexInvalidPattern = errors.New("invalid regex pattern")

// Regex matcher fields.
// The field which is specified via this constant is matched against in the regex.
const (
	SelectorRegexTitle = iota
	SelectorRegexDescription
)

// playlistStaleTimeout is the time after which the contents of a playlist will be
// invalidated and re-requested.
const playlistStaleTimeout = 24 * time.Hour

// A VideoSelector is a criterion for deciding if a given video should be downloaded.
// All specified criteria must be matched before a given video will be archived.
type VideoSelector interface {
	// Should indicates if a given matcher selects positively for this video.
	// All matchers must return true for a given video the be selected.
	// The selector is also passed the live YouTube API service connection,
	// should it wish to do further investigation.
	Should(*youtube.PlaylistItem, *youtube.Service) bool
}

// SelectorRegex matches any videos for which the title
type SelectorRegex struct {
	Match int
	patt  *regexp.Regexp
}

// NewSelectorRegex constructs a SelectorRegex by compiling the given regex source.
func NewSelectorRegex(match int, regex string) (SelectorRegex, error) {
	rp, err := regexp.Compile(regex)
	if err != nil {
		return SelectorRegex{}, fmt.Errorf("new selector regex: %w: %v", ErrRegexInvalidPattern, err)
	}

	return SelectorRegex{match, rp}, nil
}

func (s SelectorRegex) Should(vid *youtube.PlaylistItem, _ *youtube.Service) bool {
	toMatch := ""
	switch s.Match {
	case SelectorRegexTitle:
		toMatch = vid.Snippet.Title
	case SelectorRegexDescription:
		toMatch = vid.Snippet.Description
	default:
		panic("selector regex: invalid match value")
	}

	return s.patt.MatchString(toMatch)
}

// PlaylistSelector will select only for videos which are a
// member of a playlist identified via the given ID.
//
// The selector does some internal bookkeeping to ensure that
// we only hit the API once to request the playlist.
type PlaylistSelector struct {
	PlaylistID string

	listLoaded *time.Time
	list       map[string]struct{}
}

func (p *PlaylistSelector) loadPlaylist(s *youtube.Service) error {
	// empty/initialize the contents map
	p.list = make(map[string]struct{})

	rq := s.PlaylistItems.List([]string{"contentDetails"}).PlaylistId(p.PlaylistID).MaxResults(50)
	rq.Pages(context.Background(), func(r *youtube.PlaylistItemListResponse) error {
		for _, i := range r.Items {
			if i == nil || i.ContentDetails == nil {
				continue
			}

			p.list[i.ContentDetails.VideoId] = struct{}{}
		}
		return nil
	})

	_, err := rq.Do()
	return err
}

// we need to load if:
//   - the playlist hasn't been loaded yet
//   - the cache map got deleted (somehow?)
//   - the playlist is stale
func (p *PlaylistSelector) needLoad() bool {
	return p.listLoaded == nil || p.list == nil || time.Since(*p.listLoaded) > playlistStaleTimeout
}

func (p *PlaylistSelector) Should(vid *youtube.PlaylistItem, s *youtube.Service) bool {
	// If we haven't retrieved the list yet, do it now
	if p.needLoad() {
		if p.loadPlaylist(s) != nil {
			return false
		}
		now := time.Now()
		p.listLoaded = &now
	}

	_, ok := p.list[vid.ContentDetails.VideoId]

	return ok
}

// IDSelector only selects videos with a set of specified IDs.
type IDSelector struct {
	IDs      []string
	matchmap map[string]struct{}
}

func NewIDSelector(ids []string) IDSelector {
	sel := IDSelector{ids, make(map[string]struct{})}
	for _, id := range ids {
		sel.matchmap[id] = struct{}{}
	}

	return sel
}

func (i IDSelector) Should(vid *youtube.PlaylistItem, s *youtube.Service) bool {
	if vid == nil || vid.ContentDetails == nil {
		return false
	}

	_, ok := i.matchmap[vid.ContentDetails.VideoId]
	return ok
}
