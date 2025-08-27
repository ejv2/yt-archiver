package ytarchiver

import (
	"errors"
	"fmt"
	"os/exec"
)

const (
	youtubeWatchURL = "https://youtube.com/watch?v="
)

var ErrYoutubeDownloader = errors.New("ytarchiver: youtube downloader error")

func youtubeDownload(cfg Config, videoID string, outPath string) error {
	uri := youtubeWatchURL + videoID

	proc := exec.Cmd{
		Path: cfg.Downloader,
		Args: []string{
			cfg.Downloader,
			"-o", outPath,
			"--write-info-json",
			uri,
		},
	}

	err := proc.Run()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrYoutubeDownloader, err)
	}
	if !proc.ProcessState.Success() {
		return fmt.Errorf("%w: pid %d exitted with code %d", ErrYoutubeDownloader, proc.ProcessState.Pid(), proc.ProcessState.ExitCode())
	}

	return nil
}
