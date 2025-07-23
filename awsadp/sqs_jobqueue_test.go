package awsadp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/mashiike/atlasic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createElasticMQClient(t *testing.T) *sqs.Client {
	t.Helper()

	// ElasticMQ用の設定
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == sqs.ServiceID {
			return aws.Endpoint{
				URL:               "http://localhost:9324",
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}),
	)
	require.NoError(t, err)

	return sqs.NewFromConfig(cfg)
}

func createTestQueue(t *testing.T, client *sqs.Client, queueName string) string {
	t.Helper()

	// Create queue with short visibility timeout for testing
	result, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]string{
			"VisibilityTimeoutSeconds": "5", // 5秒のVisibilityTimeout
		},
	})
	require.NoError(t, err)

	// Cleanup
	t.Cleanup(func() {
		client.DeleteQueue(context.Background(), &sqs.DeleteQueueInput{
			QueueUrl: result.QueueUrl,
		})
	})

	return *result.QueueUrl
}

func TestSQSJobQueue_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	client := createElasticMQClient(t)
	randomPrefix, err := generateRandomPrefix()
	require.NoError(t, err)
	queueURL := createTestQueue(t, client, "test-queue-"+randomPrefix)

	t.Run("JobOperations", func(t *testing.T) {
		// Create SQS job queue
		jobQueue, err := NewSQSJobQueue(SQSJobQueueConfig{
			Client:   client,
			QueueURL: queueURL,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Test job config
		jobConfig := atlasic.JobConfig{
			TaskID:              "task-123",
			ContextID:           "context-456",
			AcceptedOutputModes: []string{"text/plain", "application/json"},
		}

		// Test Enqueue
		err = jobQueue.Enqueue(ctx, jobConfig)
		require.NoError(t, err)

		// Test Dequeue
		job, err := jobQueue.Dequeue(ctx)
		require.NoError(t, err)
		assert.Equal(t, jobConfig.TaskID, job.TaskID)
		assert.Equal(t, jobConfig.ContextID, job.ContextID)
		assert.Equal(t, jobConfig.AcceptedOutputModes, job.AcceptedOutputModes)

		// Test ExtendTimeoutFunc
		err = job.ExtendTimeoutFunc(ctx, 30*time.Second)
		assert.NoError(t, err)

		// Test CompleteFunc
		err = job.CompleteFunc()
		assert.NoError(t, err)
	})

	t.Run("FailAndRetry", func(t *testing.T) {
		jobQueue, err := NewSQSJobQueue(SQSJobQueueConfig{
			Client:   client,
			QueueURL: queueURL,
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		jobConfig := atlasic.JobConfig{
			TaskID:    "task-fail-test",
			ContextID: "context-fail",
		}

		// Enqueue job
		err = jobQueue.Enqueue(ctx, jobConfig)
		require.NoError(t, err)

		// Dequeue job
		job, err := jobQueue.Dequeue(ctx)
		require.NoError(t, err)
		assert.Equal(t, "task-fail-test", job.TaskID)

		// Test FailFunc (should make job immediately available for retry)
		err = job.FailFunc()
		assert.NoError(t, err)

		// Should be able to dequeue the same job again quickly
		job2, err := jobQueue.Dequeue(ctx)
		require.NoError(t, err)
		assert.Equal(t, "task-fail-test", job2.TaskID)

		// Clean up
		err = job2.CompleteFunc()
		assert.NoError(t, err)
	})

	t.Run("QueueNameConstruction", func(t *testing.T) {
		randomPrefix, err := generateRandomPrefix()
		require.NoError(t, err)
		queueName := "test-queue-name-" + randomPrefix

		// Create queue first
		_, err = client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
			QueueName: aws.String(queueName),
		})
		require.NoError(t, err)

		// Test creation with QueueName instead of QueueURL
		jobQueue, err := NewSQSJobQueue(SQSJobQueueConfig{
			Client:    client,
			QueueName: queueName,
		})
		require.NoError(t, err)

		// Test basic operation
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		jobConfig := atlasic.JobConfig{
			TaskID:    "task-name-test",
			ContextID: "context-name",
		}

		err = jobQueue.Enqueue(ctx, jobConfig)
		require.NoError(t, err)

		job, err := jobQueue.Dequeue(ctx)
		require.NoError(t, err)
		assert.Equal(t, "task-name-test", job.TaskID)

		err = job.CompleteFunc()
		assert.NoError(t, err)

		// Cleanup
		queueURL, _ := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{
			QueueName: aws.String(queueName),
		})
		client.DeleteQueue(context.Background(), &sqs.DeleteQueueInput{
			QueueUrl: queueURL.QueueUrl,
		})
	})
}

func TestSQSJobQueue_ConfigValidation(t *testing.T) {
	t.Run("MissingClient", func(t *testing.T) {
		_, err := NewSQSJobQueue(SQSJobQueueConfig{
			QueueURL: "http://localhost:9324/000000000000/test",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SQS Client is required")
	})

	t.Run("MissingQueueInfo", func(t *testing.T) {
		client := createElasticMQClient(t)
		_, err := NewSQSJobQueue(SQSJobQueueConfig{
			Client: client,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either QueueURL or QueueName must be specified")
	})
}

func TestSQSJobQueue_ParseEvent(t *testing.T) {
	client := createElasticMQClient(t)
	randomPrefix, err := generateRandomPrefix()
	require.NoError(t, err)
	queueURL := createTestQueue(t, client, "test-parse-event-"+randomPrefix)

	jobQueue, err := NewSQSJobQueue(SQSJobQueueConfig{
		Client:   client,
		QueueURL: queueURL,
	})
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("ValidSQSEvent", func(t *testing.T) {
		// Create test job config
		jobConfig := atlasic.JobConfig{
			TaskID:              "task-123",
			ContextID:           "context-456",
			AcceptedOutputModes: []string{"text/plain"},
			IncomingMessageID:   "msg-789",
		}

		// Create SQS event
		sqsEvent := events.SQSEvent{
			Records: []events.SQSMessage{
				{
					MessageId:      "message-123",
					ReceiptHandle:  "receipt-handle-123",
					Body:           mustMarshalJSON(t, jobConfig),
					EventSource:    "aws:sqs",
					EventSourceARN: "arn:aws:sqs:us-east-1:123456789012:test-queue",
				},
			},
		}

		eventData := mustMarshalJSON(t, sqsEvent)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		require.NoError(t, err)
		assert.NotNil(t, job)

		// Verify job properties
		assert.Equal(t, "task-123", job.TaskID)
		assert.Equal(t, "context-456", job.ContextID)
		assert.Equal(t, []string{"text/plain"}, job.AcceptedOutputModes)
		assert.Equal(t, "msg-789", job.IncomingMessageID)

		// Verify functions are not nil
		assert.NotNil(t, job.ExtendTimeoutFunc)
		assert.NotNil(t, job.CompleteFunc)
		assert.NotNil(t, job.FailFunc)

		// Test ExtendTimeoutFunc (note: will fail with mock receipt handle, but function should exist)
		err = job.ExtendTimeoutFunc(ctx, 30*time.Second)
		// In Lambda environment with real receipt handle this would work, but in test it fails
		assert.Error(t, err) // Expected to fail with invalid receipt handle
	})

	t.Run("NonSQSEvent", func(t *testing.T) {
		// Create non-SQS event
		event := map[string]interface{}{
			"Records": []map[string]interface{}{
				{
					"eventSource": "aws:s3",
					"eventName":   "ObjectCreated:Put",
				},
			},
		}

		eventData := mustMarshalJSON(t, event)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		assert.ErrorIs(t, err, atlasic.ErrSkipEvent)
		assert.Nil(t, job)
	})

	t.Run("EmptyEvent", func(t *testing.T) {
		// Create empty event
		event := events.SQSEvent{
			Records: []events.SQSMessage{},
		}

		eventData := mustMarshalJSON(t, event)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		assert.ErrorIs(t, err, atlasic.ErrSkipEvent)
		assert.Nil(t, job)
	})

	t.Run("MultipleSQSMessages", func(t *testing.T) {
		// Create job configs
		jobConfig1 := atlasic.JobConfig{TaskID: "task-1"}
		jobConfig2 := atlasic.JobConfig{TaskID: "task-2"}

		// Create SQS event with multiple messages
		sqsEvent := events.SQSEvent{
			Records: []events.SQSMessage{
				{
					MessageId:     "message-1",
					ReceiptHandle: "receipt-handle-1",
					Body:          mustMarshalJSON(t, jobConfig1),
					EventSource:   "aws:sqs",
				},
				{
					MessageId:     "message-2",
					ReceiptHandle: "receipt-handle-2",
					Body:          mustMarshalJSON(t, jobConfig2),
					EventSource:   "aws:sqs",
				},
			},
		}

		eventData := mustMarshalJSON(t, sqsEvent)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "multiple SQS messages found in event")
		assert.Nil(t, job)
	})

	t.Run("InvalidJobConfig", func(t *testing.T) {
		// Create SQS event with invalid job config
		sqsEvent := events.SQSEvent{
			Records: []events.SQSMessage{
				{
					MessageId:     "message-123",
					ReceiptHandle: "receipt-handle-123",
					Body:          "invalid json",
					EventSource:   "aws:sqs",
				},
			},
		}

		eventData := mustMarshalJSON(t, sqsEvent)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse job config from SQS message")
		assert.Nil(t, job)
	})

	t.Run("MixedEventSources", func(t *testing.T) {
		// Create job config
		jobConfig := atlasic.JobConfig{
			TaskID:    "task-mixed",
			ContextID: "context-mixed",
		}

		// Create mixed event (SQS + S3)
		sqsEvent := events.SQSEvent{
			Records: []events.SQSMessage{
				{
					MessageId:     "message-s3",
					ReceiptHandle: "receipt-handle-s3",
					Body:          "s3 event body",
					EventSource:   "aws:s3", // Non-SQS event
				},
				{
					MessageId:     "message-sqs",
					ReceiptHandle: "receipt-handle-sqs",
					Body:          mustMarshalJSON(t, jobConfig),
					EventSource:   "aws:sqs", // Valid SQS event
				},
			},
		}

		eventData := mustMarshalJSON(t, sqsEvent)

		// Parse event (should process only SQS message)
		job, err := jobQueue.ParseEvent(ctx, json.RawMessage(eventData))
		require.NoError(t, err)
		assert.NotNil(t, job)
		assert.Equal(t, "task-mixed", job.TaskID)
		assert.Equal(t, "context-mixed", job.ContextID)
	})

	t.Run("InvalidEventFormat", func(t *testing.T) {
		// Create invalid event format
		eventData := json.RawMessage(`{"invalid": "format"}`)

		// Parse event
		job, err := jobQueue.ParseEvent(ctx, eventData)
		// The current implementation returns ErrSkipEvent for unrecognizable events
		assert.ErrorIs(t, err, atlasic.ErrSkipEvent)
		assert.Nil(t, job)
	})
}

// Helper function to marshal JSON with error handling
func mustMarshalJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}
