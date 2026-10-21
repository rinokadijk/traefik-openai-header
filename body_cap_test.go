package traefik_openai_header

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// nextRecorder is the downstream handler. It records whether it ran and what body it saw.
type nextRecorder struct {
	called bool
	body   []byte
	header http.Header
	length int64
}

func (n *nextRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	n.header = r.Header.Clone()
	n.length = r.ContentLength
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n.body = data
	w.WriteHeader(http.StatusOK)
}

func cappedConfig(limit int64) *Config {
	c := CreateConfig()
	c.RequestURIRegex = "/v1.*/completions"
	c.MaxBodyBytes = limit
	return c
}

func newHandler(t *testing.T, cfg *Config, next http.Handler) http.Handler {
	t.Helper()
	h, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return h
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response body is not the error envelope: %v: %s", err, rec.Body.String())
	}
	return env.Error.Type, env.Error.Code
}

func TestNew_InvalidRegexReturnsError(t *testing.T) {
	cfg := CreateConfig()
	cfg.RequestURIRegex = "("
	if _, err := New(context.Background(), &nextRecorder{}, cfg, "test"); err == nil {
		t.Fatal("New accepted an invalid requestUriRegex")
	}
	cfg = CreateConfig()
	cfg.BatchUriRegex = "("
	if _, err := New(context.Background(), &nextRecorder{}, cfg, "test"); err == nil {
		t.Fatal("New accepted an invalid batchUriRegex")
	}
}

func TestNew_NegativeCapReturnsError(t *testing.T) {
	cfg := cappedConfig(-1)
	if _, err := New(context.Background(), &nextRecorder{}, cfg, "test"); err == nil {
		t.Fatal("New accepted a negative maxBodyBytes")
	}
}

func TestCap_ContentLengthAboveCapIs413(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(1024), next)
	body := bodyWithImage("gpt-4.1", 2048)
	req := postJSON("/v1/chat/completions", bytes.NewReader(body)) // sets Content-Length
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body %s", rec.Code, rec.Body.String())
	}
	if next.called {
		t.Fatal("next handler ran for an oversize request")
	}
	typ, code := decodeError(t, rec)
	if typ != "invalid_request_error" || code != "request_too_large" {
		t.Errorf("error type/code = %q/%q, want invalid_request_error/request_too_large", typ, code)
	}
	if got := req.Header.Get(ParseFailureHeader); got != "request_too_large" {
		t.Errorf("%s = %q, want request_too_large", ParseFailureHeader, got)
	}
}

func TestCap_ChunkedAboveCapIs413(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(1024), next)
	body := bodyWithImage("gpt-4.1", 2048)
	req := postJSON("/v1/chat/completions", io.NopCloser(bytes.NewReader(body))) // ContentLength -1
	if req.ContentLength != -1 {
		t.Fatalf("test setup: ContentLength = %d, want -1", req.ContentLength)
	}
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if next.called {
		t.Fatal("next handler ran for an oversize chunked request")
	}
}

func TestCap_ExactlyAtCapIsForwarded(t *testing.T) {
	next := &nextRecorder{}
	body := bodyWithImage("gpt-4.1", 500)
	h := newHandler(t, cappedConfig(int64(len(body))), next)
	req := postJSON("/v1/chat/completions", io.NopCloser(bytes.NewReader(body))) // chunked path too
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !next.called {
		t.Fatalf("status = %d, next called = %v; want 200 and next called", rec.Code, next.called)
	}
	if !bytes.Equal(next.body, body) {
		t.Errorf("downstream body differs: got %d bytes, want %d", len(next.body), len(body))
	}
	if next.length != int64(len(body)) {
		t.Errorf("downstream ContentLength = %d, want %d", next.length, len(body))
	}
	if next.header.Get("X-OpenAI-Model") != "gpt-4.1" {
		t.Errorf("X-OpenAI-Model = %q, want gpt-4.1", next.header.Get("X-OpenAI-Model"))
	}
}

func TestCap_ZeroMeansUnlimited(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(0), next)
	body := bodyWithImage("gpt-4.1", 5<<20)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/chat/completions", bytes.NewReader(body)))

	if rec.Code != http.StatusOK || !next.called {
		t.Fatalf("status = %d, next called = %v; want 200 and next called", rec.Code, next.called)
	}
	if len(next.body) != len(body) {
		t.Errorf("downstream body = %d bytes, want %d", len(next.body), len(body))
	}
}

func TestDefaultCap_LargeImagePartUnderCapSetsModel(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, defaultConfig(), next) // default cap 64 MiB
	body := bodyWithImage("gpt-4.1", 40<<20)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/chat/completions", bytes.NewReader(body)))

	if rec.Code != http.StatusOK || !next.called {
		t.Fatalf("status = %d, next called = %v; want 200 and next called", rec.Code, next.called)
	}
	if next.header.Get("X-OpenAI-Model") != "gpt-4.1" || next.header.Get("X-OpenAI-Stream") != "true" {
		t.Errorf("headers: model=%q stream=%q", next.header.Get("X-OpenAI-Model"), next.header.Get("X-OpenAI-Stream"))
	}
	if !bytes.Equal(next.body, body) {
		t.Error("downstream body is not byte-equal to the request body")
	}
}

func TestDefaultCap_BodyAboveDefaultIs413(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, defaultConfig(), next)
	body := bodyWithImage("gpt-4.1", 64<<20) // 64 MiB of image plus JSON framing > 64 MiB
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/chat/completions", bytes.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge || next.called {
		t.Fatalf("status = %d, next called = %v; want 413 and next not called", rec.Code, next.called)
	}
}

func TestInvalidJSON_ForwardsWithParseFailure(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(1024), next)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/chat/completions", strings.NewReader("INVALID JSON")))

	if !next.called {
		t.Fatal("next handler did not run for invalid JSON")
	}
	if string(next.body) != "INVALID JSON" {
		t.Errorf("downstream body = %q, want unchanged", next.body)
	}
	if next.header.Get(ParseFailureHeader) == "" {
		t.Error("parse-failure header not set for invalid JSON")
	}
}

func TestEmptyBody_ForwardsWithParseFailure(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(1024), next)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/chat/completions", strings.NewReader("")))

	if !next.called {
		t.Fatal("next handler did not run for an empty body")
	}
	if got := next.header.Get(ParseFailureHeader); got != "empty body" {
		t.Errorf("parse-failure = %q, want \"empty body\"", got)
	}
}

func TestBatch_UnderAndOverCap(t *testing.T) {
	small := `{"completion_window":"24h","endpoint":"/v1/chat/completions"}`
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(int64(len(small))), next)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, postJSON("/v1/batches", strings.NewReader(small)))

	if rec.Code != http.StatusOK || next.header.Get("X-OpenAI-Completion-Window") != "24h" {
		t.Fatalf("small batch: status %d, completion window %q", rec.Code, next.header.Get("X-OpenAI-Completion-Window"))
	}

	next = &nextRecorder{}
	h = newHandler(t, cappedConfig(int64(len(small))), next)
	rec = httptest.NewRecorder()
	big := `{"completion_window":"24h","endpoint":"/v1/chat/completions","input_file_id":"file-abc123"}`

	h.ServeHTTP(rec, postJSON("/v1/batches", strings.NewReader(big)))

	if rec.Code != http.StatusRequestEntityTooLarge || next.called {
		t.Fatalf("big batch: status %d, next called %v; want 413 and next not called", rec.Code, next.called)
	}
}

// failingReader fails the test if anything reads it, or returns err on Read if err is set.
type failingReader struct {
	t   *testing.T
	err error
}

func (f failingReader) Read([]byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.t.Fatal("request body was read for a request the plugin must not touch")
	return 0, io.EOF
}

func TestNonMatchingRequests_BodyNeverRead(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodGet, "/v1/chat/completions"},
		{http.MethodPost, "/v1/models"},
		{http.MethodPost, "/v1/embeddings"},
	}
	for _, c := range cases {
		called := false
		// This next handler must not read the body: only the plugin is under test.
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
		h := newHandler(t, cappedConfig(1024), next)
		req := httptest.NewRequest(c.method, c.path, io.NopCloser(failingReader{t: t}))
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if !called {
			t.Errorf("%s %s: next handler did not run", c.method, c.path)
		}
	}
}

func TestReadError_Is400AndStops(t *testing.T) {
	next := &nextRecorder{}
	h := newHandler(t, cappedConfig(1024), next)
	req := postJSON("/v1/chat/completions", io.NopCloser(failingReader{t: t, err: errors.New("boom")}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if next.called {
		t.Fatal("next handler ran after a body read error")
	}
	decodeError(t, rec)
}
