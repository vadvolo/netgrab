// Command netgrab-backfill uploads files already present in local storage to
// the configured S3 backend, under the same keys (their path relative to the
// storage root). Use it to seed S3 with data collected before S3 was enabled,
// or after switching S3 backends.
//
// It writes to S3 only (local already holds the files). Existing objects are
// overwritten.
//
// Usage:
//
//	go run ./cmd/netgrab-backfill -config ./config/orion.yaml
//	go run ./cmd/netgrab-backfill -prefixes configs,parsed -concurrency 32
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"netgrab/internal/config"
	"netgrab/internal/storage/s3"
)

func main() {
	_ = godotenv.Load()

	var (
		configPath   = flag.String("config", os.Getenv("CONFIG_PATH"), "path to YAML config (env CONFIG_PATH)")
		storagePath  = flag.String("storage", "", "local storage root to read from (default: config storagePath)")
		prefixesArg  = flag.String("prefixes", "configs,parsed,exports", "comma-separated top-level subtrees to upload")
		concurrency  = flag.Int("concurrency", 16, "parallel uploads")
		ensureBucket = flag.Bool("ensure-bucket", false, "create the target bucket if it does not exist before uploading")
	)
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer func() { _ = logger.Sync() }()

	cfg, err := config.LoadOrion(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !cfg.S3.Enabled {
		log.Fatalf("backfill: s3 is not enabled in config (set s3.enabled: true)")
	}
	root := *storagePath
	if root == "" {
		root = cfg.StoragePath
	}

	ctx := context.Background()
	store, err := s3.New(ctx, s3.Config{
		Bucket:       cfg.S3.Bucket,
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		Profile:      cfg.S3.Profile,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		UsePathStyle: cfg.S3.UsePathStyle,
	})
	if err != nil {
		log.Fatalf("s3: %v", err)
	}
	logger.Info("backfill target",
		zap.String("bucket", cfg.S3.Bucket),
		zap.String("endpoint", cfg.S3.Endpoint),
		zap.String("from", root),
	)

	if *ensureBucket {
		if berr := store.EnsureBucket(ctx); berr != nil {
			log.Fatalf("ensure bucket: %v", berr)
		}
		logger.Info("bucket ensured", zap.String("bucket", cfg.S3.Bucket))
	}

	// Collect the files to upload.
	var keys []string
	for _, p := range strings.Split(*prefixesArg, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		base := filepath.Join(root, p)
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			keys = append(keys, filepath.ToSlash(rel))
			return nil
		})
	}
	logger.Info("files to upload", zap.Int("count", len(keys)))

	var ok, failed, bytes int64
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(key string) {
			defer wg.Done()
			defer func() { <-sem }()

			data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
			if rerr != nil {
				logger.Warn("read failed", zap.String("key", key), zap.Error(rerr))
				atomic.AddInt64(&failed, 1)
				return
			}
			if _, perr := store.Put(ctx, key, data); perr != nil {
				logger.Warn("upload failed", zap.String("key", key), zap.Error(perr))
				atomic.AddInt64(&failed, 1)
				return
			}
			atomic.AddInt64(&ok, 1)
			atomic.AddInt64(&bytes, int64(len(data)))
		}(key)
	}
	wg.Wait()

	logger.Info("backfill complete",
		zap.Int64("uploaded", ok),
		zap.Int64("failed", failed),
		zap.Int64("bytes", bytes),
	)
}
