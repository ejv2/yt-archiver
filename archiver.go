// Package ytarchiver is an embeddable system for downloading a particular
// YouTube channel for archivist purposes.
//
// The package automatically manages a specified directory, structuring the
// archive in a convenient format for both human perusal and machine reading.
package ytarchiver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// Archiver process errors.
var (
	ErrAPIKey      = errors.New("ytarchiver: api key")
	ErrAPIConnect  = errors.New("ytarchiver: api")
	ErrDownloader  = errors.New("ytarchiver: downloader")
	ErrDownloadDir = errors.New("ytarchiver: bad download directory")
	ErrCacheBuild  = errors.New("ytarchiver: build channel cache")

	ErrCacheMiss = errors.New("ytarchiver archive: channel not in cache")

	ErrVideo = errors.New("ytarchiver: archive video")
)

// videoError is an error caused during the archiving of a given video.
type videoError struct {
	VideoID string
	Cause   error
}

func (v videoError) Error() string {
	return fmt.Sprintf("%s %s: %s", ErrVideo.Error(), v.VideoID, v.Cause.Error())
}

func (v videoError) Unwrap() error {
	return ErrVideo
}

type channelError struct {
	ChannelID string
	Errors    []error
}

func (c channelError) Error() string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "\tchannel %s: %d archiving errors:\n", c.ChannelID, len(c.Errors))

	for _, e := range c.Errors {
		fmt.Fprintf(sb, "\t\t- %s\n", e.Error())
	}

	return sb.String()
}

func (c *channelError) Add(e error) {
	c.Errors = append(c.Errors, e)
}

func (c *channelError) Nil() bool {
	return len(c.Errors) == 0
}

// ArchiveError is the error type returned from Archiver.Archive.
// It contains the list of errors caused by each channel respectively.
//
// Archiving does not stop on first error. Errors are collected after
// all channels have completed indicating if they have completed without
// error. The failure of one channel to archive does not prevent all others.
type ArchiveError []channelError

func (a ArchiveError) Error() string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "archiver: %d channel errors during archiving:\n", len(a))
	for _, e := range a {
		fmt.Fprint(sb, e.Error())
	}

	return sb.String()
}

// archiveMultiplexer is responsible for maintaining the pack of goroutines which are
// downloading videos for archive.
type archiveMultiplexer struct {
	ctx      context.Context
	cfg      Config
	workChan chan *youtube.PlaylistItem
	errChan  chan []error
}

func (mp archiveMultiplexer) worker() {
	errs := make([]error, 0)
	defer func() {
		mp.errChan <- errs
	}()

	for pi := range mp.workChan {
		outPath := filepath.Join(mp.cfg.Root, pi.Snippet.ChannelId, pi.ContentDetails.VideoId)
		err := youtubeDownload(mp.cfg, pi.ContentDetails.VideoId, outPath)
		if err != nil {
			errs = append(errs, err)
		}

		select {
		case <-mp.ctx.Done():
			return
		default:
		}
	}
}

// Wait awaits the termination of any ongoing jobs and quits the process.
// This *must* be called after the context has been cancelled and before
// discarding the multiplexer, else processes and goroutines will be leaked.
func (mp archiveMultiplexer) Wait() []error {
	errs := make([]error, 0, mp.cfg.MaxParallel)
	for i := uint(0); i < mp.cfg.MaxParallel; i++ {
		err := <-mp.errChan
		if err != nil {
			errs = append(errs, err...)
		}
	}

	return errs
}

// Done indicates to the workers that no more work is coming and that they must exit
// as soon as existing jobs are complete.
//
// Calling Done more than once will panic.
func (mp archiveMultiplexer) Done() {
	close(mp.workChan)
}

func (mp archiveMultiplexer) Submit(pi *youtube.PlaylistItem) {
	mp.workChan <- pi
}

func newArchiveMultiplexer(ctx context.Context, cfg Config) archiveMultiplexer {
	a := archiveMultiplexer{ctx, cfg,
		make(chan *youtube.PlaylistItem, cfg.MaxParallel),
		make(chan []error),
	}

	for i := uint(0); i < cfg.MaxParallel; i++ {
		go a.worker()
	}

	return a
}

type Archiver struct {
	Config

	ctx    context.Context
	client *youtube.Service

	chancache map[YouTubeChannel]*cachedChannel
}

func checkDownloader(exe string) error {
	proc, err := os.StartProcess(exe, []string{exe, "--version"}, &os.ProcAttr{})
	if err != nil {
		return fmt.Errorf("start process: %v", err)
	}

	state, err := proc.Wait()
	if err != nil || !state.Success() {
		ret := 255
		if state == nil {
			ret = state.ExitCode()
		}
		return fmt.Errorf("abnormal termination (PID %v, exit code %v)", proc.Pid, ret)
	}
	return nil
}

func checkDownloadDirectory(dir string) error {
	testpath := dir + string(os.PathSeparator) + ".ytarchiver"
	f, err := os.Create(testpath)
	if err != nil {
		return err
	}

	return f.Close()
}

// NewArchiver returns an initialised archiver struct which is ready to perform archiving.
// This will fail if the passed API key is invalid or if there is no internet connection.
func NewArchiver(cfg Config) (*Archiver, error) {
	return NewArchiverWithContext(context.Background(), cfg)
}

// NewArchiverWithContext is NewArchiver but with a user-specified context.
func NewArchiverWithContext(ctx context.Context, cfg Config) (*Archiver, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: empty API key", ErrAPIKey)
	}

	ar := &Archiver{
		cfg,
		ctx,
		nil,
		make(map[YouTubeChannel]*cachedChannel),
	}

	cl, err := youtube.NewService(ar.ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAPIConnect, err)
	}
	ar.client = cl

	if err = checkDownloader(cfg.Downloader); err != nil {
		return nil, fmt.Errorf("%w %s: %v", ErrDownloader, cfg.Downloader, err)
	}

	if err = checkDownloadDirectory(cfg.Root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownloadDir, err)
	}

	if err = ar.buildChancache(); err != nil {
		return nil, err
	}

	if err = crawlRoot(ar); err != nil {
		return nil, err
	}

	return ar, nil
}

func (a *Archiver) buildChancache() error {
	if a.chancache == nil {
		panic("build channel cache: encountered nil cache map")
	}

	for _, c := range a.Channels {
		cchan, err := c.getCachedChannel(a.client)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrCacheBuild, err)
		}

		a.chancache[c] = &cchan
	}

	return nil
}

func (a *Archiver) Archive() error {
	var err ArchiveError

	for _, ch := range a.Channels {
		var e error
		cerr := channelError{ChannelID: ch.Identity()}
		runCtx, cancel := context.WithCancel(a.ctx)
		defer cancel()
		mp := newArchiveMultiplexer(runCtx, a.Config)

		chc, ok := a.chancache[ch]
		if !ok {
			cerr.Add(ErrCacheMiss)
			err = append(err, cerr)
			continue
		}
		fmt.Printf("[%s] %v\n", chc.ID, chc)

		e = chc.Foreach(a.ctx, a.client, func(cc *cachedChannel, pi *youtube.PlaylistItem) error {
			// Setup map if it isn't already - prevents full video enumeration happening again
			if cc.Videos == nil {
				cc.Videos = make(map[string]struct{})
			}
			// If already seen, skip this video
			if _, ok := cc.Videos[pi.ContentDetails.VideoId]; ok {
				return nil
			}
			// If any selectors object, skip this video
			for _, m := range a.Selectors {
				if !m.Should(pi, a.client) {
					return nil
				}
			}

			// We're sure we need to be getting this video - submit it
			mp.Submit(pi)
			// And mark it as done (for now)
			cc.Videos[pi.ContentDetails.VideoId] = struct{}{}

			return nil
		})

		if e != nil {
			cerr.Errors = append(cerr.Errors, e)
		}

		mp.Done()
		errs := mp.Wait()
		for _, ve := range errs {
			cerr.Add(ve)
			if errors.Is(ve, ErrVideo) {
				// Video download errored - try again next time maybe?
				delete(a.chancache[ch].Videos, ve.(videoError).VideoID)
			}
		}

		if !cerr.Nil() {
			err = append(err, cerr)
		}
	}

	if len(err) != 0 {
		return err
	} else {
		return nil
	}
}
