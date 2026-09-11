package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wii/senv/internal/agentcfg"
	"github.com/wii/senv/internal/storage"
)

// Export actions, as reported in plans and results.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionSkip   = "skip"
	ActionDrift  = "drift"
	ActionError  = "error"
)

// ExportItem is one (agent, alias) decision or outcome.
type ExportItem struct {
	target    agentcfg.Target
	Agent     string
	AgentName string
	Path      string
	Alias     string
	Action    string
	Reason    string
	// Plaintext marks items whose resolved env values will be written to the
	// target file in clear text.
	Plaintext bool
	// server is the resolved definition this item would write, kept so callers
	// can render snippets with --print.
	server agentcfg.Server
}

// ExportPlan is the full set of planned writes, in target order.
type ExportPlan struct {
	Items []ExportItem
}

// NeedsWrite reports whether the plan contains anything that will modify files.
func (p *ExportPlan) NeedsWrite() bool {
	for _, item := range p.Items {
		if item.Action == ActionCreate || item.Action == ActionUpdate {
			return true
		}
	}
	return false
}

// ExporterOptions configures an Exporter. Resolve dereferences {{env:...}} /
// {{text:...}} templates in env values; nil means "no references to resolve".
type ExporterOptions struct {
	Home       string
	Scope      string
	Force      bool
	Resolve    func(string) (string, error)
	LedgerPath string
}

// Exporter writes stored profiles into agent configs and maintains the local
// ledger that tells senv-written entries from foreign ones.
type Exporter struct {
	mgr    *Manager
	opts   ExporterOptions
	ledger *Ledger
}

// NewExporter validates options and loads the ledger.
func (m *Manager) NewExporter(opts ExporterOptions) (*Exporter, error) {
	scope, err := agentcfg.ResolveScope(opts.Scope)
	if err != nil {
		return nil, err
	}
	opts.Scope = scope
	if opts.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("could not determine home directory: %w", err)
		}
		opts.Home = home
	}
	if opts.Resolve == nil {
		opts.Resolve = func(value string) (string, error) { return value, nil }
	}
	return &Exporter{mgr: m, opts: opts, ledger: LoadLedger(opts.LedgerPath)}, nil
}

// Ledger exposes the loaded ledger (warnings included) to the caller.
func (e *Exporter) Ledger() *Ledger {
	return e.ledger
}

// Server exposes the resolved definition of a planned item, for --print.
func (i ExportItem) Server() agentcfg.Server {
	return i.server
}

// Plan decides, per target and alias, what an export would do. It never writes.
func (e *Exporter) Plan(targets []agentcfg.Target, aliases []string) (*ExportPlan, error) {
	names := aliases
	if len(names) == 0 {
		servers, err := e.mgr.List()
		if err != nil {
			return nil, err
		}
		for _, s := range servers {
			names = append(names, s.Alias)
		}
	}

	desired := make(map[string]agentcfg.Server, len(names))
	resolveErr := make(map[string]error, len(names))
	for _, alias := range names {
		entry, err := e.mgr.Get(alias)
		if err != nil {
			return nil, err
		}
		server, err := e.resolveEntry(entry)
		if err != nil {
			resolveErr[alias] = err
			continue
		}
		desired[alias] = server
	}

	plan := &ExportPlan{}
	for _, target := range targets {
		path := target.ResolveConfigPath(e.opts.Home, e.opts.Scope)
		file, loadErr := loadAgentFile(target, path)
		for _, alias := range names {
			item := ExportItem{
				target:    target,
				Agent:     target.ID,
				AgentName: target.Name,
				Path:      path,
				Alias:     alias,
			}
			switch {
			case loadErr != nil:
				item.Action = ActionError
				item.Reason = loadErr.Error()
			case resolveErr[alias] != nil:
				// A profile that cannot be resolved poisons its whole target:
				// writing the remaining entries would silently diverge from
				// the vault.
				item.Action = ActionError
				item.Reason = resolveErr[alias].Error()
			default:
				item.server = desired[alias]
				// A target that cannot express this transport poisons its whole
				// target too (plan-level error, file untouched): writing the
				// remaining entries would pretend the vault exported cleanly.
				if err := target.RemoteError(desired[alias]); err != nil {
					item.Action = ActionError
					item.Reason = err.Error()
				} else {
					e.planItem(&item, file, alias, desired[alias])
				}
			}
			plan.Items = append(plan.Items, item)
		}
	}
	return plan, nil
}

// planItem fills in the action for one (target, alias) pair.
func (e *Exporter) planItem(item *ExportItem, file *agentFile, alias string, wanted agentcfg.Server) {
	current, present := file.entry(alias)
	wantedFingerprint := wanted.Fingerprint()
	// Remote entries always carry their endpoint in the clear; env values and
	// headers may hold resolved secrets. All three are plaintext on write.
	item.Plaintext = len(wanted.Env) > 0 || wanted.URL != "" || len(wanted.Headers) > 0

	switch {
	case present && current.Fingerprint() == wantedFingerprint:
		item.Action = ActionSkip
		item.Reason = "already up to date"
	case !present:
		item.Action = ActionCreate
		item.Reason = "entry not present"
	default:
		record, recorded := e.ledger.Get(item.Agent, alias)
		switch {
		case recorded && record.Fingerprint == current.Fingerprint():
			item.Action = ActionUpdate
			item.Reason = "senv-managed entry is out of date"
		case e.opts.Force:
			item.Action = ActionUpdate
			item.Reason = "overwriting an entry senv did not write (--force)"
		default:
			item.Action = ActionDrift
			item.Reason = "entry was modified locally or written by another tool; use --force to overwrite"
		}
	}
}

// ExportReport is the outcome of an execute pass.
type ExportReport struct {
	Items    []ExportItem
	Failures int
}

// Execute applies the writable items of a plan. Targets are independent: one
// failure never aborts the others, and the ledger records only entries that
// were actually written (or already matched).
func (e *Exporter) Execute(plan *ExportPlan) (ExportReport, error) {
	report := ExportReport{}
	byTarget := map[string][]int{}
	order := []string{}
	for i, item := range plan.Items {
		if _, seen := byTarget[item.Agent]; !seen {
			order = append(order, item.Agent)
		}
		byTarget[item.Agent] = append(byTarget[item.Agent], i)
	}

	for _, agentID := range order {
		indexes := byTarget[agentID]
		items := make([]ExportItem, 0, len(indexes))
		for _, i := range indexes {
			items = append(items, plan.Items[i])
		}
		items = e.executeTarget(agentID, items)
		report.Items = append(report.Items, items...)
	}

	if err := e.ledger.Save(); err != nil {
		return report, err
	}
	for _, item := range report.Items {
		if item.Action == ActionError {
			report.Failures++
		}
	}
	return report, nil
}

// executeTarget writes one agent config, re-checking drift right before the
// write so a file changed since planning is not silently clobbered.
func (e *Exporter) executeTarget(agentID string, items []ExportItem) []ExportItem {
	writable := func(item ExportItem) bool {
		return item.Action == ActionCreate || item.Action == ActionUpdate
	}
	for _, item := range items {
		if item.Action == ActionError {
			// A poisoned target is reported as a whole; nothing is written.
			return items
		}
	}

	file, err := loadAgentFile(items[0].target, items[0].Path)
	if err != nil {
		return failItems(items, err.Error())
	}

	for i := range items {
		item := &items[i]
		if !writable(*item) {
			continue
		}
		// Re-check the ledger right before writing: if the entry changed since
		// planning, the plan's assumption no longer holds.
		if record, recorded := e.ledger.Get(agentID, item.Alias); recorded {
			if current, present := file.entry(item.Alias); present && current.Fingerprint() != record.Fingerprint && !e.opts.Force {
				item.Action = ActionDrift
				item.Reason = "entry changed after planning; re-run or pass --force"
				continue
			}
		} else if _, present := file.entry(item.Alias); present && !e.opts.Force {
			item.Action = ActionDrift
			item.Reason = "entry changed after planning; re-run or pass --force"
			continue
		}
		entry, err := e.mgr.Get(item.Alias)
		if err != nil {
			item.Action = ActionError
			item.Reason = err.Error()
			continue
		}
		server, err := e.resolveEntry(entry)
		if err != nil {
			item.Action = ActionError
			item.Reason = err.Error()
			continue
		}
		file.set(item.Alias, server)
		e.ledger.Set(agentID, item.Alias, server.Fingerprint())
	}

	// Recompute after the re-checks above: every planned write may have
	// degraded to drift/error, in which case the file must keep its bytes.
	hasWritable := false
	for _, item := range items {
		if writable(item) {
			hasWritable = true
			break
		}
	}
	if !hasWritable {
		return items
	}
	data, err := file.encode()
	if err != nil {
		return failItems(items, err.Error())
	}
	if err := agentcfg.WriteWithBackup(items[0].Path, data); err != nil {
		return failItems(items, err.Error())
	}
	// Entries that were already up to date are still senv's: record them so a
	// later local edit is recognized as drift.
	for i := range items {
		if items[i].Action == ActionSkip {
			if entry, err := e.mgr.Get(items[i].Alias); err == nil {
				if server, err := e.resolveEntry(entry); err == nil {
					e.ledger.Set(agentID, items[i].Alias, server.Fingerprint())
				}
			}
		}
	}
	return items
}

func failItems(items []ExportItem, reason string) []ExportItem {
	for i := range items {
		if items[i].Action == ActionCreate || items[i].Action == ActionUpdate {
			items[i].Action = ActionError
			items[i].Reason = reason
		}
	}
	return items
}

// resolveEntry converts a stored profile into the cross-agent write shape,
// resolving env templates for stdio entries and url/header templates for
// remote entries in the process.
func (e *Exporter) resolveEntry(entry *storage.MCPServerEntry) (agentcfg.Server, error) {
	server := agentcfg.Server{Transport: entry.Transport}
	if entry.Transport == storage.MCPTransportHTTP || entry.Transport == storage.MCPTransportSSE {
		resolved, err := e.opts.Resolve(entry.URL)
		if err != nil {
			return agentcfg.Server{}, fmt.Errorf("MCP server %q url: %w", entry.Alias, err)
		}
		server.URL = resolved
		if len(entry.Headers) > 0 {
			server.Headers = make(map[string]string, len(entry.Headers))
			for key, raw := range entry.Headers {
				resolved, err := e.opts.Resolve(raw)
				if err != nil {
					return agentcfg.Server{}, fmt.Errorf("MCP server %q header %s: %w", entry.Alias, key, err)
				}
				server.Headers[key] = resolved
			}
		}
		return server, nil
	}
	server.Command = entry.Command
	server.Args = entry.Args
	if len(entry.Env) > 0 {
		server.Env = make(map[string]string, len(entry.Env))
		for key, raw := range entry.Env {
			resolved, err := e.opts.Resolve(raw)
			if err != nil {
				return agentcfg.Server{}, fmt.Errorf("MCP server %q env %s: %w", entry.Alias, key, err)
			}
			server.Env[key] = resolved
		}
	}
	return server, nil
}

// agentFile is one agent config file held in memory: JSON roots stay generic
// maps (unknown keys preserved), TOML stays as text (comments and formatting
// preserved).
type agentFile struct {
	target   agentcfg.Target
	path     string
	jsonRoot map[string]any
	tomlSrc  string
	isJSON   bool
}

func loadAgentFile(target agentcfg.Target, path string) (*agentFile, error) {
	file := &agentFile{target: target, path: path, isJSON: target.Format == agentcfg.FormatJSON}
	if file.isJSON {
		root, err := agentcfg.ReadJSONRoot(path)
		if err != nil {
			return nil, err
		}
		file.jsonRoot = root
		return file, nil
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	file.tomlSrc = string(data)
	return file, nil
}

func (f *agentFile) entry(name string) (agentcfg.Server, bool) {
	if f.isJSON {
		return agentcfg.JSONServer(f.jsonRoot, f.target.JSONServersKey, name)
	}
	return agentcfg.TOMLServer(f.tomlSrc, f.target.TOMLTableName, name)
}

func (f *agentFile) set(name string, server agentcfg.Server) {
	if f.isJSON {
		agentcfg.SetJSONServer(f.jsonRoot, f.target.JSONServersKey, name, server)
		return
	}
	block := agentcfg.RenderTOMLServerBlock(f.target.TOMLTableName, name, server)
	updated, err := agentcfg.UpsertTOMLServer(f.tomlSrc, f.target.TOMLTableName, name, block)
	if err != nil {
		// Upsert only fails on impossible inputs; keep the previous text so the
		// caller's write cannot emit a half-built file.
		return
	}
	f.tomlSrc = updated
}

func (f *agentFile) remove(name string) bool {
	if f.isJSON {
		return agentcfg.DeleteJSONServer(f.jsonRoot, f.target.JSONServersKey, name)
	}
	updated, removed := agentcfg.RemoveTOMLServer(f.tomlSrc, f.target.TOMLTableName, name)
	if removed {
		f.tomlSrc = updated
	}
	return removed
}

func (f *agentFile) encode() ([]byte, error) {
	if f.isJSON {
		return agentcfg.EncodeJSON(f.jsonRoot)
	}
	return []byte(f.tomlSrc), nil
}

// ---------------------------------------------------------------------------
// Unexport
// ---------------------------------------------------------------------------

// Unexport actions.
const (
	UnexportRemove  = "remove"
	UnexportChanged = "changed"
	UnexportAbsent  = "absent"
)

// UnexportItem is one (agent, alias) removal decision or outcome.
type UnexportItem struct {
	target    agentcfg.Target
	Agent     string
	AgentName string
	Path      string
	Alias     string
	Action    string
	Reason    string
}

// UnexportPlan is the set of removals an unexport would perform.
type UnexportPlan struct {
	Items []UnexportItem
}

// NeedsWrite reports whether anything would be removed or rewritten: remove
// items execute directly, changed items execute after per-item confirmation.
// Only absent items (never exported, already gone) mean there is nothing to do.
func (p *UnexportPlan) NeedsWrite() bool {
	for _, item := range p.Items {
		if item.Action == UnexportRemove || item.Action == UnexportChanged {
			return true
		}
	}
	return false
}

// PlanUnexport lists ledger-recorded entries for the given targets. It needs no
// profile data: the ledger fingerprint alone proves senv wrote the entry and
// that it is unchanged.
func (e *Exporter) PlanUnexport(targets []agentcfg.Target, aliases []string) (*UnexportPlan, error) {
	filter := map[string]bool{}
	for _, alias := range aliases {
		filter[alias] = true
	}
	plan := &UnexportPlan{}
	for _, target := range targets {
		path := target.ResolveConfigPath(e.opts.Home, e.opts.Scope)
		file, loadErr := loadAgentFile(target, path)
		for _, alias := range e.ledger.Aliases(target.ID) {
			if len(filter) > 0 && !filter[alias] {
				continue
			}
			item := UnexportItem{
				target:    target,
				Agent:     target.ID,
				AgentName: target.Name,
				Path:      path,
				Alias:     alias,
			}
			if loadErr != nil {
				item.Action = ActionError
				item.Reason = loadErr.Error()
				plan.Items = append(plan.Items, item)
				continue
			}
			record, _ := e.ledger.Get(target.ID, alias)
			current, present := file.entry(alias)
			switch {
			case !present:
				item.Action = UnexportAbsent
				item.Reason = "entry already absent"
			case current.Fingerprint() == record.Fingerprint:
				item.Action = UnexportRemove
				item.Reason = "matches what senv exported"
			default:
				item.Action = UnexportChanged
				item.Reason = "entry was modified after export; confirm before removing"
			}
			plan.Items = append(plan.Items, item)
		}
	}
	return plan, nil
}

// ExecuteUnexport removes planned entries. confirmChanged is consulted for
// entries modified after export; a nil callback rejects them all.
func (e *Exporter) ExecuteUnexport(plan *UnexportPlan, confirmChanged func(UnexportItem) bool) (ExportReport, error) {
	report := ExportReport{}
	byTarget := map[string][]int{}
	order := []string{}
	for i, item := range plan.Items {
		if _, seen := byTarget[item.Agent]; !seen {
			order = append(order, item.Agent)
		}
		byTarget[item.Agent] = append(byTarget[item.Agent], i)
	}
	for _, agentID := range order {
		indexes := byTarget[agentID]
		items := make([]UnexportItem, 0, len(indexes))
		for _, i := range indexes {
			items = append(items, plan.Items[i])
		}
		items = e.executeUnexportTarget(agentID, items, confirmChanged)
		for _, item := range items {
			report.Items = append(report.Items, ExportItem{
				Agent:     item.Agent,
				AgentName: item.AgentName,
				Path:      item.Path,
				Alias:     item.Alias,
				Action:    item.Action,
				Reason:    item.Reason,
			})
		}
	}
	if err := e.ledger.Save(); err != nil {
		return report, err
	}
	for _, item := range report.Items {
		if item.Action == ActionError || item.Action == UnexportChanged {
			report.Failures++
		}
	}
	return report, nil
}

func (e *Exporter) executeUnexportTarget(agentID string, items []UnexportItem, confirmChanged func(UnexportItem) bool) []UnexportItem {
	needsWrite := false
	for i := range items {
		item := &items[i]
		if item.Action == UnexportChanged {
			if confirmChanged != nil && confirmChanged(*item) {
				item.Action = UnexportRemove
				item.Reason = "confirmed locally modified entry"
				needsWrite = true
				continue
			}
			// Keep the caller-visible action so the report shows what was left
			// alone (and why).
			continue
		}
		if item.Action == UnexportRemove {
			needsWrite = true
		}
	}
	if !needsWrite {
		return items
	}

	file, err := loadAgentFile(items[0].target, items[0].Path)
	if err != nil {
		return failUnexportItems(items, err.Error())
	}
	changed := false
	for i := range items {
		if items[i].Action != UnexportRemove {
			continue
		}
		if file.remove(items[i].Alias) {
			changed = true
		}
		e.ledger.Delete(agentID, items[i].Alias)
	}
	if !changed {
		return items
	}
	data, err := file.encode()
	if err != nil {
		return failUnexportItems(items, err.Error())
	}
	if err := agentcfg.WriteWithBackup(items[0].Path, data); err != nil {
		return failUnexportItems(items, err.Error())
	}
	return items
}

func failUnexportItems(items []UnexportItem, reason string) []UnexportItem {
	for i := range items {
		if items[i].Action == UnexportRemove {
			items[i].Action = ActionError
			items[i].Reason = reason
		}
	}
	return items
}

// LedgerPathForConfigDir returns the ledger path for a senv config directory,
// so CLI callers do not have to re-derive the filename.
func LedgerPathForConfigDir(configDir string) string {
	return filepath.Join(configDir, LedgerFileName)
}

// TargetIDs renders targets as a comma-separated list, for messages.
func TargetIDs(targets []agentcfg.Target) string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return strings.Join(ids, ", ")
}
