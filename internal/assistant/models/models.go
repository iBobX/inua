// Package models lists the models a provider offers, for Settings → AI:
// the provider's own list (GET …/models), as Mister Morph's Console asks
// it, without Mister Morph. It does not import GTK.
package models

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// endpoints are the providers' own addresses, as Mister Morph has them.
var endpoints = map[string]string{
	"openai":          "https://api.openai.com",
	"anthropic":       "https://api.anthropic.com",
	"gemini":          "https://generativelanguage.googleapis.com",
	"xai":             "https://api.x.ai",
	"deepseek":        "https://api.deepseek.com",
	"kimi":            "https://api.moonshot.cn",
	"openrouter":      "https://openrouter.ai/api/v1",
	"groq":            "https://api.groq.com/openai/v1",
	"mistermorph_pro": "https://router.mistermorph.com/api/v1",
	"magpie":          "http://127.0.0.1:3425/v1",
	"sakana":          "https://api.sakana.ai/v1",
}

// ErrNoEndpoint is a compatible provider without its server's address.
var ErrNoEndpoint = errors.New("models: no endpoint to ask")

// ErrNoList is a provider that does not list its models: TypeSafe.
var ErrNoList = errors.New("models: the provider does not list its models")

// Client asks; its timeout bounds each request.
var Client = &http.Client{Timeout: 15 * time.Second}

type model struct {
	id      string
	created int64 // Unix seconds; 0 when the provider does not say
}

// List lists provider's models at endpoint ("" for the provider's own)
// with key ("" for none), newest first, those without a date last in
// name order.
func List(ctx context.Context, provider, endpoint, key string) ([]string, error) {
	if provider == "typesafe" {
		return nil, ErrNoList
	}
	base := strings.TrimRight(strings.TrimSpace(cmp.Or(endpoint, endpoints[provider])), "/")
	if base == "" {
		return nil, ErrNoEndpoint
	}
	var list []model
	var err error
	switch provider {
	case "gemini":
		list, err = gemini(ctx, strings.TrimSuffix(base, "/v1beta"), key)
	case "anthropic", "anthropic_compatible":
		list, err = anthropic(ctx, base, key)
	default: // the OpenAI catalog: OpenAI and the servers like it
		list, err = openai(ctx, base, key)
	}
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(list, func(a, b model) int {
		return cmp.Or(cmp.Compare(b.created, a.created), cmp.Compare(a.id, b.id))
	})
	ids := make([]string, 0, len(list))
	for _, m := range list {
		if m.id != "" && !slices.Contains(ids, m.id) {
			ids = append(ids, m.id)
		}
	}
	return ids, nil
}

// ModelsURL is base's model list: base/v1/models, unless base already
// ends in /v1 or /models.
func ModelsURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("models: %q is not an http(s) address", base)
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(u.Path, "/models"):
	case strings.HasSuffix(u.Path, "/v1"):
		u.Path += "/models"
	default:
		u.Path += "/v1/models"
	}
	return u.String(), nil
}

func openai(ctx context.Context, base, key string) ([]model, error) {
	u, err := ModelsURL(base)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID      string          `json:"id"`
			Created json.RawMessage `json:"created"`
		} `json:"data"`
	}
	h := http.Header{}
	if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	if err := get(ctx, u, h, &out); err != nil {
		return nil, err
	}
	var list []model
	for _, m := range out.Data {
		list = append(list, model{strings.TrimSpace(m.ID), created(m.Created)})
	}
	return list, nil
}

func anthropic(ctx context.Context, base, key string) ([]model, error) {
	u, err := ModelsURL(base)
	if err != nil {
		return nil, err
	}
	h := http.Header{"Anthropic-Version": {"2023-06-01"}}
	if key != "" {
		h.Set("X-Api-Key", key)
	}
	var list []model
	after := ""
	for range 20 { // pages
		q := url.Values{"limit": {"1000"}}
		if after != "" {
			q.Set("after_id", after)
		}
		var out struct {
			Data []struct {
				ID        string `json:"id"`
				CreatedAt string `json:"created_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := get(ctx, u+"?"+q.Encode(), h, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Data {
			var at int64
			if t, err := time.Parse(time.RFC3339, m.CreatedAt); err == nil {
				at = t.Unix()
			}
			list = append(list, model{strings.TrimSpace(m.ID), at})
		}
		if !out.HasMore || out.LastID == "" {
			break
		}
		after = out.LastID
	}
	return list, nil
}

func gemini(ctx context.Context, base, key string) ([]model, error) {
	h := http.Header{}
	if key != "" {
		h.Set("X-Goog-Api-Key", key)
	}
	var list []model
	token := ""
	for range 20 { // pages
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var out struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := get(ctx, base+"/v1beta/models?"+q.Encode(), h, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Models {
			list = append(list, model{id: strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")})
		}
		if token = out.NextPageToken; token == "" {
			break
		}
	}
	return list, nil
}

// get reads u's JSON into out; an error status says what the server did.
func get(ctx context.Context, u string, h http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header = h
	req.Header.Set("Accept", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		text := strings.Join(strings.Fields(string(body)), " ")
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		return fmt.Errorf("models: %s: %s", resp.Status, text)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("models: not a model list: %w", err)
	}
	return nil
}

// created reads a model's created time: Unix seconds, or milliseconds, as
// a number or a string; else 0.
func created(raw json.RawMessage) int64 {
	v, err := strconv.ParseFloat(strings.Trim(strings.TrimSpace(string(raw)), `"`), 64)
	if err != nil || v <= 0 {
		return 0
	}
	if v > 1e12 {
		v /= 1000
	}
	return int64(v)
}
