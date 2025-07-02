// Package awsadp provides AWS adapters for ATLASIC interfaces.
// This package implements Storage and JobQueue interfaces using AWS services.
//
// S3Storage: implements Storage interface using AWS S3
// SQSJobQueue: implements JobQueue interface using AWS SQS
//
// These adapters are compatible with minio for local development,
// allowing seamless transition between local and AWS environments.
package awsadp