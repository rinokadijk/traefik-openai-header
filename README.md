# Traefik OpenAI Header
This is a middleware plugin gets openai request metadata as headers.

Forked from [rinokadijk/traefik-openai-header](https://github.com/rinokadijk/traefik-openai-header) v0.1.0 (Apache-2.0).

## Config
```yaml
chatCompletionUriRegex: /v1/chat/completions
batchUriRegex: /v1/batches
requestFields:
  model: X-OpenAI-Model
  user: X-OpenAI-User
  temperature: X-OpenAI-Temperature
  max_completion_tokens: X-OpenAI-Max-Completion-Tokens
  logprobs: X-OpenAI-Logprobs
  top_logprobs: X-OpenAI-Top-Logprobs
  tool_choice: X-OpenAI-Tool-Choice
  stream: X-OpenAI-Stream
  completion_window: X-OpenAI-Completion-Window
  oai_endpoint: X-OpenAI-Endpoint
```
maxBodyBytes: 67108864
```

## Request body cap (`maxBodyBytes`)

The plugin must buffer the request body to read `model` and the other fields.
`maxBodyBytes` caps that buffer. The default is 67108864 (64 MiB). `0` means unlimited.

- If `Content-Length` is above the cap, the plugin rejects the request before it reads any body byte.
- If the body is chunked, the plugin stops reading one byte past the cap and rejects the request.
- The response is `413 Request Entity Too Large` with an OpenAI-style body:

```json
{"error":{"message":"request body exceeds the maximum of 67108864 bytes","type":"invalid_request_error","code":"request_too_large"}}
```

The plugin also sets `X-OpenAI-Parse-Failure: request_too_large` on the request, so a
Traefik `headerlabels` metric label on that header shows rejected requests.

## Changes from v0.1.0

- The body is read once, into one buffer, bounded by `maxBodyBytes`. v0.1.0 copied it three times.
- The URI regexes compile once in `New`. An invalid regex is a configuration error.
- A body read error returns `400` and stops. v0.1.0 wrote `500` and still forwarded the request.
- An empty regex matches nothing. v0.1.0 matched every request.
