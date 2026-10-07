package brewkeg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Backup is one snapshot of everything the CLI touched during a single run.
// Restore is driven entirely by this: a file that existed before is written
// back byte-for-byte, a file brewkeg created is removed.
type Backup struct {
	ID        string            `json:"id"`
	CreatedAt string            `json:"createdAt"`
	BaseURL   string            `json:"baseUrl"`
	Targets   []string          `json:"targets"`
	Entries   []BackupEntry     `json:"entries"`
	Notes     map[string]string `json:"notes,omitempty"`
}

type BackupEntry struct {
	Target  string `json:"target"`
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	// BackupFile is the path inside the backup dir holding the pre-change copy.
	BackupFile string `json:"backupFile,omitempty"`
}

// NewBackup starts a snapshot for one run of setup.
func NewBackup(baseURL string) *Backup {
	return &Backup{ID: newBackupID(), CreatedAt: NowISO(), BaseURL: baseURL, Notes: map[string]string{}}
}

func newBackupID() string {
	return time.Now().UTC().Format("20060102-150405") + "-" + randomHex(3)
}

func NowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func randomHex(n int) string {
	const hexd = "0123456789abcdef"
	b := make([]byte, n)
	f, err := os.Open("/dev/urandom")
	if err == nil {
		defer f.Close()
		raw := make([]byte, n)
		if _, err := f.Read(raw); err == nil {
			for i, v := range raw {
				b[i] = hexd[v%16]
			}
			return string(b)
		}
	}
	for i := range b {
		b[i] = hexd[(i*7+int(time.Now().UnixNano()))%16]
	}
	return string(b)
}

// capture backs up one file before we modify it. Safe to call repeatedly in a
// run: the first capture wins, so a file touched twice is restored to its
// original state, not to an intermediate one.
func (b *Backup) Capture(target, path string) error {
	if b == nil {
		return fmt.Errorf("no backup in progress")
	}
	for _, e := range b.Entries {
		if e.Path == path {
			return nil
		}
	}

	dir := filepath.Join(BackupsRoot(), b.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	e := BackupEntry{Target: target, Path: path, Existed: FileExists(path)}
	if e.Existed {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Back up the pre-brewkeg state, not the previous run's output. Without
		// this, setup run #2 captures run #1's config, and restoring the latest
		// backup leaves brewkeg configured with the old key.
		raw = sanitizeBackup(path, raw)
		if len(strings.TrimSpace(string(raw))) == 0 {
			// The file held nothing but our own changes — restoring means deleting.
			e.Existed = false
			b.Entries = append(b.Entries, e)
			return nil
		}
		name := sanitize(path)
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			return err
		}
		e.BackupFile = name
	}
	b.Entries = append(b.Entries, e)
	return nil
}

// replacedLineRe matches the line we comment out when we take over a root key,
// e.g. `# brewkeg replaced: model_provider = "openai"`. It is brewkeg's own
// footprint too, so it must not end up in the "pre-brewkeg" backup copy.
var replacedLineRe = regexp.MustCompile(`(?m)^# brewkeg replaced: .*\n?`)

// sanitizeBackup strips brewkeg's own footprint from a copy being backed up.
func sanitizeBackup(path string, raw []byte) []byte {
	s := string(raw)
	if HasBlock(s) {
		if stripped, ok := StripBlock(s); ok {
			s = stripped
		}
	}
	s = replacedLineRe.ReplaceAllString(s, "")
	s = strings.TrimRight(s, "\n") + "\n"
	if strings.HasSuffix(path, ".json") {
		var doc map[string]any
		if err := json.Unmarshal([]byte(s), &doc); err == nil {
			if env, ok := doc["env"].(map[string]any); ok {
				for _, k := range brewkegEnvKeys {
					delete(env, k)
				}
				doc["env"] = env
			}
			if out, err := json.MarshalIndent(doc, "", "  "); err == nil {
				s = string(out) + "\n"
			}
		}
	}
	return []byte(s)
}

func sanitize(p string) string {
	r := strings.NewReplacer(string(filepath.Separator), "_", ":", "_", " ", "_")
	s := r.Replace(p)
	s = strings.TrimPrefix(s, "_")
	if len(s) > 120 {
		s = s[len(s)-120:]
	}
	return s + ".bak"
}

func (b *Backup) Save() error {
	dir := filepath.Join(BackupsRoot(), b.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600)
}

func BackupDir(id string) string { return filepath.Join(BackupsRoot(), id) }

func LoadBackup(id string) (*Backup, error) {
	raw, err := os.ReadFile(filepath.Join(BackupDir(id), "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("backup %s not found", id)
	}
	var b Backup
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("backup %s is corrupt: %w", id, err)
	}
	return &b, nil
}

func ListBackups() ([]Backup, error) {
	ents, err := os.ReadDir(BackupsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Backup
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if b, err := LoadBackup(e.Name()); err == nil {
			out = append(out, *b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func LatestBackup() (*Backup, error) {
	all, err := ListBackups()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no backups found in %s — nothing to restore", BackupsRoot())
	}
	return &all[0], nil
}

// RestoreResult reports what each entry did, for honest output.
type RestoreResult struct {
	Path   string
	Action string // restored | removed | missing
}

func (b *Backup) Restore(dryRun bool) ([]RestoreResult, error) {
	var out []RestoreResult
	for _, e := range b.Entries {
		switch {
		case e.Existed:
			raw, err := os.ReadFile(filepath.Join(BackupDir(b.ID), e.BackupFile))
			if err != nil {
				return out, fmt.Errorf("reading backup of %s: %w", e.Path, err)
			}
			if !dryRun {
				if err := WriteFile(e.Path, string(raw)); err != nil {
					return out, err
				}
			}
			out = append(out, RestoreResult{e.Path, "restored original"})
		case FileExists(e.Path):
			// brewkeg created this file. If it now holds only our marker block,
			// delete it; if the user added their own content, only strip our block.
			cur := ReadFile(e.Path)
			if dryRun {
				out = append(out, RestoreResult{e.Path, "would remove brewkeg block" + removeSuffix(e.Path)})
				continue
			}
			if stripped, ok := StripBlock(cur); ok && strings.TrimSpace(stripped) == "" {
				os.Remove(e.Path)
				out = append(out, RestoreResult{e.Path, "removed (brewkeg created it)"})
			} else {
				_ = WriteFile(e.Path, stripped)
				out = append(out, RestoreResult{e.Path, "removed brewkeg block"})
			}
		default:
			out = append(out, RestoreResult{e.Path, "already gone"})
		}
	}
	return out, nil
}

func removeSuffix(path string) string {
	if strings.HasSuffix(path, ".json") {
		return " / delete empty file"
	}
	return " / delete if empty"
}
