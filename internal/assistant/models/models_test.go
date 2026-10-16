package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each catalog is asked at its path, with its key header, and read newest
// first, models without a date last in name order.
func TestList(t *testing.T) {
	for _, tc := range []struct {
		provider, path, header, body, want string
	}{
		{"openai_chat_compatible", "/v1/models", "Authorization",
			`{"data":[{"id":"b"},{"id":"old","created":1600000000},{"id":"new","created":"1767225600000"},{"id":"a"},{"id":"a"}]}`,
			"[new old a b]"},
		{"anthropic_compatible", "/v1/models", "X-Api-Key",
			`{"data":[{"id":"claude-old","created_at":"2025-01-01T00:00:00Z"},{"id":"claude-new","created_at":"2026-01-01T00:00:00Z"}],"has_more":false}`,
			"[claude-new claude-old]"},
		{"gemini", "/v1beta/models", "X-Goog-Api-Key",
			`{"models":[{"name":"models/gemini-b"},{"name":"models/gemini-a"}]}`,
			"[gemini-a gemini-b]"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Header.Get(tc.header) == "" {
					http.Error(w, "wrong request "+r.URL.String(), http.StatusBadRequest)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			got, err := List(context.Background(), tc.provider, srv.URL, "key")
			if err != nil || fmt.Sprint(got) != tc.want {
				t.Errorf("%q %v, want %s", got, err, tc.want)
			}
		})
	}
}

// Anthropic's list comes in pages.
func TestAnthropicPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after_id") == "" {
			fmt.Fprint(w, `{"data":[{"id":"one"}],"has_more":true,"last_id":"one"}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"two"}],"has_more":false}`)
	}))
	defer srv.Close()
	if got, err := List(context.Background(), "anthropic", srv.URL, "k"); err != nil || fmt.Sprint(got) != "[one two]" {
		t.Errorf("%q %v", got, err)
	}
}

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := List(context.Background(), "openai", srv.URL, "k"); err == nil || err.Error() != `models: 401 Unauthorized: {"error": "bad key"}` {
		t.Errorf("unauthorized: %v", err)
	}
	if _, err := List(context.Background(), "openai_chat_compatible", "", ""); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("no endpoint: %v", err)
	}
	if _, err := List(context.Background(), "typesafe", "", "k"); !errors.Is(err, ErrNoList) {
		t.Errorf("typesafe: %v", err)
	}
}

func TestModelsURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.openai.com":        "https://api.openai.com/v1/models",
		"http://127.0.0.1:3425/v1/":     "http://127.0.0.1:3425/v1/models",
		"https://openrouter.ai/api/v1":  "https://openrouter.ai/api/v1/models",
		"https://x.example/v1/models?a": "https://x.example/v1/models",
	} {
		if got, err := ModelsURL(in); err != nil || got != want {
			t.Errorf("%s: %s %v, want %s", in, got, err, want)
		}
	}
	if _, err := ModelsURL("ftp://x"); err == nil {
		t.Error("ftp")
	}
}
