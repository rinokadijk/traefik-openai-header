# Traefik OpenAI Header
This is a middleware plugin gets openai request metadata as headers.

## Config
```yaml
requestUriPrefix: /v1/chat/completions
requestFields:
  model: X-OpenAI-Model
  user: X-OpenAI-User
  temperature: X-OpenAI-Temperature
  max_completion_tokens: X-OpenAI-Max-Completion-Tokens
  logprobs: X-OpenAI-Logprobs
  top_logprobs: X-OpenAI-Top-Logprobs
  tool_choice: X-OpenAI-Tool-Choice
  stream: X-OpenAI-Stream
```