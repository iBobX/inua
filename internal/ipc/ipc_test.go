package ipc

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, path, func(_ context.Context, r Request) Response {
			if r.Target != "launcher" {
				return Response{Error: "no target " + r.Target}
			}
			return Response{OK: true, Result: r.Function + ":" + strings.Join(r.Args, ",")}
		})
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	cctx, ccancel := context.WithTimeout(context.Background(), time.Second)
	defer ccancel()
	var resp Response
	var err error
	for range 50 { // wait for the listener
		if resp, err = Call(cctx, path, Request{Op: "call", Target: "launcher", Function: "open", Args: []string{"a", "b"}}); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || resp.Result != "open:a,b" {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	if _, err := Call(cctx, path, Request{Op: "call", Target: "nope"}); err == nil || !strings.Contains(err.Error(), "no target") {
		t.Fatalf("expected handler error, got %v", err)
	}

	// A second server on the same live socket must refuse to start.
	if err := Serve(context.Background(), path, nil); err == nil || !strings.Contains(err.Error(), "another instance") {
		t.Fatalf("second Serve: %v", err)
	}
}
