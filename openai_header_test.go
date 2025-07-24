package traefik_openai_header

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletionHeaders_ServeHTTP(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestFields map[string]string
		want          string
		error         bool
	}{
		{
			name:          "empty",
			input:         "",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Parse-Failure",
			error:         false,
		},
		{
			name:          "non json",
			input:         "INVALID JSON",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Parse-Failure",
			error:         false,
		},
		{
			name:          "model",
			input:         "{\"model\": \"test\"}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "model-with-parse-failure",
			input:         "{\"model\": \"test\",\"unknownfield\":\"value\"}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "2 models",
			input:         "{\"model\": \"test\", \"model\": \"test2\"}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "user",
			input:         "{\"user\": \"test\"}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-User",
			error:         false,
		},
		{
			name:          "temperature",
			input:         "{\"temperature\": 1.0}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Temperature",
			error:         false,
		},
		{
			name:          "openai-default",
			input:         "{\n  \"model\": \"gpt-4.1\",\n  \"messages\": [\n    {\n      \"role\": \"developer\",\n      \"content\": \"You are a helpful assistant.\"\n    },\n    {\n      \"role\": \"user\",\n      \"content\": \"Hello!\"\n    }\n  ]\n}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "openai-image",
			input:         "{\n    \"model\": \"gpt-4.1\",\n    \"messages\": [\n      {\n        \"role\": \"user\",\n        \"content\": [\n          {\n            \"type\": \"text\",\n            \"text\": \"What is in this image?\"\n          },\n          {\n            \"type\": \"image_url\",\n            \"image_url\": {\n              \"url\": \"https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg\"\n            }\n          }\n        ]\n      }\n    ],\n    \"max_tokens\": 300\n  }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "openai-stream",
			input:         "{\n    \"model\": \"gpt-4.1\",\n    \"messages\": [\n      {\n        \"role\": \"developer\",\n        \"content\": \"You are a helpful assistant.\"\n      },\n      {\n        \"role\": \"user\",\n        \"content\": \"Hello!\"\n      }\n    ],\n    \"stream\": true\n  }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Stream",
			error:         false,
		},
		{
			name:          "openai-functions",
			input:         "{\n  \"model\": \"gpt-4.1\",\n  \"messages\": [\n    {\n      \"role\": \"user\",\n      \"content\": \"What is the weather like in Boston today?\"\n    }\n  ],\n  \"tools\": [\n    {\n      \"type\": \"function\",\n      \"function\": {\n        \"name\": \"get_current_weather\",\n        \"description\": \"Get the current weather in a given location\",\n        \"parameters\": {\n          \"type\": \"object\",\n          \"properties\": {\n            \"location\": {\n              \"type\": \"string\",\n              \"description\": \"The city and state, e.g. San Francisco, CA\"\n            },\n            \"unit\": {\n              \"type\": \"string\",\n              \"enum\": [\"celsius\", \"fahrenheit\"]\n            }\n          },\n          \"required\": [\"location\"]\n        }\n      }\n    }\n  ],\n  \"tool_choice\": \"auto\"\n}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Tool-Choice",
			error:         false,
		},
		{
			name:          "openai-functions-toolchoice-object",
			input:         "{\n  \"model\": \"gpt-4.1\",\n  \"messages\": [\n    {\n      \"role\": \"user\",\n      \"content\": \"What is the weather like in Boston today?\"\n    }\n  ],\n  \"tools\": [\n    {\n      \"type\": \"function\",\n      \"function\": {\n        \"name\": \"get_current_weather\",\n        \"description\": \"Get the current weather in a given location\",\n        \"parameters\": {\n          \"type\": \"object\",\n          \"properties\": {\n            \"location\": {\n              \"type\": \"string\",\n              \"description\": \"The city and state, e.g. San Francisco, CA\"\n            },\n            \"unit\": {\n              \"type\": \"string\",\n              \"enum\": [\"celsius\", \"fahrenheit\"]\n            }\n          },\n          \"required\": [\"location\"]\n        }\n      }\n    }\n  ],\n  \"tool_choice\": {\"type\":\"file_search\"}\n}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "openai-logprobs",
			input:         "{\n    \"model\": \"gpt-4.1\",\n    \"messages\": [\n      {\n        \"role\": \"user\",\n        \"content\": \"Hello!\"\n      }\n    ],\n    \"logprobs\": 5,\n    \"top_logprobs\": 2\n  }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Top-Logprobs",
			error:         false,
		},
		{
			name:          "openai-logprobs-null",
			input:         "{\n    \"model\": \"gpt-4.1\",\n    \"messages\": [\n      {\n        \"role\": \"user\",\n        \"content\": \"Hello!\"\n      }\n    ],\n    \"logprobs\": null,\n    \"top_logprobs\": 2\n  }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Model",
			error:         false,
		},
		{
			name:          "openai-temp-topp-presence-penalty",
			input:         "{\"model\": \"deepseek-r1-0528\", \"messages\": [{\"role\": \"user\", \"content\": \"What is the capital of New York?\"}], \"temperature\": 0.7, \"top_p\": 0.6, \"presence_penalty\" : 0.0}",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Presence-Penalty",
			error:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vh := validationHandler{
				t:     t,
				want:  tt.want,
				error: tt.error,
			}

			e, err := New(nil, vh, newConfig(), tt.name)
			if err != nil {
				t.Errorf("Failed initializing Handler: %s", err)
				t.FailNow()
			}

			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tt.input)))
			e.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/completions", strings.NewReader(tt.input)))

			if recorder.Code != http.StatusOK && !tt.error {
				t.Errorf("expected status code 200 but got %d", recorder.Code)
				t.FailNow()
			}
		})
	}
}

func TestBatchHeaders_ServeHTTP(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestFields map[string]string
		want          string
		error         bool
	}{
		{
			name:          "empty",
			input:         "",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Parse-Failure",
			error:         false,
		},
		{
			name:          "non json",
			input:         "INVALID JSON",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Parse-Failure",
			error:         false,
		},
		{
			name:          "batch-with-completion-window",
			input:         "{\n    \"input_file_id\": \"file-abc123\",\n    \"endpoint\": \"/v1/chat/completions\",\n    \"completion_window\": \"24h\"\n  }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Completion-Window",
			error:         false,
		},
		{
			name:          "batch-with-endpoint-and-metadata",
			input:         "{\n    \"input_file_id\": \"file-abc123\",\n    \"endpoint\": \"/v1/chat/completions\",\n    \"completion_window\": \"24h\"\n, \"metadata\": {\n    \"customer_id\": \"user_123456789\",\n    \"batch_description\": \"Nightly eval job\"\n }\n }",
			requestFields: map[string]string{},
			want:          "X-OpenAI-Endpoint",
			error:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vh := validationHandler{
				t:     t,
				want:  tt.want,
				error: tt.error,
			}

			e, err := New(nil, vh, newConfig(), tt.name)
			if err != nil {
				t.Errorf("Failed initializing Handler: %s", err)
				t.FailNow()
			}

			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/batches", strings.NewReader(tt.input)))

			if recorder.Code != http.StatusOK && !tt.error {
				t.Errorf("expected status code 200 but got %d", recorder.Code)
				t.FailNow()
			}
		})
	}
}

type String string

func (s String) AsReader() io.Reader {
	return io.NopCloser(strings.NewReader(string(s)))
}

func newConfig() *Config {
	c := CreateConfig()
	c.RequestURIRegex = "/v1.*/completions"
	return c
}

type validationHandler struct {
	t     *testing.T
	want  string
	error bool
}

func (vh validationHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-OpenAI-Parse-Failure") != "" && vh.want != "X-OpenAI-Parse-Failure" {
		vh.t.Errorf("not expected parse failure %v", r.Header.Get("X-OpenAI-Parse-Failure"))
		vh.t.FailNow()
	}

	if r.Header.Get(vh.want) == "" {
		vh.t.Errorf("expected value for header %v", vh.want)
		vh.t.FailNow()
	}
}
