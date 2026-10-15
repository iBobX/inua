package desk

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Watch gives only what changed after the first read: a new notification
// each, the focused window once, the system's state without its clock.
func TestWatch(t *testing.T) {
	var mu sync.Mutex
	notes := `{"notifications":[{"id":1,"summary":"old"}]}`
	status := `{"wifi":true,"time":"09:00"}`
	d, _ := fakeDesktop()
	d.Call = func(_ context.Context, target, fn string, _ ...string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if target == "notifications" {
			return notes, nil
		}
		return status, nil
	}
	set := func(p *string, v string) { mu.Lock(); *p = v; mu.Unlock() }
	watch := func(topic string) (func() []any, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		var got []any
		var gmu sync.Mutex
		go Watch(ctx, d, topic, 10*time.Millisecond, func(v any) { gmu.Lock(); got = append(got, v); gmu.Unlock() })
		return func() []any { gmu.Lock(); defer gmu.Unlock(); return append([]any(nil), got...) }, cancel
	}

	got, stop := watch("notification")
	time.Sleep(40 * time.Millisecond)
	set(&notes, `{"notifications":[{"id":3,"summary":"newest"},{"id":2,"summary":"new"},{"id":1,"summary":"old"}]}`)
	time.Sleep(60 * time.Millisecond)
	stop()
	if g := got(); len(g) != 2 || g[0].(map[string]any)["summary"] != "new" || g[1].(map[string]any)["summary"] != "newest" {
		t.Errorf("notifications %v", g)
	}

	got, stop = watch("system")
	time.Sleep(40 * time.Millisecond)
	set(&status, `{"wifi":true,"time":"09:01"}`) // only the clock
	time.Sleep(40 * time.Millisecond)
	set(&status, `{"wifi":false,"time":"09:01"}`)
	time.Sleep(60 * time.Millisecond)
	stop()
	if g := got(); len(g) != 1 || g[0].(map[string]any)["wifi"] != false {
		t.Errorf("system %v", g)
	}

	// The fake's windows do not change: nothing.
	got, stop = watch("window")
	time.Sleep(60 * time.Millisecond)
	stop()
	if g := got(); len(g) != 0 {
		t.Errorf("window %v", g)
	}
	if err := Watch(context.Background(), d, "weather", time.Second, nil); err == nil {
		t.Error("an unknown topic")
	}
}
