package ytarchiver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	youtubeWatchURL = "https://youtube.com/watch?v="
)

var ErrYoutubeDownloader = errors.New("ytarchiver: youtube downloader error")

func youtubeDownload(cfg Config, videoID string, outPath string) error {
	uri := youtubeWatchURL + videoID
	var err error

	max := cfg.MaxRetries
	if max == 0 {
		max = 1
	}

	for i := uint(0); i < cfg.MaxRetries; i++ {
		proc := exec.Cmd{
			Path: cfg.Downloader,
			Args: []string{
				cfg.Downloader,
				"-o", outPath,
				"--merge-output-format", "mp4",
			},
		}

		if cfg.DumpVideoInfo {
			proc.Args = append(proc.Args, "--write-info-json")
		}
		proc.Args = append(proc.Args, uri)

		err = proc.Run()
		if err != nil {
			err = fmt.Errorf("%w: %v", ErrYoutubeDownloader, err)
			continue
		}
		if !proc.ProcessState.Success() {
			err = fmt.Errorf("%w: pid %d exitted with code %d", ErrYoutubeDownloader, proc.ProcessState.Pid(), proc.ProcessState.ExitCode())
			continue
		}

		// If we got to here, all succeeded and no more retries
		return nil
	}

	return err
}

// crawlRoot looks at each file and directory in the root of the downloads
// dir and marks already downloaded videos as present in the videos map.
func crawlRoot(a *Archiver) error {
	for _, ch := range a.Channels {
		cch := a.chancache[ch.Identity()]

		dir, err := os.ReadDir(a.Root + string(os.PathSeparator) + cch.ID)
		if err != nil {
			// This is ok and expected as not all channels will yet have
			// been started to be archived.
			continue
		}

		if len(dir) != 0 && cch.Videos == nil {
			cch.Videos = make(map[string]struct{})
		}

		for _, f := range dir {
			if f.IsDir() {
				continue
			}
			if strings.HasSuffix(f.Name(), ".json") {
				continue
			}

			name := f.Name()
			estart := strings.IndexByte(name, '.')
			if estart != -1 {
				name = name[:estart]
			}

			// Name should now contain the raw video ID so insert it
			cch.Videos[name] = struct{}{}
		}
	}

	return nil
}
