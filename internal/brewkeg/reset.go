package brewkeg

import (
	"fmt"
	"os"
	"strings"
)

// ResetPlan is what a reset would do, so a confirmation can describe it before
// anything is written. It is the same list the run actually acts on — a plan
// that can drift from the action is worse than no plan.
type ResetPlan struct {
	Targets []string `json:"targets"` // services brewkeg is currently inside
	Key     bool     `json:"key"`     // a key is stored and will be forgotten
	Config  bool     `json:"config"`  // a config file exists and will be removed
	Caches  []string `json:"caches"`  // gateway-keyed caches that will be cleared
}

// PlanReset reports what Reset would change, without changing it.
//
// Empty Targets means there is nothing to disconnect — the machine is already
// clean — and is not an error. Reset on a clean machine should be a no-op the
// user can perform without fear, not a refusal.
func PlanReset(s Spec) ResetPlan {
	p := ResetPlan{}
	for _, t := range s.Targets {
		if enabledFor(specTarget{spec: t}, s) {
			p.Targets = append(p.Targets, t.ID)
		}
	}
	p.Key = FileExists(KeyStorePath())
	p.Config = FileExists(UserConfigPath())
	p.Caches = StaleGatewayCachesPending()
	return p
}

// Reset returns the machine to its pre-brewkeg state: every service brewkeg
// currently points at is disconnected inside ONE backup (so a mistake is still
// undoable), then the stored key and the config file are forgotten.
//
// Order matters. The eviction runs first because it needs the backup to be
// saved, and because the key is what lets a later run re-apply — a reset that
// forgot the key first would leave services configured with no way to undo it
// from the app. The key is cleared last, after there is nothing left pointing
// at us.
//
// A dry run reports the plan and writes nothing, including the backup.
func Reset(s Spec, dryRun bool) (*ResetPlan, *Backup, []ApplyResult, error) {
	plan := PlanReset(s)
	if dryRun {
		return &plan, nil, nil, nil
	}

	// An empty target list is the whole point: evictTargets treats "wanted =
	// nothing" as "take brewkeg out of everything currently configured".
	var b *Backup
	var results []ApplyResult
	if len(plan.Targets) > 0 {
		var err error
		b, results, err = ApplyWithSpec(s, []string{}, Options{})
		if err != nil {
			return &plan, b, results, err
		}
		if !anyOK(results) {
			return &plan, b, results, fmt.Errorf("could not disconnect anything — nothing else was changed")
		}
	}

	if err := ClearKey(); err != nil && !os.IsNotExist(err) {
		return &plan, b, results, fmt.Errorf("configs were reset but the stored key could not be cleared: %w", err)
	}
	if err := os.Remove(UserConfigPath()); err != nil && !os.IsNotExist(err) {
		return &plan, b, results, fmt.Errorf("configs were reset but the config file could not be removed: %w", err)
	}
	for _, c := range plan.Caches {
		if err := os.Remove(c); err != nil && !os.IsNotExist(err) {
			return &plan, b, results, fmt.Errorf("configs were reset but %s could not be cleared: %w", c, err)
		}
	}
	return &plan, b, results, nil
}

// ResetSummary is one honest sentence for the window to show afterwards.
func ResetSummary(plan *ResetPlan, results []ApplyResult) string {
	bits := []string{}
	if n := len(results); n > 0 {
		bits = append(bits, fmt.Sprintf("Disconnected %d service(s)", n))
	} else {
		bits = append(bits, "Nothing was configured")
	}
	if plan.Key {
		bits = append(bits, "forgot the stored key")
	}
	if plan.Config {
		bits = append(bits, "removed config.json")
	}
	if n := len(plan.Caches); n > 0 {
		bits = append(bits, fmt.Sprintf("cleared %d caches", n))
	}
	return strings.Join(bits, ", ") + "."
}
