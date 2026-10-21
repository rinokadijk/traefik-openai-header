// Package traefik_openai_header contains the config and functions to convert model params into headers
package traefik_openai_header

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

const ParseFailureHeader = "X-OpenAI-Parse-Failure"
const UserAgentHeader = "X-OpenAI-User-Agent"

// DefaultMaxBodyBytes is the default request body cap: 64 MiB.
const DefaultMaxBodyBytes int64 = 64 << 20

// Config the plugin configuration.
type Config struct {
	RequestFields          map[string]interface{} `json:"requestFields"`
	RequestURIRegex        string                 `json:"requestUriRegex"`
	ChatCompletionUriRegex string                 `json:"chatCompletionUriRegex"`
	BatchUriRegex          string                 `json:"batchUriRegex"`
	// MaxBodyBytes caps the request body the plugin buffers. Bodies above it get a 413. 0 = unlimited.
	MaxBodyBytes int64 `json:"maxBodyBytes"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	fields := map[string]interface{}{}
	fields["model"] = "X-OpenAI-Model"
	fields["frequency_penalty"] = "X-OpenAI-Frequency-Penalty"
	fields["user"] = "X-OpenAI-User"
	fields["temperature"] = "X-OpenAI-Temperature"
	fields["top_p"] = "X-OpenAI-Top-P"
	fields["max_completion_tokens"] = "X-OpenAI-Max-Completion-Tokens"
	fields["presence_penalty"] = "X-OpenAI-Presence-Penalty"
	fields["logprobs"] = "X-OpenAI-Logprobs"
	fields["top_logprobs"] = "X-OpenAI-Top-Logprobs"
	fields["tool_choice"] = "X-OpenAI-Tool-Choice"
	fields["stream"] = "X-OpenAI-Stream"
	fields["include_usage"] = "X-OpenAI-Stream-Options-Include-Usage"
	fields["completion_window"] = "X-OpenAI-Completion-Window"
	fields["oai_endpoint"] = "X-OpenAI-Endpoint"
	return &Config{
		RequestFields:          fields,
		RequestURIRegex:        "/v1/chat/completions",
		ChatCompletionUriRegex: "/v1/chat/completions",
		BatchUriRegex:          "/v1/batches",
		MaxBodyBytes:           DefaultMaxBodyBytes,
	}
}

// Handler contains the config for the plugin
type Handler struct {
	name          string
	next          http.Handler
	requestFields map[string]interface{}
	chatRegex     *regexp.Regexp
	batchRegex    *regexp.Regexp
	maxBodyBytes  int64
}

// compileOrNil compiles expr once. An empty expr matches nothing.
func compileOrNil(expr string) (*regexp.Regexp, error) {
	if expr == "" {
		return nil, nil
	}
	return regexp.Compile(expr)
}

func matches(re *regexp.Regexp, s string) bool {
	return re != nil && re.MatchString(s)
}

// New Creates a new HTTP Handler to translate the openai model into headers
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		config = CreateConfig()
	}

	chatCompletionUri := ""
	if config.RequestURIRegex != "" {
		chatCompletionUri = config.RequestURIRegex
	} else {
		chatCompletionUri = config.ChatCompletionUriRegex
	}

	chatRegex, err := compileOrNil(chatCompletionUri)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid requestUriRegex %q: %w", name, chatCompletionUri, err)
	}
	batchRegex, err := compileOrNil(config.BatchUriRegex)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid batchUriRegex %q: %w", name, config.BatchUriRegex, err)
	}
	if config.MaxBodyBytes < 0 {
		return nil, fmt.Errorf("%s: maxBodyBytes must be 0 or greater, got %d", name, config.MaxBodyBytes)
	}

	return &Handler{
		name:          name,
		requestFields: config.RequestFields,
		chatRegex:     chatRegex,
		batchRegex:    batchRegex,
		maxBodyBytes:  config.MaxBodyBytes,
		next:          next,
	}, nil
}

type audio struct {
	Format string `json:"format,omitempty"`
	Voice  string `json:"voice,omitempty"`
}

type responseFormat struct {
	Type string `json:"type,omitempty"`
}

type streamOptions struct {
	IncludeUsage *bool `json:"include_usage,omitempty"`
}

type approximate struct {
	City     string `json:"city,omitempty"`
	Country  string `json:"country,omitempty"`
	Region   string `json:"region,omitempty"`
	TimeZone string `json:"timezone,omitempty"`
}

type userLocation struct {
	Approximate approximate `json:"approximate,omitempty"`
}

type webSearchOptions struct {
	SearchContextSize string       `json:"search_context_size,omitempty"`
	UserLocation      userLocation `json:"user_location,omitempty"`
}

type chatCompletionRequest struct {
	Model               string            `json:"model"`
	Audio               audio             `json:"audio,omitempty"`
	FrequencyPenalty    *float32          `json:"frequency_penalty,omitempty"`
	MaxCompletionTokens *float32          `json:"max_completion_tokens,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	Modalities          []string          `json:"modalities,omitempty"`
	N                   *int              `json:"n,omitempty"`
	PresencePenalty     *float32          `json:"presence_penalty,omitempty"`
	ReasoningEffort     string            `json:"reasoning_effort,omitempty"`
	ResponseFormat      responseFormat    `json:"response_format,omitempty"`
	Seed                *int              `json:"seed,omitempty"`
	ServiceTier         string            `json:"service_tier,omitempty"`
	Store               *bool             `json:"store,omitempty"`
	Stream              *bool             `json:"stream,omitempty"`
	StreamOptions       streamOptions     `json:"stream_options,omitempty"`
	Temperature         *float32          `json:"temperature,omitempty"`
	TopP                *float32          `json:"top_p,omitempty"`
	User                string            `json:"user,omitempty"`
	WebSearchOptions    webSearchOptions  `json:"web_search_options,omitempty"`
	Logprobs            *int              `json:"logprobs"`
	TopLogprobs         *int              `json:"top_logprobs"`
	ToolChoice          interface{}       `json:"tool_choice"`
}

type chatCompletionModelOnlyRequest struct {
	Model string `json:"model"`
}

type batchRequest struct {
	CompletionWindow string `json:"completion_window"`
	Endpoint         string `json:"endpoint"`
}

func (e *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isChatCompletionRequest := matches(e.chatRegex, r.RequestURI)
	isBatchRequest := matches(e.batchRegex, r.RequestURI)

	if (!isChatCompletionRequest && !isBatchRequest) || r.Method != http.MethodPost {
		e.next.ServeHTTP(w, r)
		return
	}

	data, ok := e.readBody(w, r)
	if !ok {
		// readBody wrote the error response. Never forward the request after that.
		return
	}

	if len(data) < 1 {
		r.Header.Set(ParseFailureHeader, "empty body")
	}

	if len(data) > 0 && len(e.requestFields) > 0 && isChatCompletionRequest {
		e.handleChatCompletionRequest(data, r)
	}

	if len(data) > 0 && len(e.requestFields) > 0 && isBatchRequest {
		e.handleBatchRequest(data, r)
	}

	if len(r.Header.Get("User-Agent")) > 0 {
		r.Header.Set(UserAgentHeader, r.Header.Get("User-Agent"))
	}

	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))

	e.next.ServeHTTP(w, r)
}

// readBody reads the request body once into memory, bounded by maxBodyBytes.
// It returns false after it has written an error response.
func (e *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	limit := e.maxBodyBytes
	if limit > 0 && r.ContentLength > limit {
		// The client declared the size. Reject before reading a single body byte.
		e.rejectTooLarge(w, r)
		return nil, false
	}

	var src io.Reader = r.Body
	if limit > 0 {
		// One byte past the cap is enough to prove the body is too large.
		src = io.LimitReader(r.Body, limit+1)
	}

	var buf bytes.Buffer
	if r.ContentLength > 0 {
		// Pre-size so the buffer does not grow by doubling.
		buf.Grow(int(r.ContentLength) + bytes.MinRead)
	}
	if _, err := buf.ReadFrom(src); err != nil {
		r.Header.Set(ParseFailureHeader, "read error")
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error(), "invalid_request_body")
		return nil, false
	}

	if limit > 0 && int64(buf.Len()) > limit {
		e.rejectTooLarge(w, r)
		return nil, false
	}

	return buf.Bytes(), true
}

func (e *Handler) rejectTooLarge(w http.ResponseWriter, r *http.Request) {
	// The parse-failure header feeds the Traefik "parsefailure" metric label.
	r.Header.Set(ParseFailureHeader, "request_too_large")
	writeError(w, http.StatusRequestEntityTooLarge,
		fmt.Sprintf("request body exceeds the maximum of %d bytes", e.maxBodyBytes), "request_too_large")
}

// writeError writes an OpenAI-style error envelope.
func writeError(w http.ResponseWriter, status int, message string, code string) {
	body, err := json.Marshal(map[string]interface{}{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
	if err != nil {
		body = []byte(`{"error":{"message":"request rejected","type":"invalid_request_error"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	// Ask the server to close the connection so an unread upload does not stall the socket.
	w.Header().Set("Connection", "close")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (e *Handler) handleChatCompletionRequest(data []byte, r *http.Request) {
	request := chatCompletionRequest{}
	modelField := fmt.Sprintf("%v", e.requestFields["model"])
	if err := json.Unmarshal(data, &request); err != nil {
		r.Header.Set(ParseFailureHeader, err.Error())
		fmt.Println("Unable to unmarshal", err.Error())
		modelOnlyRequest := chatCompletionModelOnlyRequest{}
		err = json.Unmarshal(data, &modelOnlyRequest)
		if err != nil || len(modelField) < 1 {
			r.Header.Set(ParseFailureHeader, "Unknown model")
		} else {
			r.Header.Set(modelField, request.Model)
		}
	} else {
		if len(modelField) < 1 {
			r.Header.Set(ParseFailureHeader, "No model field configuration")
		} else {
			r.Header.Set(modelField, request.Model)
		}
	}

	if request.User != "" {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["user"]), request.User)
	}

	if request.Temperature != nil {
		field := fmt.Sprintf("%v", e.requestFields["temperature"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.Temperature))
		}
	}

	if request.MaxCompletionTokens != nil {
		field := fmt.Sprintf("%v", e.requestFields["max_completion_tokens"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.MaxCompletionTokens))
		}
	}

	if request.Logprobs != nil {
		field := fmt.Sprintf("%v", e.requestFields["logprobs"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.Logprobs))
		}
	}

	if request.TopLogprobs != nil {
		field := fmt.Sprintf("%v", e.requestFields["top_logprobs"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.TopLogprobs))
		}
	}

	if toolChoice, ok := request.ToolChoice.(string); ok {
		field := fmt.Sprintf("%v", e.requestFields["tool_choice"])
		if len(field) > 0 {
			r.Header.Set(field, toolChoice)
		}
	}

	if request.FrequencyPenalty != nil {
		field := fmt.Sprintf("%v", e.requestFields["frequency_penalty"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.FrequencyPenalty))
		}
	}

	if request.PresencePenalty != nil {
		field := fmt.Sprintf("%v", e.requestFields["presence_penalty"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.PresencePenalty))
		}
	}

	if request.TopP != nil {
		field := fmt.Sprintf("%v", e.requestFields["top_p"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.TopP))
		}
	}

	if request.Stream != nil {
		field := fmt.Sprintf("%v", e.requestFields["stream"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.Stream))
		}
	}

	if request.StreamOptions.IncludeUsage != nil {
		field := fmt.Sprintf("%v", e.requestFields["include_usage"])
		if len(field) > 0 {
			r.Header.Set(field, fmt.Sprintf("%v", *request.StreamOptions.IncludeUsage))
		}
	}
}

func (e *Handler) handleBatchRequest(data []byte, r *http.Request) {
	request := batchRequest{}
	if err := json.Unmarshal(data, &request); err != nil {
		r.Header.Set(ParseFailureHeader, err.Error())
		fmt.Println("Unable to unmarshal", err.Error())
	} else {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["completion_window"]), request.CompletionWindow)
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["oai_endpoint"]), request.Endpoint)
	}
}
