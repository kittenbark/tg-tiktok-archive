package main

import (
	"flag"
	"github.com/kittenbark/tg-tiktok-archive/internal/archive"
	"log"
	"log/slog"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	archivePath := flag.String("archive", "archive.yaml", "path to archive file")
	downloadedPath := flag.String("downloaded", "downloaded.yaml", "path to downloaded cache file")
	errorsPath := flag.String("errors", "post_errors.yaml", "path to errors cache file")
	loglevel := flag.Int("loglevel", int(slog.LevelInfo), "log level")
	flag.Parse()
	slog.SetLogLoggerLevel(slog.Level(*loglevel))

	arch, err := archive.New(*configPath, *archivePath, *downloadedPath, *errorsPath)
	if err != nil {
		log.Fatal(err)
	}
	arch.Start()
}
