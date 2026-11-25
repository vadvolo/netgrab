// Package s3 is an S3-compatible storage.Store backend. It works against any
// S3 API — AWS S3, MinIO, or a cloud object store — selected by endpoint and
// region.
//
// Credentials come from the standard AWS chain (env AWS_ACCESS_KEY_ID /
// AWS_SECRET_ACCESS_KEY, shared profile, etc.) unless AccessKey/SecretKey are
// set explicitly in Config.
package s3

import (
	"bytes"
	"context"
	"fmt"

	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Config describes the bucket and endpoint.
type Config struct {
	Bucket   string // required
	Endpoint string // empty = AWS default endpoint for the region
	Region   string // default us-east-1
	Profile  string // optional shared-config profile

	// Explicit static credentials; when empty the default AWS chain is used.
	AccessKey string
	SecretKey string

	// UsePathStyle forces path-style addressing (bucket in the path rather than
	// the host). MinIO requires it; most cloud stores use virtual-hosted style.
	UsePathStyle bool
}

const defaultRegion = "us-east-1"

// Store is a connected S3 client scoped to one bucket.
type Store struct {
	client *s3.Client
	bucket string
	region string
}

// New builds an S3 store from cfg, loading credentials from the AWS chain.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	endpoint := cfg.Endpoint
	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if cfg.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3: loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	return &Store{client: client, bucket: cfg.Bucket, region: region}, nil
}

// EnsureBucket creates the bucket if it does not already exist. It is a no-op
// when the bucket is present (or owned by this account). Safe to call on every
// start, but typically invoked once from an admin command.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil // already exists and reachable
	}

	in := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
	// us-east-1 must NOT send a LocationConstraint; any other region must.
	if s.region != "" && s.region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}
	if _, cerr := s.client.CreateBucket(ctx, in); cerr != nil {
		// Treat "already owned by you / already exists" as success.
		var ae smithy.APIError
		if errors.As(cerr, &ae) {
			switch ae.ErrorCode() {
			case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
				return nil
			}
		}
		return fmt.Errorf("s3: create bucket %s: %w", s.bucket, cerr)
	}
	return nil
}

// Put uploads data under key and returns an s3://bucket/key locator.
func (s *Store) Put(ctx context.Context, key string, data []byte) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return "", fmt.Errorf("s3: put %s/%s: %w", s.bucket, key, err)
	}
	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}

// Bucket returns the bucket this store writes to.
func (s *Store) Bucket() string { return s.bucket }
