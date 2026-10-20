package brewkeg

import "path/filepath"

// RunDir holds the files a launcher writes for the length of one session:
// the Claude Code settings file, a Pi provider extension. Nothing here
// outlives the agent.
func RunDir() string { return filepath.Join(brewkegHome(), "run") }

// CommandCodeProvidersPath is where Command Code keeps its BYOK providers.
func CommandCodeProvidersPath() string {
	return HomeJoin(".commandcode", "providers.json")
}
