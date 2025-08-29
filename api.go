package ytarchiver

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/api/youtube/v3"
)

var (
	ErrChannelNotIdentified = errors.New("no identifying information for channel")
	ErrEmptyResults         = errors.New("no results returned")
	ErrNoSuchChannel        = errors.New("channel not found")
)

func isHTTPError(status int) bool {
	return status < 200 || status >= 300
}

// YouTubeChannel is a struct containing one or more unique identifiers to
// select a channel. Only the most specific is used by the system.
//
// The priority system for identification is as follows:
//
//  1. Channel ID
//  2. Channel handle
//  3. YouTube username
//
// The highest specifier set will be used and the rest ignored.
//
// YouTubeChannel also contains a slice of VideoSelectors which will
// be applied in addition to the global video selectors configured in
// the root.
type YouTubeChannel struct {
	ID        string
	Handle    string
	Username  string
	Selectors []VideoSelector
}

func (c YouTubeChannel) String() string {
	return c.Identity()
}

func (c YouTubeChannel) Identity() string {
	switch {
	case c.ID != "":
		return c.ID
	case c.Handle != "":
		return c.Handle
	case c.Username != "":
		return c.Username
	default:
		return "unknown"
	}
}

func (c YouTubeChannel) requestAddIdentity(r *youtube.ChannelsListCall) error {
	switch {
	case c.ID != "":
		r.Id(c.ID)
	case c.Handle != "":
		r.ForHandle(c.Handle)
	case c.Username != "":
		r.ForUsername(c.Username)
	default:
		return ErrChannelNotIdentified
	}

	return nil
}

// newCachedChannel requests the API to build a cached channel.
func (c YouTubeChannel) getCachedChannel(srv *youtube.Service) (cachedChannel, error) {
	req := srv.Channels.List([]string{"id", "snippet", "contentDetails"})
	if err := c.requestAddIdentity(req); err != nil {
		return cachedChannel{}, fmt.Errorf("caching %s: %v", c.Identity(), err)
	}

	r, err := req.Do()
	if err != nil {
		return cachedChannel{}, fmt.Errorf("caching %s: list channel: %v", c.Identity(), err)
	}
	if isHTTPError(r.HTTPStatusCode) {
		return cachedChannel{}, fmt.Errorf("caching %s: list channel: http status %d", c.Identity(), r.HTTPStatusCode)
	}
	if len(r.Items) == 0 {
		return cachedChannel{}, fmt.Errorf("caching %s: list channel: %w", c.Identity(), ErrNoSuchChannel)
	}

	rs := r.Items[0]

	return cachedChannel{
		ID:        rs.Id,
		Name:      rs.Snippet.Title,
		UploadsID: rs.ContentDetails.RelatedPlaylists.Uploads,
		Videos:    nil,
	}, nil
}

// cachedChannel contains details of a channel pertinent to the operation
// of the archiver. We make this request once to preserve quota.
type cachedChannel struct {
	// Unique channel identifier
	ID string
	// Friendly name of the channel.
	Name string
	// ID of the uploads playlist.
	UploadsID string
	// Videos indicates if a given video ID has been seen yet.
	// This is initially nil and is then populated exactly once on the first archive run.
	Videos map[string]struct{}
}

func (c cachedChannel) String() string {
	return c.Name
}

// checkUpcoming returns a map containing any videos in the given set which are upcoming and - as a
// result - should not be considered for archiving.
// To conserve quota, the set contained within resp should be as large as permitted by the API (probably
// 50 max).
func (c *cachedChannel) checkUpcoming(resp *youtube.PlaylistItemListResponse, srv *youtube.Service) (map[string]struct{}, error) {
	ids := make([]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		ids = append(ids, it.ContentDetails.VideoId)
	}

	r, err := srv.Videos.List([]string{"snippet"}).Id(ids...).Do()
	if err != nil {
		return nil, fmt.Errorf("check upcoming: %v", err)
	}

	upcoming := make(map[string]struct{})
	for _, v := range r.Items {
		if v == nil {
			continue
		}

		if v.Snippet.LiveBroadcastContent != "none" && v.Snippet.LiveBroadcastContent != "completed" {
			upcoming[v.Id] = struct{}{}
		}
	}

	return upcoming, nil
}

func (c *cachedChannel) foreach(resp *youtube.PlaylistItemListResponse, srv *youtube.Service, cmd func(*cachedChannel, *youtube.PlaylistItem) error) error {
	if isHTTPError(resp.HTTPStatusCode) {
		return fmt.Errorf("foreach video on %s: http status %d", c.ID, resp.HTTPStatusCode)
	}
	if resp == nil || len(resp.Items) == 0 {
		return ErrEmptyResults
	}

	upcoming, err := c.checkUpcoming(resp, srv)
	if err != nil {
		return err
	}

	for _, v := range resp.Items {
		if v == nil {
			continue
		}
		// Video flagged as upcoming; skip it for now
		// NOTE: As we aren't running the callback here, we also aren't
		// marking this as present in the map so this check is re-done.
		if _, ok := upcoming[v.ContentDetails.VideoId]; ok {
			continue
		}

		if err := cmd(c, v); err != nil {
			return err
		}
	}

	return nil
}

// Foreach runs cmd on each video returned from a given channel.
// This does involve an API hit and is not just for each video in the Videos map.
// If the Videos map is nil, it is initialized and every video on the channel is visited.
// Else, only the first page of results is visited.
// If cmd returns an error, the foreach sequence halts (no more videos are visited).
func (c *cachedChannel) Foreach(ctx context.Context, srv *youtube.Service, cmd func(*cachedChannel, *youtube.PlaylistItem) error) error {
	rq := srv.PlaylistItems.List([]string{"contentDetails", "snippet"}).PlaylistId(c.UploadsID).MaxResults(50)
	if c.Videos == nil {
		n := 0
		err := rq.Pages(ctx, func(pilr *youtube.PlaylistItemListResponse) error {
			n++
			return c.foreach(pilr, srv, cmd)
		})

		if err != nil {
			return fmt.Errorf("foreach video on %s (page %d): %v", c.ID, n, err)
		}
	} else {
		r, err := rq.Do()
		if err != nil {
			return fmt.Errorf("foreach video on %s: request: %v", c.ID, err)
		}

		err = c.foreach(r, srv, cmd)
		if err != nil {
			return fmt.Errorf("foreach video on %s: %v", c.ID, err)
		}
	}

	return nil
}
