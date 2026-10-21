# Traefik OpenAI Header
This is a middleware plugin gets openai request metadata as headers.

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
maxBodyBytes: 67108864
maxInFlightBytes: 2147483648
```

## Request body cap (`maxBodyBytes`)

The plugin must buffer the request body to read `model` and the other fields.
`maxBodyBytes` caps that buffer. The default is `0`: no cap, the v0.1.0 behaviour. Set it to enable the cap (for example 67108864 for 64 MiB).

- If `Content-Length` is above the cap, the plugin rejects the request before it reads any body byte.
- If the body is chunked, the plugin stops reading one byte past the cap and rejects the request.
- The response is `413 Request Entity Too Large` with an OpenAI-style body:

```json
{"error":{"message":"request body exceeds the maximum of 67108864 bytes","type":"invalid_request_error","code":"request_too_large"}}
```

The plugin also sets `X-OpenAI-Parse-Failure: request_too_large` on the request, so a
Traefik `headerlabels` metric label on that header shows rejected requests.

The buffer grows with the bytes that arrive (64 KiB at first, then doubling), never from the
declared `Content-Length` alone. A client that sends headers and then stalls holds at most 64 KiB.

## In-flight budget (`maxInFlightBytes`)

`maxInFlightBytes` caps the total body bytes one middleware instance holds at the same time,
summed over all requests in flight. Default `0` = no budget. When a request would push the total
above the budget, the plugin returns `503 Service Unavailable` with `Retry-After: 1` and:

```json
{"error":{"message":"too many request body bytes in flight on this gateway, retry shortly","type":"server_error","code":"request_body_budget_exceeded"}}
```

It sets `X-OpenAI-Parse-Failure: request_body_budget_exceeded` on the request. Bytes are charged
as they arrive and released as soon as the upstream has read the body to EOF, or when the request
ends. Size the budget from the container memory limit: buffers can retain up to 2x the bytes
charged while they grow, so a 2 GiB budget needs about 4 GiB of headroom.

## Changes from v0.1.0

- New optional `maxBodyBytes` cap and `maxInFlightBytes` budget. Both are off by default, so a v0.1.0 configuration behaves as before.
- The body is read once, into one buffer, bounded by `maxBodyBytes`. v0.1.0 copied it three times.
- The URI regexes compile once in `New`. An invalid regex is a configuration error: the middleware fails to load, and every router that uses it returns an error. v0.1.0 logged the error on every request and matched nothing.
- A body read error (the client stopped mid-upload) sets `X-OpenAI-Parse-Failure: read error` and forwards the bytes that arrived, so the gateway rejects the request through its normal path. Traefik writes no response. v0.1.0 wrote a `500` and then still forwarded the request.
- An empty regex matches nothing. v0.1.0 matched every request.
