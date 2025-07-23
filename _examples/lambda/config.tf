provider "aws" {
  region = "ap-northeast-1"
}

terraform {
  required_version = "= 1.12.2"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= 6.4.0"
    }
  }
  backend "s3" {
    bucket = "terraform.example.com"
    key    = "atlasic-example/terraform.tfstate"
    region = "ap-northeast-1"
  }
}

resource "aws_iam_role" "atlasic_example" {
  name = "atlasic-example"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Sid    = ""
        Principal = {
          Service = [
            "lambda.amazonaws.com",
          ]
        }
      }
    ]
  })
}

resource "aws_iam_policy" "atlasic_example" {
  name   = "atlasic-example"
  path   = "/"
  policy = data.aws_iam_policy_document.atlasic_example.json
}

resource "aws_iam_role_policy_attachment" "atlasic_example" {
  role       = aws_iam_role.atlasic_example.name
  policy_arn = aws_iam_policy.atlasic_example.arn
}

variable "task_bucket_name" {
  description = "S3 bucket name"
  type        = string
}

data "aws_iam_policy_document" "atlasic_example" {
  statement {
    resources = [
      aws_sqs_queue.atlasic_example.arn,
    ]
    actions = [
      "sqs:DeleteMessage",
      "sqs:GetQueueUrl",
      "sqs:ChangeMessageVisibility",
      "sqs:ReceiveMessage",
      "sqs:SendMessage",
      "sqs:GetQueueAttributes",
    ]
  }
  statement {
    resources = [
      "arn:aws:s3:::${var.task_bucket_name}",
      "arn:aws:s3:::${var.task_bucket_name}/*",
    ]
    actions = [
      "s3:PutObject",
      "s3:GetObject",
      "s3:ListBucket",
      "s3:DeleteObject",
      "s3:ListObjects",
    ]
  }
  statement {
    actions = [
      "logs:GetLog*",
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["*"]
  }
  statement {
    actions = [
      "bedrock:InvokeModel",
      "bedrock:Converse",
    ]
    resources = [ "*"]
  }
  statement {
    actions = [
      "iam:PassRole",
    ]
    resources = [
      "arn:aws:iam::*:role/*AmazonBedrock*"
    ]
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["bedrock.amazonaws.com"]
    }
  }
}

## for simple example
resource "aws_sqs_queue" "atlasic_example" {
  name                       = "atlasic-example"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 300
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.atlasic_example_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "atlasic_example_dlq" {
  name                      = "atlasic-example-dlq"
  message_retention_seconds = 345600
}

data "archive_file" "atlasic_example_dummy" {
  type        = "zip"
  output_path = "${path.module}/atlasic_example_dummy.zip"
  source {
    content  = "atlasic_example_dummy"
    filename = "bootstrap"
  }
  depends_on = [
    null_resource.atlasic_example_dummy
  ]
}

resource "null_resource" "atlasic_example_dummy" {}

resource "aws_lambda_function" "atlasic_example" {
  lifecycle {
    ignore_changes = all
  }

  function_name = "atlasic-example"
  role          = aws_iam_role.atlasic_example.arn

  handler  = "bootstrap"
  runtime  = "provided.al2"
  filename = data.archive_file.atlasic_example_dummy.output_path
}

resource "aws_lambda_alias" "atlasic_example" {
  lifecycle {
    ignore_changes = all
  }
  name             = "current"
  function_name    = aws_lambda_function.atlasic_example.arn
  function_version = aws_lambda_function.atlasic_example.version
}

resource "aws_lambda_event_source_mapping" "atlasic_example" {
  batch_size                         = 10
  event_source_arn                   = aws_sqs_queue.atlasic_example.arn
  enabled                            = true
  maximum_batching_window_in_seconds = 5
  function_name                      = aws_lambda_alias.atlasic_example.arn
  function_response_types            = ["ReportBatchItemFailures"]
}

resource "aws_lambda_function_url" "atlasic_example" {
  function_name      = aws_lambda_alias.atlasic_example.function_name
  qualifier          = aws_lambda_alias.atlasic_example.name
  authorization_type = "NONE"

  cors {
    allow_credentials = true
    allow_origins     = ["*"]
    allow_methods     = ["GET", "POST"]
    expose_headers    = ["keep-alive", "date"]
    max_age           = 0
  }

  invoke_mode = "RESPONSE_STREAM"
}
