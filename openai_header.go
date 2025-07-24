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

// Config the plugin configuration.
type Config struct {
	RequestFields          map[string]interface{} `json:"requestFields"`
	RequestURIRegex        string                 `json:"requestUriRegex"`
	ChatCompletionUriRegex string                 `json:"chatCompletionUriRegex"`
	BatchUriRegex          string                 `json:"batchUriRegex"`
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
	fields["completion_window"] = "X-OpenAI-Completion-Window"
	fields["endpoint"] = "X-OpenAI-Endpoint"
	return &Config{
		RequestFields:          fields,
		RequestURIRegex:        "/v1/chat/completions",
		ChatCompletionUriRegex: "/v1/chat/completions",
		BatchUriRegex:          "/v1/batches",
	}
}

// Handler contains the config for the plugin
type Handler struct {
	name                 string
	next                 http.Handler
	requestFields        map[string]interface{}
	requestURIRegex      string
	batchRequestURIRegex string
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

	return &Handler{
		name:                 name,
		requestFields:        config.RequestFields,
		requestURIRegex:      chatCompletionUri,
		batchRequestURIRegex: config.BatchUriRegex,
		next:                 next,
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
	Messages            json.RawMessage   `json:"messages,omitempty"`
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
	isChatCompletionRequest, err := regexp.MatchString(e.requestURIRegex, r.RequestURI)
	if err != nil {
		fmt.Println("Error while matching RequestURI", err.Error())
	}

	isBatchRequest, err := regexp.MatchString(e.batchRequestURIRegex, r.RequestURI)
	if err != nil {
		fmt.Println("Error while matching BatchRequestURI", err.Error())
	}

	if (isChatCompletionRequest || isBatchRequest) && r.Method == "POST" {
		var body bytes.Buffer
		tee := io.TeeReader(r.Body, &body)

		data, err := io.ReadAll(tee)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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

		r.Body = io.NopCloser(bytes.NewReader(data))
	}

	e.next.ServeHTTP(w, r)
}

func (e *Handler) handleChatCompletionRequest(data []byte, r *http.Request) {
	request := chatCompletionRequest{}
	if err := json.Unmarshal(data, &request); err != nil {
		r.Header.Set(ParseFailureHeader, err.Error())
		fmt.Println("Unable to unmarshal", err.Error())
		modelOnlyRequest := chatCompletionModelOnlyRequest{}
		err = json.Unmarshal(data, &modelOnlyRequest)
		if err != nil {
			r.Header.Set(ParseFailureHeader, "Unknown model")
		} else {
			r.Header.Set(fmt.Sprintf("%v", e.requestFields["model"]), request.Model)
		}
	} else {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["model"]), request.Model)
	}

	if request.User != "" {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["user"]), request.User)
	}

	if request.Temperature != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["temperature"]), fmt.Sprintf("%v", *request.Temperature))
	}

	if request.MaxCompletionTokens != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["max_completion_tokens"]), fmt.Sprintf("%v", *request.MaxCompletionTokens))
	}

	if request.Logprobs != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["logprobs"]), fmt.Sprintf("%v", *request.Logprobs))
	}

	if request.TopLogprobs != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["top_logprobs"]), fmt.Sprintf("%v", *request.TopLogprobs))
	}

	if toolChoice, ok := request.ToolChoice.(string); ok {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["tool_choice"]), toolChoice)
	}

	if request.FrequencyPenalty != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["frequency_penalty"]), fmt.Sprintf("%v", *request.FrequencyPenalty))
	}

	if request.PresencePenalty != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["presence_penalty"]), fmt.Sprintf("%v", *request.PresencePenalty))
	}

	if request.TopP != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["top_p"]), fmt.Sprintf("%v", *request.TopP))
	}

	if request.Stream != nil {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["stream"]), fmt.Sprintf("%v", *request.Stream))
	}
}

func (e *Handler) handleBatchRequest(data []byte, r *http.Request) {
	request := batchRequest{}
	if err := json.Unmarshal(data, &request); err != nil {
		r.Header.Set(ParseFailureHeader, err.Error())
		fmt.Println("Unable to unmarshal", err.Error())
	} else {
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["completion_window"]), request.CompletionWindow)
		r.Header.Set(fmt.Sprintf("%v", e.requestFields["endpoint"]), request.Endpoint)
	}
}
