package storage

import (
	"context"
	"fmt"

	"netgrab/internal/storage/local"
	"netgrab/internal/storage/s3"
)

// S3Params configures the optional S3 backend for Build. The zero value (with
// Enabled false) means "local only".
type S3Params struct {
	Enabled      bool
	Bucket       string
	Endpoint     string
	Region       string
	Profile      string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
}

// Build assembles the write target: always local (rooted at localPath), plus S3
// when s3p.Enabled. It returns the Multi store and a human-readable list of the
// backends it contains (for logging).
func Build(ctx context.Context, localPath string, s3p S3Params) (*Multi, []string, error) {
	stores := []Store{local.NewLocalStorage(localPath, "")}
	descr := []string{"local:" + localPath}

	if s3p.Enabled {
		st, err := s3.New(ctx, s3.Config{
			Bucket:       s3p.Bucket,
			Endpoint:     s3p.Endpoint,
			Region:       s3p.Region,
			Profile:      s3p.Profile,
			AccessKey:    s3p.AccessKey,
			SecretKey:    s3p.SecretKey,
			UsePathStyle: s3p.UsePathStyle,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("storage: building S3 backend: %w", err)
		}
		stores = append(stores, st)
		descr = append(descr, fmt.Sprintf("s3://%s (%s)", s3p.Bucket, s3p.Endpoint))
	}

	return NewMulti(stores...), descr, nil
}
