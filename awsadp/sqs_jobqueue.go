package awsadp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/mashiike/atlasic"
)

// SQSJobQueueConfig represents the configuration for SQS JobQueue
type SQSJobQueueConfig struct {
	Client    *sqs.Client
	QueueURL  string
	QueueName string       // For automatic generation when QueueName is not specified (Optional)
	Logger    *slog.Logger // Optional logger, defaults to slog.Default()
}

// SQSJobQueue implements JobQueue interface using AWS SQS
type SQSJobQueue struct {
	client   *sqs.Client
	queueURL string
	logger   *slog.Logger
}

// NewSQSJobQueue creates a new SQS-based job queue
func NewSQSJobQueue(config SQSJobQueueConfig) (*SQSJobQueue, error) {
	if config.Client == nil {
		return nil, errors.New("SQS Client is required")
	}

	queueURL := config.QueueURL
	if queueURL == "" && config.QueueName == "" {
		return nil, errors.New("either QueueURL or QueueName must be specified")
	}

	// Get QueueURL using GetQueueUrl when QueueName is specified
	if queueURL == "" {
		result, err := config.Client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{
			QueueName: aws.String(config.QueueName),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get queue URL for %s: %w", config.QueueName, err)
		}
		queueURL = *result.QueueUrl
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &SQSJobQueue{
		client:   config.Client,
		queueURL: queueURL,
		logger:   logger,
	}, nil
}

// Enqueue adds a new job to the SQS queue
func (q *SQSJobQueue) Enqueue(ctx context.Context, config atlasic.JobConfig) error {
	messageBody, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal job config: %w", err)
	}

	_, err = q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(string(messageBody)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}

// Dequeue retrieves a job from the SQS queue, blocking until one is available
func (q *SQSJobQueue) Dequeue(ctx context.Context) (*atlasic.Job, error) {
	for {
		// Long polling for efficiency
		result, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(q.queueURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     20, // Long polling
		})
		if err != nil {
			return nil, fmt.Errorf("failed to receive message from SQS: %w", err)
		}

		if len(result.Messages) == 0 {
			// No messages, check context cancellation
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				continue // Continue polling
			}
		}

		message := result.Messages[0]

		// Parse job config from message body
		var jobConfig atlasic.JobConfig
		if err := json.Unmarshal([]byte(*message.Body), &jobConfig); err != nil {
			// Invalid message, delete it to avoid infinite reprocessing
			if deleteErr := q.deleteMessage(context.Background(), *message.ReceiptHandle); deleteErr != nil {
				q.logger.Error("Failed to delete invalid message", "error", deleteErr, "receiptHandle", *message.ReceiptHandle)
			}
			continue
		}

		// Create job with SQS-specific functions
		job := &atlasic.Job{
			TaskID:              jobConfig.TaskID,
			ContextID:           jobConfig.ContextID,
			AcceptedOutputModes: jobConfig.AcceptedOutputModes,
			IncomingMessageID:   jobConfig.IncomingMessageID,
			ExtendTimeoutFunc:   q.createExtendTimeoutFunc(*message.ReceiptHandle),
			CompleteFunc:        q.createCompleteFunc(*message.ReceiptHandle),
			FailFunc:            q.createFailFunc(*message.ReceiptHandle),
		}

		return job, nil
	}
}

// Close gracefully shuts down the queue
func (q *SQSJobQueue) Close() error {
	// SQS doesn't require explicit closing
	return nil
}

// createExtendTimeoutFunc creates a function to extend message visibility timeout
func (q *SQSJobQueue) createExtendTimeoutFunc(receiptHandle string) func(context.Context, time.Duration) error {
	return func(ctx context.Context, duration time.Duration) error {
		visibilityTimeout := int32(duration.Seconds())
		_, err := q.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(q.queueURL),
			ReceiptHandle:     aws.String(receiptHandle),
			VisibilityTimeout: visibilityTimeout,
		})
		if err != nil {
			return fmt.Errorf("failed to extend timeout: %w", err)
		}
		return nil
	}
}

// createCompleteFunc creates a function to mark job as completed (delete message)
func (q *SQSJobQueue) createCompleteFunc(receiptHandle string) func() error {
	return func() error {
		return q.deleteMessage(context.Background(), receiptHandle)
	}
}

// createFailFunc creates a function to mark job as failed (immediate retry)
// TODO: Future enhancement - support RetryAfter duration
func (q *SQSJobQueue) createFailFunc(receiptHandle string) func() error {
	return func() error {
		// Set visibility timeout to 0 for immediate retry
		_, err := q.client.ChangeMessageVisibility(context.Background(), &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(q.queueURL),
			ReceiptHandle:     aws.String(receiptHandle),
			VisibilityTimeout: 0,
		})
		if err != nil {
			return fmt.Errorf("failed to fail job: %w", err)
		}
		return nil
	}
}

// deleteMessage deletes a message from the queue
func (q *SQSJobQueue) deleteMessage(ctx context.Context, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}
	return nil
}

// ParseEvent implements EventParser interface for AWS Lambda integration
// This allows SQS messages to be processed directly from Lambda events
func (q *SQSJobQueue) ParseEvent(ctx context.Context, event json.RawMessage) (*atlasic.Job, error) {
	// Try to parse as SQS event
	var sqsEvent events.SQSEvent

	if err := json.Unmarshal(event, &sqsEvent); err != nil {
		return nil, fmt.Errorf("failed to parse SQS event: %w", err)
	}

	targetEvents := make([]events.SQSMessage, 0, len(sqsEvent.Records))
	for _, record := range sqsEvent.Records {
		if record.EventSource != "aws:sqs" {
			q.logger.Warn("Skipping non-SQS event", "eventSource", record.EventSource)
			continue
		}
		targetEvents = append(targetEvents, record)
	}
	if len(targetEvents) == 0 {
		return nil, atlasic.ErrSkipEvent
	}

	if len(targetEvents) > 1 {
		return nil, fmt.Errorf("multiple SQS messages found in event, expected only one: %d messages", len(targetEvents))
	}

	// Parse job config from SQS message body
	var jobConfig atlasic.JobConfig
	if err := json.Unmarshal([]byte(targetEvents[0].Body), &jobConfig); err != nil {
		return nil, fmt.Errorf("failed to parse job config from SQS message: %w", err)
	}

	// Create job - Lambda handles message deletion automatically
	job := &atlasic.Job{
		TaskID:              jobConfig.TaskID,
		ContextID:           jobConfig.ContextID,
		AcceptedOutputModes: jobConfig.AcceptedOutputModes,
		IncomingMessageID:   jobConfig.IncomingMessageID,
		ExtendTimeoutFunc: func(ctx context.Context, duration time.Duration) error {
			_, err := q.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          aws.String(q.queueURL),
				ReceiptHandle:     aws.String(targetEvents[0].ReceiptHandle),
				VisibilityTimeout: int32(duration.Seconds()),
			})
			if err != nil {
				return fmt.Errorf("failed to extend visibility timeout: %w", err)
			}
			return nil
		},
		CompleteFunc: func() error {
			// Lambda deletes message automatically on success
			return nil
		},
		FailFunc: func() error {
			// Lambda handles retry automatically on error
			return nil
		},
	}

	return job, nil
}
