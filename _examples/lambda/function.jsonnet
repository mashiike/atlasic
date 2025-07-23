local tfstate = std.native('tfstate');
local must_env = std.native('must_env');

{
  Architectures: [
    'arm64',
  ],
  Environment: {
    Variables: {
      ATLASIC_QUEUE_URL: tfstate('aws_sqs_queue.atlasic_example.url'),
      ATLASIC_STORAGE_BUCKET: must_env('TF_VAR_task_bucket_name'),
    },
  },
  Description: 'atlasic example lambda function',
  FunctionName: 'atlasic-example',
  Handler: 'bootstrap',
  MemorySize: 512,
  Role: tfstate('aws_iam_role.atlasic_example.arn'),
  Runtime: 'provided.al2023',
  Timeout: 360,
  TracingConfig: {
    Mode: 'PassThrough',
  },
  LoggingConfig: {
    LogFormat: 'JSON',
  },
}
