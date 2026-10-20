package brewkeg

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

// Agent is a coding agent brewkeg can launch.
type Agent struct {
	ID    string
	Label string
	Bin   string
	// Pkg is the npm package the agent is published under, empty when it is
	// not on npm. The doctor asks the registry for the newest version.
	Pkg string
	// Install is how a missing agent is installed.
	Install string
	// Update is how an outdated agent is updated. Empty when there is no
	// single command that does it.
	Update string
	// Family is the model family this agent is tuned for.
	Family string
	// Aliases are other names the agent answers to.
	Aliases []string
}

// Agents is every agent brewkeg can launch, in menu order.
func Agents() []Agent {
	return []Agent{
		{
			ID: "claude", Label: "Claude Code", Bin: "claude",
			Pkg:     "@anthropic-ai/claude-code",
			Install: "npm install -g @anthropic-ai/claude-code",
			Update:  "npm install -g @anthropic-ai/claude-code@latest",
			Family:  "opus",
		},
		{
			ID: "codex", Label: "Codex", Bin: "codex",
			Pkg:     "@openai/codex",
			Install: "npm install -g @openai/codex",
			Update:  "npm install -g @openai/codex@latest",
			Family:  "gpt",
		},
		{
			ID: "opencode", Label: "OpenCode", Bin: "opencode",
			Pkg:     "opencode-ai",
			Install: "npm install -g opencode-ai",
			Update:  "npm install -g opencode-ai@latest",
			Family:  "gpt",
		},
		{
			ID: "pi", Label: "Pi", Bin: "pi",
			Pkg:     "@earendil-works/pi-coding-agent",
			Install: "npm install -g @earendil-works/pi-coding-agent",
			Update:  "npm install -g @earendil-works/pi-coding-agent@latest",
			Family:  "gpt",
		},
		{
			ID: "dsh", Label: "DeepSeek Harness", Bin: "dsh",
			Pkg:     "@deepseek-ai/dsh",
			Install: "npm i -g @deepseek-ai/dsh",
			Family:  "gpt",
		},
		{
			ID: "kilo", Label: "Kilo Code", Bin: "kilo",
			Pkg:     "@kilocode/cli",
			Install: "npm install -g @kilocode/cli",
			Update:  "npm install -g @kilocode/cli@latest",
			Family:  "gpt",
		},
		{
			ID: "command-code", Label: "Command Code", Bin: "command-code",
			Pkg:     "command-code",
			Install: "npm install -g command-code",
			Update:  "npm install -g command-code@latest",
			Family:  "gpt",
			Aliases: []string{"cmd"},
		},
	}
}

// AgentByID finds an agent by id or alias.
func AgentByID(id string) (Agent, bool) {
	for _, a := range Agents() {
		if a.ID == id {
			return a, true
		}
		for _, al := range a.Aliases {
			if al == id {
				return a, true
			}
		}
	}
	return Agent{}, false
}

// AgentStatus is what the doctor reports about one agent.
type AgentStatus struct {
	Agent
	Installed bool
	Path      string
	Version   string
}

// LookBin finds an executable, including the usual install dirs a
// Finder-launched app does not have on PATH.
func LookBin(bin string) string {
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	for _, dir := range []string{
		"/opt/homebrew/bin", "/usr/local/bin",
		HomeJoin(".local", "bin"), HomeJoin("bin"),
		HomeJoin(".bun", "bin"), "/usr/bin",
	} {
		cand := filepath.Join(dir, bin)
		if FileExists(cand) {
			return cand
		}
	}
	return ""
}

var versionRe = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

// AgentVersion asks an agent for its version. Empty when it will not say.
func AgentVersion(ctx context.Context, bin string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return versionRe.FindString(string(out))
}
