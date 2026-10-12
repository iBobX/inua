package assistant

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/quailyquaily/mistermorph/integration"
)

// Serve exposes Console's Runtime API with the desktop's tools and approval
// policy. Cancellation stops its tasks and waits for them to finish.
func (a *Assistant) Serve(ctx context.Context) error {
	if a.set.ServeListen == "" {
		return errors.New("assistant: serve_listen is not set")
	}
	if a.set.ServeToken == "" {
		return errors.New("assistant: the Runtime API needs a token")
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := a.rt.ServeRuntimeAPI(ctx, integration.RuntimeAPIOptions{Registry: a.reg})
		// Upstream closes its listener asynchronously. A settings reload
		// may try to bind just before the previous listener has closed.
		if !errors.Is(err, syscall.EADDRINUSE) || attempt == 4 {
			if err != nil {
				return fmt.Errorf("assistant Runtime API: %w", err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
