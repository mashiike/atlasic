# AWS Adapters for ATLASIC

This package provides AWS service adapters for ATLASIC interfaces, enabling cloud-native deployments while maintaining compatibility with local development using minio.

## Features

- **S3Storage**: Implements `atlasic.Storage` interface using AWS S3
- **Minio Compatibility**: Same codebase works with both AWS S3 and local minio
- **Test Isolation**: Random prefix support for parallel test execution
- **Optimistic Concurrency**: Full support for ATLASIC's version control

## S3Storage Usage

### Basic Usage

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/mashiike/atlasic/awsadp"
)

// Load AWS configuration
cfg, err := config.LoadDefaultConfig(ctx)
if err != nil {
    log.Fatal(err)
}

// Create S3 client
s3Client := s3.NewFromConfig(cfg)

// Create S3Storage
storage := awsadp.NewS3Storage(awsadp.S3StorageConfig{
    Client: s3Client,
    Bucket: "my-atlasic-bucket",
})

// Use with ATLASIC Server
server := &atlasic.Server{
    Addr:    ":8080",
    Agent:   myAgent,
    Storage: storage,
}
```

### With Object Key Prefix (useful for multi-tenant scenarios)

```go
storage := awsadp.NewS3Storage(awsadp.S3StorageConfig{
    Client: s3Client,
    Bucket: "shared-atlasic-bucket",
    Prefix: "tenant-123",  // All objects will be prefixed with "tenant-123/"
})
```

## Testing with Minio

### Setup

1. Start minio using Docker Compose:
```bash
docker-compose -f docker-compose.test.yml up -d
```

2. Run tests:
```bash
go test ./awsadp -v
```

### Environment Variables

- `MINIO_ENDPOINT`: minio endpoint (default: `http://localhost:9000`)
- `MINIO_ACCESS_KEY`: access key (default: `minioadmin`)
- `MINIO_SECRET_KEY`: secret key (default: `minioadmin`)
- `MINIO_BUCKET`: bucket name (default: `atlasic-test`)
- `MINIO_REGION`: region (default: `us-east-1`)
- `SKIP_INTEGRATION_TESTS`: set to `true` to skip integration tests

### Test Isolation

Tests automatically use random prefixes to ensure isolation:

```go
// Each test gets a unique prefix like "test-a1b2c3d4/"
storage, err := awsadp.NewS3StorageForTesting(ctx, cfg)
```

## S3 Object Structure

The S3Storage adapter uses the following object key structure:

```
{prefix}/tasks/{taskID}.json                    # Task objects
{prefix}/contexts/{contextID}/{taskID}.json     # Context-based task listing
{prefix}/events/{contextID}/{taskID}.json       # Event streams
{prefix}/push-configs/{taskID}/{configID}.json  # Push notification configs
```

When `prefix` is empty, objects are stored at the root level.

## Error Handling

S3Storage properly maps S3 errors to ATLASIC error types:

- S3 `NoSuchKey` → `atlasic.ErrTaskNotFound`
- S3 `NoSuchKey` → `atlasic.ErrContextNotFound` 
- S3 `NoSuchKey` → `atlasic.ErrPushNotificationConfigNotFound`

## Performance Considerations

- **Optimistic Concurrency**: Version checking requires additional S3 operations
- **Event Sourcing**: Large event streams are stored as single objects
- **Prefix Performance**: Use meaningful prefixes to optimize S3 listing operations

## Compatibility

- **AWS S3**: Full compatibility with all S3 features
- **Minio**: Tested with minio for local development
- **S3-Compatible**: Should work with other S3-compatible services

## Future Enhancements

- **SQSJobQueue**: AWS SQS implementation for JobQueue interface (planned)
- **Performance Optimization**: Potential event stream partitioning for large workloads
- **Encryption**: Support for S3 server-side encryption