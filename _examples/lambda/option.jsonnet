local must_env = std.native('must_env');

{
  log_level: 'debug',
  color: true,
  region: 'ap-northeast-1',
  tfstate: 's3://'+must_env('TF_VAR_tfstate_bucket_name')+'/atlasic-example/terraform.tfstate',
}
