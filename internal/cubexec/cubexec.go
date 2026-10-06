// Package cubexec shells out to the cub CLI for the steps of a play: a play
// shows its audience the cub command a person would type, so it runs that
// command rather than the API call behind it. The plugin is executed by cub,
// so cub is on PATH and the session's context applies.
package cubexec

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes cub with the given arguments, returning its combined output.
// CONFIGHUB_AGENT=1 keeps the output terse and machine-friendly.
func Run(args ...string) (string, error) {
	cmd := exec.Command("cub", args...)
	cmd.Env = append(os.Environ(), "CONFIGHUB_AGENT=1")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("cub %s: %w\n%s", strings.Join(args, " "), err, text)
	}
	return text, nil
}
