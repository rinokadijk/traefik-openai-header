package traefik_openai_header

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This file has no dependency on the v0.2.0 config fields, so the same benchmark
// runs against v0.1.0 for a before/after comparison.

func postJSON(path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// bodyWithImage builds a chat body whose single message carries a base64 image of imageBytes bytes.
func bodyWithImage(model string, imageBytes int) []byte {
	var b bytes.Buffer
	b.WriteString(`{"messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,`)
	b.Write(bytes.Repeat([]byte("A"), imageBytes))
	b.WriteString(`"}}]}],"model":"`)
	b.WriteString(model)
	b.WriteString(`","stream":true}`)
	return b.Bytes()
}

func BenchmarkServeHTTP_40MB(b *testing.B) {
	body := bodyWithImage("gpt-4.1", 40<<20)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	h, err := New(context.Background(), next, defaultConfig(), "bench")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := postJSON("/v1/chat/completions", bytes.NewReader(body))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
