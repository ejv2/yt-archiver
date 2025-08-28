package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	ytarchiver "github.com/ejv2/yt-archiver"
)

const (
	VersionMajor = 1
	VersionMinor = 1
	VersionPatch = 0
	VersionRev   = 1
)

func initialize() (Config, *ytarchiver.Archiver, error) {
	cfg, err := NewConfig()
	if err != nil {
		return Config{}, nil, fmt.Errorf("ytarchiver: parsing config: %s", err.Error())
	}

	if err = ValidateConfig(cfg); err != nil {
		return Config{}, nil, fmt.Errorf("invalid config: %w", err)
	}

	conf, err := cfg.ArchiverConfig()
	if err != nil {
		return Config{}, nil, fmt.Errorf("ytarchiver: loading config: %w", err)
	}

	ar, err := ytarchiver.NewArchiver(conf)
	if err != nil {
		return Config{}, nil, err
	}

	return cfg, ar, nil
}

func doArchive(t time.Time, ar *ytarchiver.Archiver, cfg Config) {
	log.Printf("Starting archive run on %d channel(s)", len(cfg.Channels))
	if err := ar.Archive(); err != nil {
		fmt.Println(err)
	}

	log.Printf("Archive OK; time elapsed %v", time.Since(t))
}

func main() {
	log.Printf("Starting ytarchiver v%d.%d.%d-%d...", VersionMajor, VersionMinor, VersionPatch, VersionRev)

	cfg, ar, err := initialize()
	if err != nil {
		log.Fatalln(err)
	}

	exitchan := make(chan os.Signal, 1)
	signal.Notify(exitchan, os.Interrupt, syscall.SIGTERM)
	reloadchan := make(chan os.Signal, 1)
	signal.Notify(reloadchan, syscall.SIGHUP)
	archivechan := make(chan os.Signal, 1)
	signal.Notify(archivechan, syscall.SIGALRM)

	log.Printf("Archiver ready on %d worker(s), %d channel(s) and archiving approx. every %v", cfg.MaxParallel, len(cfg.Channels), cfg.Interval)
	tk := time.NewTicker(cfg.Interval)
	for {
		select {
		case <-archivechan:
			t := time.Now()
			doArchive(t, ar, cfg)
		case t := <-tk.C:
			doArchive(t, ar, cfg)
		case <-exitchan:
			log.Println("Caught fatal signal; exitting gracefully...")
			os.Exit(0)
		case <-reloadchan:
			log.Println("Got SIGHUP; reloading configuration...")
			cfg, ar, err = initialize()
			if err != nil {
				log.Println("Got error in configuration while live reloading!")
				log.Fatalln(err)
			}
			log.Printf("Now ready on %d worker(s), %d channel(s) and archiving approx. every %v", cfg.MaxParallel, len(cfg.Channels), cfg.Interval)
			tk.Reset(cfg.Interval)
		}
	}
}
