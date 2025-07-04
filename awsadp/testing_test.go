package awsadp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestingConfig provides configuration for testing with minio
type TestingConfig struct {
	Endpoint        string // e.g., "http://localhost:9000"
	AccessKeyID     string // e.g., "minioadmin"
	SecretAccessKey string // e.g., "minioadmin"
	Bucket          string // e.g., "atlasic-test"
	Region          string // e.g., "us-east-1" (minio default)
}

// DefaultTestingConfig returns default configuration for local minio testing
func DefaultTestingConfig() TestingConfig {
	return TestingConfig{
		Endpoint:        getEnv("MINIO_ENDPOINT", "http://localhost:9000"),
		AccessKeyID:     getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		SecretAccessKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
		Bucket:          getEnv("MINIO_BUCKET", "atlasic-test"),
		Region:          getEnv("MINIO_REGION", "us-east-1"),
	}
}

// NewS3ClientForTesting creates an S3 client configured for minio testing
func NewS3ClientForTesting(ctx context.Context, cfg TestingConfig) (*s3.Client, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"", // session token (not needed for minio)
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint resolver for minio
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true // Required for minio
	})

	return client, nil
}

// NewS3StorageForTesting creates an S3Storage instance with random prefix for isolated testing
func NewS3StorageForTesting(ctx context.Context, cfg TestingConfig) (*S3Storage, error) {
	client, err := NewS3ClientForTesting(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Generate random prefix for test isolation
	prefix, err := generateRandomPrefix()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random prefix: %w", err)
	}

	return NewS3Storage(S3StorageConfig{
		Client: client,
		Bucket: cfg.Bucket,
		Prefix: prefix,
	}), nil
}

// EnsureBucketExists creates the test bucket if it doesn't exist
func EnsureBucketExists(ctx context.Context, client *s3.Client, bucket string) error {
	// Check if bucket exists
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		return nil // Bucket already exists
	}

	// Create bucket
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket %s: %w", bucket, err)
	}

	return nil
}

// CleanupTestObjects removes all objects with the given prefix (for test cleanup)
func CleanupTestObjects(ctx context.Context, client *s3.Client, bucket, prefix string) error {
	// List all objects with the prefix
	result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return fmt.Errorf("failed to list objects for cleanup: %w", err)
	}

	// Delete all objects
	for _, obj := range result.Contents {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		if err != nil {
			return fmt.Errorf("failed to delete object %s: %w", *obj.Key, err)
		}
	}

	return nil
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func generateRandomPrefix() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "test-" + hex.EncodeToString(bytes), nil
}
