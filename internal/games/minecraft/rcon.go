package minecraft

import (
	"fmt"
	"time"

	"github.com/gorcon/rcon"
)

// rconTimeout bounds both connecting and waiting for a reply, so a stuck
// server can't hang a status request.
const rconTimeout = 5 * time.Second

// NewRCONQuery returns a Query function (see Config.Query) that sends one
// command over RCON to addr, authenticating with password.
//
// It returns a closure: a function that "closes over" addr and password,
// remembering them for every later call. Each call opens a fresh
// connection and closes it afterwards. That's a little slower than keeping
// one open, but there's nothing to go stale when the server restarts.
func NewRCONQuery(addr, password string) func(cmd string) (string, error) {
	return func(cmd string) (string, error) {
		conn, err := rcon.Dial(addr, password,
			rcon.SetDialTimeout(rconTimeout),
			rcon.SetDeadline(rconTimeout),
		)
		if err != nil {
			return "", fmt.Errorf("rcon dial %s: %w", addr, err)
		}
		// defer runs when the function returns, however it returns.
		defer conn.Close()

		reply, err := conn.Execute(cmd)
		if err != nil {
			return "", fmt.Errorf("rcon %q: %w", cmd, err)
		}
		return reply, nil
	}
}
