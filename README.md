## ATLASIC - A2A Toolkit Library to build Agentic Service for Infrastructure on Cloud

## memo 

古いATLASICからの移行。
別で検討してたagentstateから部分的に移植して方向転換。

全体的にはAgentServiceの実態は スーパーバイザーAgent で ReAct(Reasoning and Acting)を行う。
RegisterAgentで登録された専門Agentにスーパーバイザーが委譲する。
スーパーバイザーはdelegate_to_agentのToolを持つ

多分ConversationHistoryとGetTaskで行ける

現状は、まだ移植、統合途中
