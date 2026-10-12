package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/assistant/keys"
	"github.com/lyricat/inua/internal/config"
)

func TestServeSettings(t *testing.T) {
	c := config.Assistant{ServeListen: "127.0.0.1:8794", Profile: "local", Profiles: []config.AIProfile{
		{Name: "local", Provider: "openai_chat_compatible", Model: "test"},
	}}
	if s, err := SettingsFrom(c, nil); err != nil || s.ServeListen != "" || s.ServeToken != "" {
		t.Fatalf("disabled endpoint: %v", err)
	}
	c.ServeEnable = true
	c.ServeListen = "not-an-address"
	if _, err := SettingsFrom(c, nil); err == nil {
		t.Fatal("invalid listen address accepted")
	}
	c.ServeListen = "127.0.0.1:8794"
	if _, err := SettingsFrom(c, nil); err == nil {
		t.Fatal("missing token accepted")
	}
	s, err := SettingsFrom(c, map[string]string{keys.ServeTokenKey: "test-token"})
	if err != nil || s.ServeListen != c.ServeListen || s.ServeToken != "test-token" {
		t.Fatalf("saved token not resolved: %v", err)
	}
	c.ServeEnable = false
	s, err = SettingsFrom(c, map[string]string{keys.ServeTokenKey: "test-token"})
	if err != nil || s.ServeListen != "" || s.ServeToken != "" || c.ServeListen != "127.0.0.1:8794" {
		t.Fatalf("turning off endpoint must retain its address without exposing HTTP: %v", err)
	}
}

// serveTest runs the real Runtime API against a scripted model, with isolated
// state and no connection to the desktop or an external model provider.
func serveTest(t *testing.T, model *fakeModel, own []Tool, approve Approver) (*Assistant, string, context.CancelFunc) {
	t.Helper()
	llm := httptest.NewServer(model)
	t.Cleanup(llm.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	a, err := New(Settings{
		Profiles:         []Profile{{Name: "test", Provider: "openai_chat_compatible", Model: "test", Endpoint: llm.URL + "/v1", APIKey: "test"}},
		AskBeforeChanges: true, StateDir: t.TempDir(), CacheDir: t.TempDir(), MaxSteps: 6,
		ServeListen: address, ServeToken: "test-token",
	}, own, approve)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("endpoint did not stop")
		}
	})
	url := "http://" + address + "/runtime"
	client := &http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		resp, err := client.Get(url + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return a, url, cancel
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("endpoint did not start")
	return nil, "", cancel
}

func runtimeRequest(t *testing.T, url, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func waitTask(t *testing.T, url, id, status string) map[string]any {
	t.Helper()
	var task map[string]any
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		code, out := runtimeRequest(t, url, "GET", "/tasks/"+id, "test-token", nil)
		task = out
		if code == http.StatusOK && out["status"] == status {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task did not reach %s: %v", status, task)
	return nil
}

func TestRuntimeAPIAuthToolsAndLocalIsolation(t *testing.T) {
	localStarted := make(chan struct{})
	localRelease := make(chan struct{})
	var changes atomic.Int32
	var asked atomic.Int32
	own := []Tool{
		{Name: "wait_local", Description: "Waits for the local test.", Run: func(ctx context.Context, _ map[string]any) (any, error) {
			close(localStarted)
			select {
			case <-localRelease:
				return "ok", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}},
		{Name: "change_desktop", Description: "Changes the fake desktop.", Risk: Change, Run: func(context.Context, map[string]any) (any, error) {
			changes.Add(1)
			return "changed", nil
		}},
	}
	model := &fakeModel{t: t, replies: []reply{
		{tool: "wait_local", args: `{}`},
		{tool: "change_desktop", args: `{}`}, {final: "Not changed."},
		{final: "Local complete."},
	}}
	a, url, _ := serveTest(t, model, own, func(_ context.Context, _, doing string) (bool, error) {
		if !strings.HasPrefix(doing, "Remote Console: ") {
			t.Error("remote approval not identified")
		}
		asked.Add(1)
		return false, nil
	})
	for _, token := range []string{"", "wrong-token"} {
		if code, _ := runtimeRequest(t, url, "GET", "/tasks", token, nil); code != http.StatusUnauthorized {
			t.Fatalf("unauthorized request: %d", code)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	localDone := make(chan error, 1)
	var localEvents atomic.Int32
	go func() {
		answer, err := a.Ask(ctx, "local question", Events{Tool: func(_, _ string) { localEvents.Add(1) }})
		if err == nil && answer != "Local complete." {
			t.Error("wrong local answer")
		}
		localDone <- err
	}()
	select {
	case <-localStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("local task did not start")
	}
	code, submitted := runtimeRequest(t, url, "POST", "/tasks", "test-token", map[string]string{"task": "remote question"})
	if code != http.StatusOK {
		t.Fatalf("submission: %d %v", code, submitted)
	}
	task := waitTask(t, url, submitted["id"].(string), "done")
	if task["result"].(map[string]any)["final"].(map[string]any)["output"] != "Not changed." {
		t.Fatalf("wrong remote result: %v", task)
	}
	if asked.Load() != 1 || changes.Load() != 0 || localEvents.Load() != 1 {
		t.Fatalf("approvals=%d changes=%d local events=%d", asked.Load(), changes.Load(), localEvents.Load())
	}
	close(localRelease)
	select {
	case err := <-localDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local task did not finish")
	}
	a.mu.Lock()
	history, _ := json.Marshal(a.history)
	a.mu.Unlock()
	if strings.Contains(string(history), "remote question") {
		t.Error("remote conversation entered local history")
	}
}

func TestRuntimeAPICancelsApproval(t *testing.T) {
	for _, stopServer := range []bool{false, true} {
		name := "task"
		if stopServer {
			name = "server"
		}
		t.Run(name, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			var changed atomic.Bool
			tool := Tool{Name: "change_desktop", Description: "Changes the fake desktop.", Risk: Always,
				Run: func(context.Context, map[string]any) (any, error) { changed.Store(true); return "changed", nil }}
			model := &fakeModel{t: t, replies: []reply{{tool: tool.Name, args: `{}`}}}
			_, url, cancel := serveTest(t, model, []Tool{tool}, func(ctx context.Context, _, _ string) (bool, error) {
				close(started)
				<-ctx.Done()
				close(stopped)
				return false, ctx.Err()
			})
			code, submitted := runtimeRequest(t, url, "POST", "/tasks", "test-token", map[string]string{"task": "change"})
			if code != http.StatusOK {
				t.Fatalf("submission: %d", code)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("approval did not start")
			}
			if stopServer {
				cancel()
			} else {
				id := submitted["id"].(string)
				code, _ := runtimeRequest(t, url, "POST", "/tasks/"+id+"/stop", "test-token", map[string]string{"task_id": id})
				if code != http.StatusOK {
					t.Fatalf("stop: %d", code)
				}
				waitTask(t, url, id, "canceled")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("approval was not canceled")
			}
			if changed.Load() {
				t.Error("canceled tool changed the desktop")
			}
		})
	}
}
