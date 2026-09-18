package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/text"
)

func TestRenameProviderOwnedCredentialAndPointers(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 128000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	em := env.NewManager(store, "test-password")
	seedRef := ownedSeedRefTemplate("acme")
	if err := em.Set("default", "SENV_ACME_API_KEY", seedRef); err != nil {
		t.Fatalf("seed env ref: %v", err)
	}
	pointerPath := filepath.Join(t.TempDir(), "agent-pointers.json")
	pf := &PointerFile{Version: 1, Agents: map[string]AgentPointer{}}
	pf.Set("codex", "acme", []string{"m1"}, "m1")
	pf.Set("pi", "acme", []string{"m1"}, "m1")
	pf.Set("other", "keep", []string{"x"}, "x")
	if err := SavePointers(pointerPath, pf); err != nil {
		t.Fatalf("SavePointers: %v", err)
	}

	res, err := mgr.RenameProvider("acme", "acme-prod", pointerPath)
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.Entry.Alias != "acme-prod" || res.Entry.CredentialRef != OwnedCredentialRef("acme-prod") {
		t.Fatalf("entry = %+v", res.Entry)
	}
	if res.PointersUpdated != 2 {
		t.Fatalf("PointersUpdated = %d, want 2", res.PointersUpdated)
	}
	if res.EnvRefsUpdated != 1 {
		t.Fatalf("EnvRefsUpdated = %d, want 1", res.EnvRefsUpdated)
	}
	gotRef, err := em.Get("default", "SENV_ACME_API_KEY")
	if err != nil || gotRef != ownedSeedRefTemplate("acme-prod") {
		t.Fatalf("env ref = %q, %v", gotRef, err)
	}

	if _, err := mgr.GetProvider("acme"); err == nil {
		t.Fatal("old alias still present")
	}
	got, err := mgr.GetProvider("acme-prod")
	if err != nil {
		t.Fatalf("GetProvider new: %v", err)
	}
	if got.BaseURL != "https://api.example.com/v1" || strings.Join(got.Models, ",") != "m1" {
		t.Fatalf("profile mutated unexpectedly: %+v", got)
	}

	tm := text.NewManager(store, "test-password")
	if _, err := tm.Get(LLMKeysGroup, "acme"); err == nil {
		t.Fatal("old credential still present")
	}
	secret, err := tm.Get(LLMKeysGroup, "acme-prod")
	if err != nil || secret != "sk-secret" {
		t.Fatalf("credential = %q, %v", secret, err)
	}

	loaded, err := LoadPointers(pointerPath)
	if err != nil {
		t.Fatalf("LoadPointers: %v", err)
	}
	if loaded.Agents["codex"].Provider != "acme-prod" || loaded.Agents["pi"].Provider != "acme-prod" {
		t.Fatalf("pointers = %+v", loaded.Agents)
	}
	if loaded.Agents["other"].Provider != "keep" {
		t.Fatalf("unrelated pointer changed: %+v", loaded.Agents["other"])
	}
	if strings.Join(loaded.Agents["codex"].Models, ",") != "m1" || loaded.Agents["codex"].DefaultModel != "m1" {
		t.Fatalf("models mutated: %+v", loaded.Agents["codex"])
	}
}

func TestRenameProviderConflicts(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	seed := func(alias string) {
		t.Helper()
		if _, err := mgr.AddProvider(AddProviderOptions{
			Alias: alias, BaseURL: "https://api.example.com", APIKey: "sk-" + alias,
			Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
		}); err != nil {
			t.Fatalf("seed %s: %v", alias, err)
		}
	}
	seed("acme")
	seed("taken")

	if _, err := mgr.RenameProvider("acme", "taken", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("alias conflict error = %v", err)
	}
	if _, err := mgr.GetProvider("acme"); err != nil {
		t.Fatalf("source changed after conflict: %v", err)
	}

	tm := text.NewManager(store, "test-password")
	if err := tm.Set(LLMKeysGroup, "orphan-key", "other-secret"); err != nil {
		t.Fatalf("seed orphan credential: %v", err)
	}
	if _, err := mgr.RenameProvider("acme", "orphan-key", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("credential conflict error = %v", err)
	}
	secret, err := tm.Get(LLMKeysGroup, "acme")
	if err != nil || secret != "sk-acme" {
		t.Fatalf("source credential after conflict = %q, %v", secret, err)
	}
}

func TestRenameProviderExternalKeyRefUnchanged(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	em := env.NewManager(store, "test-password")
	if err := em.AddGroup("llm", "test"); err != nil {
		t.Fatal(err)
	}
	if err := em.Set("llm", "KEY", "sk-ext"); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", KeyRef: "env:llm/KEY",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := mgr.RenameProvider("acme", "acme-prod", filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.Entry.CredentialRef != "env:llm/KEY" {
		t.Fatalf("credential_ref = %q", res.Entry.CredentialRef)
	}
	if res.PointersUpdated != 0 {
		t.Fatalf("PointersUpdated = %d, want 0", res.PointersUpdated)
	}
	if res.EnvRefsUpdated != 0 {
		t.Fatalf("EnvRefsUpdated = %d, want 0", res.EnvRefsUpdated)
	}
	got, err := em.Get("llm", "KEY")
	if err != nil || got != "sk-ext" {
		t.Fatalf("external env moved/deleted: %q, %v", got, err)
	}
	tm := text.NewManager(store, "test-password")
	infos, err := tm.List(LLMKeysGroup)
	if err != nil {
		t.Fatalf("list llm-keys: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("unexpected llm-keys entries: %v", infos)
	}
}

func TestRenameProviderMissingOwnedCredential(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tm := text.NewManager(store, "test-password")
	if err := tm.Delete(LLMKeysGroup, "acme"); err != nil {
		t.Fatalf("delete credential: %v", err)
	}

	res, err := mgr.RenameProvider("acme", "acme-prod", "")
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.Entry.CredentialRef != OwnedCredentialRef("acme-prod") {
		t.Fatalf("credential_ref = %q", res.Entry.CredentialRef)
	}
	if _, err := mgr.GetProvider("acme-prod"); err != nil {
		t.Fatalf("new alias missing: %v", err)
	}
}

func TestRenameProviderEnvSeedRefExactMatchOnly(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	em := env.NewManager(store, "test-password")
	partial := "prefix " + ownedSeedRefTemplate("acme") + " suffix"
	if err := em.Set("default", "NOTE", partial); err != nil {
		t.Fatalf("seed partial env: %v", err)
	}

	res, err := mgr.RenameProvider("acme", "acme-prod", "")
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.EnvRefsUpdated != 0 {
		t.Fatalf("EnvRefsUpdated = %d, want 0", res.EnvRefsUpdated)
	}
	got, err := em.Get("default", "NOTE")
	if err != nil || got != partial {
		t.Fatalf("partial env changed: %q, %v", got, err)
	}
}

func TestRenameProviderMissingCredentialStillCascadesEnvRef(t *testing.T) {
	mgr, store, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk-secret",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tm := text.NewManager(store, "test-password")
	if err := tm.Delete(LLMKeysGroup, "acme"); err != nil {
		t.Fatalf("delete credential: %v", err)
	}
	em := env.NewManager(store, "test-password")
	if err := em.Set("default", "SENV_ACME_API_KEY", ownedSeedRefTemplate("acme")); err != nil {
		t.Fatalf("seed env ref: %v", err)
	}

	res, err := mgr.RenameProvider("acme", "acme-prod", "")
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.EnvRefsUpdated != 1 {
		t.Fatalf("EnvRefsUpdated = %d, want 1", res.EnvRefsUpdated)
	}
	gotRef, err := em.Get("default", "SENV_ACME_API_KEY")
	if err != nil || gotRef != ownedSeedRefTemplate("acme-prod") {
		t.Fatalf("env ref = %q, %v", gotRef, err)
	}
}

func TestRenameProviderMissingOld(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.RenameProvider("missing", "new", ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenameProviderSameAliasNoop(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := mgr.RenameProvider("acme", "acme", "")
	if err != nil {
		t.Fatalf("RenameProvider: %v", err)
	}
	if res.PointersUpdated != 0 || res.Entry.Alias != "acme" {
		t.Fatalf("result = %+v", res)
	}
}

func TestRenameProviderPointerFileCorrupt(t *testing.T) {
	mgr, _, _ := newTestProviderManager(t)
	if _, err := mgr.AddProvider(AddProviderOptions{
		Alias: "acme", BaseURL: "https://api.example.com", APIKey: "sk",
		Models: []string{"m1"}, ModelContexts: map[string]int{"m1": 1000},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pointerPath := filepath.Join(t.TempDir(), "agent-pointers.json")
	if err := os.WriteFile(pointerPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	res, err := mgr.RenameProvider("acme", "acme-prod", pointerPath)
	if err == nil || !strings.Contains(err.Error(), "pointers were not updated") {
		t.Fatalf("error = %v", err)
	}
	if res == nil || res.Entry.Alias != "acme-prod" {
		t.Fatalf("vault rename should have succeeded: %+v", res)
	}
	if _, err := mgr.GetProvider("acme-prod"); err != nil {
		t.Fatalf("renamed profile missing after pointer failure: %v", err)
	}
}
