package system

import (
	"context"
	"fmt"
)

// CanHibernate asks logind whether the machine can hibernate: set up for
// it and with room to resume from ("yes", or "challenge": allowed once the
// user is asked). Over its own connection, at most callLimit long: call it
// off the main loop.
func CanHibernate(dial Dialer) (bool, error) {
	conn, err := dial()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	var answer string
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.login1.Manager.CanHibernate", 0).Store(&answer)
	if err != nil {
		return false, fmt.Errorf("logind: CanHibernate: %w", err)
	}
	return answer == "yes" || answer == "challenge", nil
}
