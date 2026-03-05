package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/akave-ai/akavelog/internal/config"
	"github.com/akave-ai/akavelog/internal/model"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// O3Client uploads and downloads objects from Akave O3 (S3-compatible API).
type O3Client struct {
	client *s3.Client
	bucket string
}

// NewO3Client builds an S3-compatible client for the given O3 config.
// Returns nil if cfg is nil or endpoint/bucket are empty.
func NewO3Client(cfg *config.O3Config) (*O3Client, error) {
	if cfg == nil || cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, nil
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	client := s3.NewFromConfig(aws.Config{
		Region:      region,
		Credentials: aws.NewCredentialsCache(creds),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})
	return &O3Client{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the bucket if it does not exist (HeadBucket fails → CreateBucket).
func (c *O3Client) EnsureBucket(ctx context.Context) error {
	if c == nil {
		return nil
	}
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	// HeadBucket failed (404 NoSuchBucket or similar); try to create.
	_, createErr := c.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if createErr != nil {
		var apiErr smithy.APIError
		if errors.As(createErr, &apiErr) {
			switch apiErr.ErrorCode() {
			case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
				return nil
			}
		}
		return createErr
	}
	return nil
}

// PutObject uploads data to key. Key can include prefixes (e.g. "project/default/2024/01/15/batch-abc.json.gz").
func (c *O3Client) PutObject(ctx context.Context, key string, data []byte, contentType string) error {
	if c == nil {
		return fmt.Errorf("o3 client not configured")
	}
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

// ObjectInfo describes an object in O3 (for list response).
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ListObjects lists objects under prefix (e.g. "logs/"). Returns nil, nil if client is nil.
func (c *O3Client) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if c == nil {
		return nil, nil
	}
	out, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
	}
	result := make([]ObjectInfo, 0, len(out.Contents))
	for _, o := range out.Contents {
		info := ObjectInfo{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size)}
		if o.LastModified != nil {
			info.LastModified = *o.LastModified
		}
		result = append(result, info)
	}
	return result, nil
}

// GetObject downloads an object by key. Returns nil, nil if client is nil.
func (c *O3Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("o3 client not configured")
	}
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// GetObjectLogs downloads a gzipped JSON batch by key and returns the log entries.
func (c *O3Client) GetObjectLogs(ctx context.Context, key string) ([]model.LogEntry, error) {
	raw, err := c.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var entries []model.LogEntry
	if err := json.Unmarshal(decoded, &entries); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return entries, nil
}
