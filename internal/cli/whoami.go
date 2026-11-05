package cli

import (
	"fmt"
	"io"
	"strings"
)

type keyList struct {
	Items []struct {
		Name          string `json:"name"`
		DisplayPrefix string `json:"display_prefix"`
	} `json:"items"`
}

// keyName finds the name of the key by its display prefix, the 8 characters
// after brd_.
func (l keyList) keyName(key string) string {
	prefix := strings.TrimPrefix(key, "brd_")
	if len(prefix) < 8 {
		return "unknown"
	}
	for _, k := range l.Items {
		if k.DisplayPrefix == prefix[:8] {
			return k.Name
		}
	}
	return "unknown"
}

// whoami runs `board whoami`: it prints the handle, the way back and the key
// name, and asks the server with the key, so a line that prints says the
// server is reachable and accepts the key.
func whoami(args []string, stdout, stderr io.Writer, env Env) int {
	if p, ok := parseArgs(args, whoamiFlags); !ok || len(p.pos) != 0 {
		return usageLine(stderr, WhoamiUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var keys keyList
	if err := c.do(ctx, "GET", "/api/v1/keys", nil, nil, &keys); err != nil {
		return report(stderr, err)
	}
	line := fmt.Sprintf("you: %s (key %s)", c.id.Actor, keys.keyName(c.key))
	if c.id.WayBack != "" {
		line += "  way back: " + c.id.WayBack
	}
	_, _ = fmt.Fprintln(stdout, line+"  server ok")
	return ExitOK
}
