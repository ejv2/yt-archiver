package ytarchiver

import (
	"errors"
	"fmt"
	"regexp"

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

func (s SelectorRegex) Should(vid *youtube.Video) bool {
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
