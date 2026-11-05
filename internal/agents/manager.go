// Package agents runs a scenario's autonomous actors: each is a service
// account of its own, acting in a cub context of its own, that repeatedly
// makes a change inside a ChangeSet and rolls it out as a change order. The
// point is what the audience sees when several of them work at once: a change
// to a component that requires review stops for a person, other changes to
// the same component wait behind it, and changes to anything else go through.
package agents

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/scenario"
	"github.com/confighub/cub-demo/internal/seed"
)

// Manager creates and removes the identities the agents act as: a service
// account per agent, a key for it on this machine, and a cub context signed
// in with that key.
type Manager struct {
	Model  *scenario.Model
	Out    io.Writer
	Server string
	DryRun bool
}

// AccountName is the agent's service account: <demo>-agent-<name>.
func (m *Manager) AccountName(a scenario.Agent) string {
	return m.Model.Scenario.Name + "-agent-" + a.Name
}

// ContextName is the cub context the agent acts in, named as its account.
func (m *Manager) ContextName(a scenario.Agent) string {
	return m.AccountName(a)
}

// Up ensures every agent's account, key and context, and signs the context
// in. Re-runnable: what exists is kept, and a context whose key no longer
// signs in gets a fresh key. The accounts get the manager role: an agent
// creates ChangeSets, change orders and Releases in spaces it did not
// create, which the editor role does not allow.
func (m *Manager) Up() error {
	for _, a := range m.Model.Scenario.Agents {
		name := m.AccountName(a)
		if _, err := cubexec.Run("serviceaccount", "get", name); err != nil {
			if err := m.run("", "serviceaccount", "create", name, "--org-role", "manager",
				"--display-name", "agent "+a.Name+" of demo "+m.Model.Scenario.Name,
				"--label", seed.LabelDemoName+"="+m.Model.Scenario.Name); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(m.Out, "  service account %s exists\n", name)
		}
		ctx := m.ContextName(a)
		if _, err := cubexec.Run("context", "get", ctx); err != nil {
			if err := m.run("", "context", "create", ctx, "--server", m.Server); err != nil {
				return err
			}
		}
		if m.DryRun {
			continue
		}
		if _, err := cubexec.RunIn(ctx, "auth", "login", "--private-key="+name); err != nil {
			// No key on this machine signs in as the account: register one.
			alias := fmt.Sprintf("%s-%d", name, time.Now().Unix())
			if err := m.run("", "serviceaccount", "key", "add", name, "--generate", alias,
				"--description", "cub demo agents on "+hostname()); err != nil {
				return err
			}
			if err := m.run(ctx, "auth", "login", "--private-key="+alias); err != nil {
				return err
			}
		}
		fmt.Fprintf(m.Out, "  %s acts as %s in cub context %s\n", a.Name, name, ctx)
	}
	return nil
}

// Down deletes every agent's context and service account. The local keys
// stay, since they sign in to nothing once the account is gone.
func (m *Manager) Down() error {
	for _, a := range m.Model.Scenario.Agents {
		name := m.AccountName(a)
		ctx := m.ContextName(a)
		if _, err := cubexec.Run("context", "get", ctx); err == nil {
			if err := m.run("", "context", "delete", ctx); err != nil {
				return err
			}
		}
		if _, err := cubexec.Run("serviceaccount", "get", name); err == nil {
			if err := m.run("", "serviceaccount", "delete", name); err != nil {
				return err
			}
		}
	}
	return nil
}

// run prints and runs one cub command in a context, honoring dry-run.
func (m *Manager) run(context string, args ...string) error {
	if context != "" {
		fmt.Fprintf(m.Out, "▶ cub --context %s %s\n", context, strings.Join(args, " "))
	} else {
		fmt.Fprintf(m.Out, "▶ cub %s\n", strings.Join(args, " "))
	}
	if m.DryRun {
		return nil
	}
	_, err := cubexec.RunIn(context, args...)
	return err
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "this machine"
	}
	return h
}
