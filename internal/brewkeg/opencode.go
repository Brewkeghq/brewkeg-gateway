package brewkeg

import (
	"strings"
)

// OpenCode is the open-source terminal agent. Its config is one plain JSON file
// at ~/.config/opencode/opencode.json on every OS, and a custom provider is a
// single entry under `providers` — so, unlike Cursor, there is a file to own.
//
// The shape below was read off a real install (v2.2.5) and then confirmed
// against the binary itself: a provider entry with `package` + `settings.baseURL`
// + `settings.apiKey` produced a POST to <baseURL>/chat/completions carrying
// `Authorization: Bearer <key>`. The older `provider`/`npm`/`options` spelling
// still parses, but the published schema marks it deprecated, so we write the
// new one.
//
// What we touch, and nothing else:
//   providers.brewkeg
//   model            (only when it names us)

const (
	// OpenCodeProviderID is our key under `providers`. Stable: it is what
	// separates our entry from a provider the user added themselves.
	OpenCodeProviderID = "brewkeg"
	// OpenCodePackage is the wire adapter. OpenCode resolves it by name and
	// installs it on first use; the openai-compatible one speaks our
	// /v1/chat/completions, so OpenCode gets the OpenAI family of ids.
	OpenCodePackage = "@opencode/ai/providers/openai-compatible"
	// OpenCodeConfigPath is where the config lives, relative to $HOME. Same
	// path on every OS, including Windows.
	OpenCodeConfigPath = ".config/opencode/opencode.json"
)

// WriteOpenCodeProvider points OpenCode at brewkeg and makes it the default.
//
// models is the list the server publishes with the rest of the spec. An empty
// list falls back to the OpenAI-family defaults, for an offline run.
func WriteOpenCodeProvider(path string, o Options, models []string) error {
	doc, err := loadJSONDoc(path)
	if err != nil {
		return err
	}
	if _, ok := doc["$schema"]; !ok {
		// A file we are creating ourselves: match what the app writes, so an
		// editor offers completion for the rest of the file.
		doc["$schema"] = "https://opencode.ai/config.json"
	}

	lineup := opencodeLineup(o, models)

	entry := map[string]any{
		"name":    "Brewkeg",
		"package": OpenCodePackage,
		"settings": map[string]any{
			// OpenCode appends /chat/completions to this, so the /v1 has to be
			// here for the request to land on our /v1/chat/completions.
			"baseURL": strings.TrimRight(o.BaseURL, "/") + "/v1",
			"apiKey":  o.APIKey,
		},
		"models": opencodeModels(lineup),
	}

	providers := ensureMap(doc, "providers")
	if existing, ok := providers[OpenCodeProviderID].(map[string]any); ok {
		// Edit in place: a setting or a model we do not know about survives.
		mergeInPlace(existing, entry)
	} else {
		providers[OpenCodeProviderID] = entry
	}

	// The default model. OpenCode stores it as "provider/model", and without one
	// the provider is listed but nothing routes through it — which reads as a
	// setup that did nothing.
	//
	// Set only when it is unset or already ours. A user who chose their own
	// default keeps it: the provider is still there to pick, and a removal that
	// deleted a model the user chose would be a silent loss.
	if cur, _ := doc["model"].(string); cur == "" || strings.HasPrefix(cur, OpenCodeProviderID+"/") {
		doc["model"] = OpenCodeProviderID + "/" + lineup[0]
	}

	return writeJSONDoc(path, doc)
}

// RemoveOpenCodeProvider takes brewkeg back out, leaving every other provider,
// every other setting and the user's own default model alone.
func RemoveOpenCodeProvider(path string) (bool, error) {
	doc, err := loadJSONDoc(path)
	if err != nil {
		return false, err
	}

	changed := false
	if providers, ok := doc["providers"].(map[string]any); ok {
		if _, ours := providers[OpenCodeProviderID]; ours {
			delete(providers, OpenCodeProviderID)
			changed = true
			if len(providers) == 0 {
				delete(doc, "providers")
			}
		}
	}

	// The default model, in either spelling the file accepts: the string
	// "brewkeg/<id>" or the object {providerID, model}.
	switch m := doc["model"].(type) {
	case string:
		if strings.HasPrefix(m, OpenCodeProviderID+"/") {
			delete(doc, "model")
			changed = true
		}
	case map[string]any:
		if id, _ := m["providerID"].(string); id == OpenCodeProviderID {
			delete(doc, "model")
			changed = true
		}
	}

	if !changed {
		return false, nil
	}
	return true, writeJSONDoc(path, doc)
}

// OpenCodePointsAtBrewkeg reports whether our provider is in the file. It looks
// for the provider entry, not for a marker of ours: someone who wired brewkeg in
// by hand must see the same switch state as someone who used the app.
func OpenCodePointsAtBrewkeg(path string) bool {
	doc, err := loadJSONDoc(path)
	if err != nil {
		return false
	}
	providers, _ := doc["providers"].(map[string]any)
	_, ours := providers[OpenCodeProviderID]
	return ours
}

// opencodeLineup is the model ids the provider offers. The server owns the
// list; the two defaults below are the offline fallback and are OpenAI-family,
// because that is the protocol this adapter speaks.
func opencodeLineup(o Options, models []string) []string {
	if len(models) > 0 {
		return models
	}
	var out []string
	for _, m := range []string{o.CodexModel, o.MainModel} {
		if m != "" {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		out = []string{"gpt-6-astra"}
	}
	return out
}

func opencodeModels(ids []string) map[string]any {
	out := make(map[string]any, len(ids))
	for _, id := range ids {
		// The label is the id itself: inventing a prettier name here would be
		// one more place for a model to be described wrongly.
		out[id] = map[string]any{"name": id}
	}
	return out
}
