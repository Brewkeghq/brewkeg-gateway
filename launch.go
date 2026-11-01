package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/brewkeghq/brewkeg-gateway/internal/brewkeg"
)

// launch is one agent run: what to execute, what environment to give it, and
// which files exist only for the length of the session.
type launch struct {
	bin   string
	args  []string
	env   []string
	unset []string
	files []launchFile
}

type launchFile struct {
	path string
	body []byte
	// existed and original let a file we merge into be put back as it was.
	existed  bool
	original []byte
}

// session is everything a launch needs from the user.
type session struct {
	APIKey  string
	BaseURL string
	Model   string
	Catalog brewkeg.Catalog
}

// defaultModel is what an agent runs when the user names no model.
func defaultModel(agent brewkeg.Agent) string {
	if agent.Family == "opus" {
		return "claude-opus-5"
	}
	return "gpt-6-astra"
}

// runAgent launches a coding agent against brewkeg.
func runAgent(agentID string, args []string) int {
	agent, ok := brewkeg.AgentByID(agentID)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown agent %q\n", agentID)
		return 2
	}

	printEnv := hasFlag(args, "--print-env")
	model := flagValue(args, "--model")
	rest := stripFlags(args, []string{"--model"}, []string{"--print-env"})

	bin := brewkeg.LookBin(agent.Bin)
	if bin == "" {
		fmt.Fprintf(os.Stderr, "%s is not installed.\n", agent.Label)
		fmt.Fprintf(os.Stderr, "  install: %s\n", agent.Install)
		return 1
	}

	sess, err := resolveSession(agent, model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}

	plan, err := planLaunch(agent, sess, rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	plan.bin = bin

	if printEnv {
		printPlan(plan)
		return 0
	}
	return execPlan(plan)
}

// resolveSession works out the key, the gateway and the model for this run.
// The key comes from a flag, the environment, the stored key or the config
// file, in that order; a missing key is a prompt, never a guess.
func resolveSession(agent brewkeg.Agent, model string) (session, error) {
	s := session{BaseURL: brewkeg.BaseURL()}

	cfg := brewkeg.LoadUserConfig()
	if os.Getenv("BREWKEG_BASE_URL") == "" && cfg.BaseURL != "" {
		s.BaseURL = cfg.BaseURL
	}

	s.APIKey = strings.TrimSpace(os.Getenv("BREWKEG_API_KEY"))
	if s.APIKey == "" {
		s.APIKey = brewkeg.StoredKey()
	}
	if s.APIKey == "" {
		s.APIKey = cfg.APIKey
	}
	if s.APIKey == "" {
		s.APIKey = promptSecret("Paste your brewkeg API key (from brewkeg.dev/dashboard/keys)")
	}
	if s.APIKey == "" {
		return s, fmt.Errorf("no API key — run `brewkeg login` first")
	}

	s.Model = model
	if s.Model == "" {
		s.Model = cfg.Model
	}
	if s.Model == "" {
		s.Model = defaultModel(agent)
	}

	cat, err := brewkeg.FetchCatalog(nil, s.BaseURL)
	if err != nil {
		cat = brewkeg.DefaultCatalog()
	}
	s.Catalog = cat
	return s, nil
}

// planLaunch builds the command line, the environment and the session files
// for one agent.
func planLaunch(agent brewkeg.Agent, s session, rest []string) (launch, error) {
	switch agent.ID {
	case "claude":
		return claudeLaunch(s, rest)
	case "codex":
		return codexLaunch(s, rest)
	case "opencode":
		return opencodeLaunch(s, rest, "opencode")
	case "kilo":
		return opencodeLaunch(s, rest, "kilo")
	case "pi":
		return piLaunch(s, rest)
	case "command-code":
		return commandCodeLaunch(s, rest)
	case "dsh":
		return dshLaunch(s, rest)
	}
	return launch{}, fmt.Errorf("no launcher for %s", agent.ID)
}

// claudeLaunch writes a session settings file and points Claude Code at it.
// The file is temporary: the user's own ~/.claude/settings.json is left alone.
func claudeLaunch(s session, rest []string) (launch, error) {
	body, err := claudeSettingsJSON(s)
	if err != nil {
		return launch{}, err
	}
	path := filepath.Join(brewkeg.RunDir(), fmt.Sprintf("claude-settings-%d.json", os.Getpid()))

	env := []string{
		"ANTHROPIC_BASE_URL=" + s.BaseURL,
		"ANTHROPIC_AUTH_TOKEN=" + s.APIKey,
		"ANTHROPIC_MODEL=" + s.Model,
		"ANTHROPIC_DEFAULT_MODEL=" + s.Model,
	}
	if fast, ok := s.Catalog.ClaudeFamily("haiku"); ok {
		env = append(env, "ANTHROPIC_SMALL_FAST_MODEL="+fast.ID)
	}

	args := append([]string{"--settings", path}, rest...)
	return launch{
		bin:   "claude",
		args:  args,
		env:   env,
		unset: []string{"ANTHROPIC_API_KEY"},
		files: []launchFile{{path: path, body: body}},
	}, nil
}

// claudeSettingsJSON is the settings file Claude Code reads for this session:
// where the gateway is, what key to use, and which model to run.
//
// It carries no `modelPicker`. Rewriting that block replaced the user's own
// rows — every model they already had, gone, on a plain `brewkeg launch` — and
// the ANTHROPIC_DEFAULT_<FAMILY>_* variables are the same edit wearing a
// different hat: they retarget and relabel the rows of the built-in picker.
// Neither belongs in a launcher. The picker is the user's; we set the endpoint,
// the credential and the model for this session, and leave the list alone.
func claudeSettingsJSON(s session) ([]byte, error) {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":   s.BaseURL,
		"ANTHROPIC_AUTH_TOKEN": s.APIKey,
		"ANTHROPIC_MODEL":      s.Model,
	}
	if fast, ok := s.Catalog.ClaudeFamily("haiku"); ok {
		env["ANTHROPIC_SMALL_FAST_MODEL"] = fast.ID
	}

	doc := map[string]any{"env": env}
	return json.MarshalIndent(doc, "", "  ")
}

// codexLaunch points Codex at brewkeg with -c overrides, so the user's own
// ~/.codex/config.toml is not touched.
func codexLaunch(s session, rest []string) (launch, error) {
	base := strings.TrimRight(s.BaseURL, "/") + "/v1"
	args := append([]string{
		"-c", `model_provider="brewkeg"`,
		"-c", `model_providers.brewkeg.name="brewkeg"`,
		"-c", fmt.Sprintf("model_providers.brewkeg.base_url=%q", base),
		"-c", `model_providers.brewkeg.wire_api="responses"`,
		"-c", fmt.Sprintf("model_providers.brewkeg.experimental_bearer_token=%q", s.APIKey),
		"-c", fmt.Sprintf("model=%q", s.Model),
	}, rest...)
	return launch{
		bin:   "codex",
		args:  args,
		env:   []string{"BREWKEG_API_KEY=" + s.APIKey},
		unset: []string{"OPENAI_API_KEY"},
	}, nil
}

// opencodeLaunch configures OpenCode or Kilo Code, which share the
// OPENCODE_CONFIG_CONTENT / KILO_CONFIG_CONTENT shape.
func opencodeLaunch(s session, rest []string, which string) (launch, error) {
	models := openAIModels(s.Catalog)
	if _, ok := models[s.Model]; !ok {
		models[s.Model] = map[string]any{
			"name": s.Model + " · brewkeg",
			"limit": map[string]any{
				"context": 200000,
				"output":  8192,
			},
			"modalities": map[string]any{
				"input":  []string{"text"},
				"output": []string{"text"},
			},
		}
	}
	small := s.Model
	if fast, ok := s.Catalog.ClaudeFamily("haiku"); ok {
		small = fast.ID
	}
	doc := map[string]any{
		"model":       "brewkeg/" + s.Model,
		"small_model": "brewkeg/" + small,
		"provider": map[string]any{
			"brewkeg": map[string]any{
				"name": "brewkeg",
				"npm":  "@ai-sdk/openai-compatible",
				"options": map[string]any{
					"apiKey":  s.APIKey,
					"baseURL": strings.TrimRight(s.BaseURL, "/") + "/v1",
				},
				"models": models,
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return launch{}, err
	}
	env := []string{whichEnvName(which) + "=" + string(raw)}
	if which == "kilo" {
		env = append(env, "KILO_NO_DAEMON=1")
	}
	return launch{bin: which, args: rest, env: env}, nil
}

func whichEnvName(which string) string {
	if which == "kilo" {
		return "KILO_CONFIG_CONTENT"
	}
	return "OPENCODE_CONFIG_CONTENT"
}

// openAIModels is the model map the OpenAI-compatible agents read.
func openAIModels(cat brewkeg.Catalog) map[string]any {
	out := map[string]any{}
	for _, m := range cat.Models {
		out[m.ID] = map[string]any{
			"name": m.ID + " · brewkeg",
			"limit": map[string]any{
				"context": m.ContextWindow,
				"output":  m.MaxOutput,
			},
			"modalities": map[string]any{
				"input":  []string{"text"},
				"output": []string{"text"},
			},
		}
	}
	return out
}

// piLaunch writes a Pi provider extension for the length of the session.
func piLaunch(s session, rest []string) (launch, error) {
	models := make([]any, 0, len(s.Catalog.Models))
	for _, m := range s.Catalog.Models {
		models = append(models, map[string]any{
			"id":            m.ID,
			"name":          m.ID + " · brewkeg",
			"contextWindow": m.ContextWindow,
			"maxTokens":     m.MaxOutput,
			"input":         []string{"text"},
			"reasoning":     strings.Contains(strings.Join(m.Capabilities, ","), "reasoning"),
			"cost": map[string]any{
				"input":      m.InputUSD,
				"output":     m.OutputUSD,
				"cacheRead":  m.CacheReadUSD,
				"cacheWrite": m.CacheWriteUSD,
			},
		})
	}
	provider := map[string]any{
		"api":     "openai-completions",
		"apiKey":  s.APIKey,
		"baseUrl": strings.TrimRight(s.BaseURL, "/") + "/v1",
		"models":  models,
	}
	body, err := json.Marshal(provider)
	if err != nil {
		return launch{}, err
	}
	js := fmt.Sprintf(
		"export default function(pi) { pi.registerProvider(\"brewkeg\", %s); }\n",
		string(body),
	)
	path := filepath.Join(brewkeg.RunDir(), fmt.Sprintf("pi-provider-%d.mjs", os.Getpid()))
	args := append([]string{"--extension", path, "--provider", "brewkeg", "--model", s.Model}, rest...)
	return launch{
		bin:   "pi",
		args:  args,
		env:   []string{"PI_CODING_AGENT_DIR=" + brewkeg.HomeJoin(".pi", "agent")},
		files: []launchFile{{path: path, body: []byte(js)}},
	}, nil
}

// commandCodeLaunch adds a brewkeg provider to Command Code's provider list
// and puts the original file back when the session ends.
func commandCodeLaunch(s session, rest []string) (launch, error) {
	path := brewkeg.CommandCodeProvidersPath()
	doc := map[string]any{}
	if raw := brewkeg.ReadFile(path); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return launch{}, fmt.Errorf("%s is not valid JSON — fix or move it first", path)
		}
	}
	providers, _ := doc["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	models := map[string]any{}
	for _, m := range s.Catalog.Models {
		models[m.ID] = map[string]any{
			"name":          m.ID + " · brewkeg",
			"contextWindow": m.ContextWindow,
			"maxOutput":     m.MaxOutput,
			"cost":          map[string]any{"input": m.InputUSD, "output": m.OutputUSD},
		}
	}
	providers["brewkeg"] = map[string]any{
		"name":    "brewkeg",
		"baseURL": strings.TrimRight(s.BaseURL, "/") + "/v1",
		"apiKey":  "$BREWKEG_API_KEY",
		"models":  models,
	}
	doc["provider"] = providers

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return launch{}, err
	}
	existed := brewkeg.FileExists(path)
	args := append([]string{"--model", "brewkeg/" + s.Model}, rest...)
	return launch{
		bin:   "command-code",
		args:  args,
		env:   []string{"BREWKEG_API_KEY=" + s.APIKey},
		files: []launchFile{{path: path, body: append(body, '\n'), existed: existed, original: []byte(brewkeg.ReadFile(path))}},
	}, nil
}

// dshLaunch runs DeepSeek Harness with the OpenAI-compatible environment.
// dsh reads its provider from a plugin profile, so the env vars are what we
// can set; a profile that ignores them keeps talking to DeepSeek.
func dshLaunch(s session, rest []string) (launch, error) {
	env := []string{
		"OPENAI_BASE_URL=" + strings.TrimRight(s.BaseURL, "/") + "/v1",
		"OPENAI_API_KEY=" + s.APIKey,
		"OPENAI_MODEL=" + s.Model,
	}
	return launch{bin: "dsh", args: rest, env: env}, nil
}

// printPlan shows what would be injected instead of launching.
func printPlan(p launch) {
	for _, name := range p.unset {
		fmt.Printf("unset %s\n", name)
	}
	for _, e := range p.env {
		name, value, _ := strings.Cut(e, "=")
		if value == "" {
			fmt.Printf("unset %s\n", name)
			continue
		}
		fmt.Printf("export %s=%q\n", name, value)
	}
	fmt.Printf("%s %s\n", filepath.Base(p.bin), strings.Join(p.args, " "))
	for _, f := range p.files {
		fmt.Printf("# brewkeg writes at launch, removes on exit: %s\n", f.path)
		fmt.Printf("#   %s\n", string(f.body))
	}
}

// execPlan writes the session files, runs the agent and puts the files back.
func execPlan(p launch) int {
	for _, f := range p.files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
			return 1
		}
		if err := os.WriteFile(f.path, f.body, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
			return 1
		}
	}
	defer restoreFiles(p.files)

	cmd := exec.Command(p.bin, p.args...)
	cmd.Env = append(dropEnv(os.Environ(), p.unset), p.env...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "brewkeg: %v\n", err)
		return 1
	}
	return 0
}

// dropEnv removes named variables from an environment. A variable that is set
// but empty is not the same as one that is absent, and Claude Code treats
// ANTHROPIC_API_KEY as set when it is empty.
func dropEnv(env []string, names []string) []string {
	if len(names) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			if name == n {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// restoreFiles puts back every file the session touched.
func restoreFiles(files []launchFile) {
	for _, f := range files {
		if f.existed {
			_ = os.WriteFile(f.path, f.original, 0o600)
			continue
		}
		_ = os.Remove(f.path)
	}
}

// stripFlags removes flags from an argument list. A flag in valued takes the
// next argument with it; a flag in standalone stands alone.
func stripFlags(args []string, valued, standalone []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		matched := false
		for _, n := range standalone {
			if a == n || strings.HasPrefix(a, n+"=") {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, n := range valued {
			if a == n {
				if i+1 < len(args) {
					i++
				}
				matched = true
				break
			}
			if strings.HasPrefix(a, n+"=") {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, a)
		}
	}
	return out
}
