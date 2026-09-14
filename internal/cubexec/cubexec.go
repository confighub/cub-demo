// Package cubexec shells out to the cub CLI for the operations whose logic
// lives there — change-order creation and stage promotion are ~1000 lines of
// client-side behaviour in cub that would be folly to reimplement. The plugin
// is executed by cub, so cub is on PATH and the session's context applies.
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

// RunWithStdin executes cub with the given arguments, feeding input on stdin.
func RunWithStdin(input string, args ...string) (string, error) {
	cmd := exec.Command("cub", args...)
	cmd.Env = append(os.Environ(), "CONFIGHUB_AGENT=1")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("cub %s: %w\n%s", strings.Join(args, " "), err, text)
	}
	return text, nil
}
