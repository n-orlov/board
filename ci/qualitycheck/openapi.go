// The OpenAPI gates (SPEC 15.4): the document lints clean, and the generated
// Go and TypeScript code has no drift. Neither has a threshold; each runs the
// SAME script ci/lint.sh runs (the npm `openapi:lint` script and
// ci/openapi-drift.sh), so the two entry points cannot disagree. A gate whose
// input does not exist fails, never passes.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Inputs and commands of the OpenAPI gates.
const (
	openapiGoInput = "internal/server/openapi.gen.go"
	openapiTSInput = "web/src/lib/api/schema.d.ts"
)

// openapiCommand is one gate's command, run from dir (relative to the
// working directory).
type openapiCommand struct {
	dir  string
	name string
	args []string
}

// openapiLintCommand runs web/'s `openapi:lint` script over the document.
func openapiLintCommand() openapiCommand {
	return openapiCommand{dir: "web", name: "npm", args: []string{"run", "--silent", "openapi:lint", "--", "../" + openapiInput}}
}

// openapiDriftCommand runs the generated-code drift script.
func openapiDriftCommand() openapiCommand {
	return openapiCommand{dir: ".", name: "sh", args: []string{"ci/openapi-drift.sh"}}
}

// runOpenAPIGate fails when an input is missing, fails with the tool's own
// output when the command exits non-zero, and passes only on exit 0. A command
// that cannot be started at all is a tooling error (exit 2).
func runOpenAPIGate(name, todo string, inputs []string, c openapiCommand) (ok bool, output string, err error) {
	var missing []string
	for _, in := range inputs {
		if _, serr := os.Stat(filepath.Clean(in)); serr != nil {
			missing = append(missing, in)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("%s gate: input missing: %s\nwhat to do: %s\n", name, strings.Join(missing, ", "), todo), nil
	}
	cmd := exec.Command(c.name, c.args...) //nolint:gosec // G204: the command is the gate's own fixed invocation, never external input
	cmd.Dir = c.dir
	out, runErr := cmd.CombinedOutput()
	text := string(out)
	if runErr == nil {
		return true, text + "what to do: nothing.\n", nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return false, text + "what to do: " + todo + "\n", nil
	}
	return false, text, fmt.Errorf("running %s %s: %w", c.name, strings.Join(c.args, " "), runErr)
}

func runOpenAPILintGate() (bool, string, error) {
	return runOpenAPIGate("openapi-lint", "fix each finding in api/openapi.yaml; the ruleset is web/redocly.yaml and has no threshold.",
		[]string{openapiInput, "web/node_modules"}, openapiLintCommand())
}

func runOpenAPIDriftGate() (bool, string, error) {
	return runOpenAPIGate("openapi-drift", "run ci/openapi-gen.sh and commit the regenerated Go and TypeScript.",
		[]string{openapiInput, openapiGoInput, openapiTSInput, "web/node_modules"}, openapiDriftCommand())
}
