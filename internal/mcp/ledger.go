package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LedgerFileName is the machine-local export ledger, stored next to the LLM
// agent pointer (both are local state that deliberately never enters the
// vault: agent config files are per-machine, so a synced ledger would describe
// a state that other machines never had — see ADR-0007/ADR-0003).
const LedgerFileName = "mcp-exports.json"

const ledgerVersion = 1

// LedgerRecord is what senv remembers about one exported entry.
type LedgerRecord struct {
	Fingerprint string    `json:"fingerprint"`
	ExportedAt  time.Time `json:"exported_at"`
}

type ledgerFile struct {
	Version int                                `json:"version"`
	Entries map[string]map[string]LedgerRecord `json:"entries"`
}

// Ledger tracks which agent config entries senv wrote, keyed by agent id then
// profile alias. It is the only way to tell "senv wrote this" from "someone
// else did": the target files themselves carry no ownership marker.
type Ledger struct {
	path     string
	entries  map[string]map[string]LedgerRecord
	warnings []string
}

// LoadLedger reads the ledger. A missing file is an empty ledger; a corrupt
// one degrades to empty with a warning, which makes every entry look external
// (that is, requires --force) rather than silently trusting stale state.
func LoadLedger(path string) *Ledger {
	l := &Ledger{path: path, entries: map[string]map[string]LedgerRecord{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			l.warnings = append(l.warnings, fmt.Sprintf("read %s: %v", path, err))
		}
		return l
	}
	if len(data) == 0 {
		return l
	}
	var parsed ledgerFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		l.warnings = append(l.warnings, fmt.Sprintf("parse %s: %v; treating every entry as externally written", path, err))
		return l
	}
	if parsed.Version != ledgerVersion {
		l.warnings = append(l.warnings, fmt.Sprintf("unsupported ledger version %d; treating every entry as externally written", parsed.Version))
		return l
	}
	if parsed.Entries != nil {
		l.entries = parsed.Entries
	}
	return l
}

// Warnings returns non-fatal ledger problems, in load order.
func (l *Ledger) Warnings() []string {
	return l.warnings
}

// Get returns the record for one exported entry.
func (l *Ledger) Get(agent, alias string) (LedgerRecord, bool) {
	records, ok := l.entries[agent]
	if !ok {
		return LedgerRecord{}, false
	}
	record, ok := records[alias]
	return record, ok
}

// Set records one successful export.
func (l *Ledger) Set(agent, alias, fingerprint string) {
	records, ok := l.entries[agent]
	if !ok {
		records = map[string]LedgerRecord{}
		l.entries[agent] = records
	}
	records[alias] = LedgerRecord{Fingerprint: fingerprint, ExportedAt: time.Now().UTC()}
}

// Delete forgets one exported entry.
func (l *Ledger) Delete(agent, alias string) {
	records, ok := l.entries[agent]
	if !ok {
		return
	}
	delete(records, alias)
	if len(records) == 0 {
		delete(l.entries, agent)
	}
}

// Aliases returns the aliases recorded for an agent, sorted.
func (l *Ledger) Aliases(agent string) []string {
	records := l.entries[agent]
	out := make([]string, 0, len(records))
	for alias := range records {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// Save writes the ledger atomically (temp file + rename) with 0600.
func (l *Ledger) Save() error {
	if l.path == "" {
		return nil
	}
	payload := ledgerFile{Version: ledgerVersion, Entries: l.entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ledger: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ledger dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp-exports-*.tmp")
	if err != nil {
		return fmt.Errorf("create ledger temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod ledger temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write ledger: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync ledger: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close ledger: %w", err)
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		return fmt.Errorf("replace ledger: %w", err)
	}
	return nil
}
