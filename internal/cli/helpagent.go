package cli

import (
	"context"
	_ "embed" // protocolText is the embedded agent protocol
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// HelpAgentUsage is the single stderr line for a malformed `vh help`.
const HelpAgentUsage = "usage: vh help agent [--version]"

// protocolText is the agent protocol cheat sheet (SPEC §8.6). Its first line is
// the header `vh protocol vN`.
//
//go:embed protocol.txt
var protocolText string

var protocolHeader = regexp.MustCompile(`^vh protocol v(\d+)\n`)

// seenTimeout bounds the request that records the protocol as read: printing
// the text never waits long for a server.
const seenTimeout = 5 * time.Second

// ProtocolVersion is the version N in the header of the embedded protocol
// text, or -1 when the header is not there.
func ProtocolVersion() int {
	m := protocolHeader.FindStringSubmatch(protocolText)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// helpCmd runs `vh help ...`: only `help agent` exists.
func helpCmd(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 || args[0] != "agent" {
		return usageLine(stderr, HelpAgentUsage)
	}
	p, ok := parseArgs(slices.Clone(args[1:]), helpAgentFlags)
	if !ok || len(p.pos) != 0 {
		return usageLine(stderr, HelpAgentUsage)
	}
	if p.has("--version") {
		_, _ = fmt.Fprintln(stdout, ProtocolVersion())
		return ExitOK
	}
	_, _ = io.WriteString(stdout, protocolText)
	recordProtocolSeen(env)
	return ExitOK
}

// recordProtocolSeen tells the server the actor has read this protocol
// version. It is best effort and silent: without a configured server, or when
// the server does not answer, the text has already been printed and the
// command still succeeds.
func recordProtocolSeen(env Env) {
	c, err := newClient(env)
	if err != nil {
		return
	}
	ctx, stop := clientContext()
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, seenTimeout)
	defer cancel()
	_ = c.do(ctx, http.MethodPut, "/api/v1/actors/me/protocol-seen", nil, map[string]int{"version": ProtocolVersion()}, nil)
}
