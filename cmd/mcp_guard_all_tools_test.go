package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

func assertMCPToolDenied[Input any](t *testing.T, name string, handler func(*managers, context.Context, *mcp.CallToolRequest, Input) (*mcp.CallToolResult, emptyOut, error), input Input) {
	t.Helper()
	authorizeCalls := 0
	autoPullCalls := 0
	authorize := func() (*managers, func(), error) {
		authorizeCalls++
		return nil, nil, session.ErrMCPRevoked
	}
	wrapped := guardMCPTool(authorize, func() { autoPullCalls++ }, handler)
	result, _, err := wrapped(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("%s returned Go error: %v", name, err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("%s did not return a tool-level revocation", name)
	}
	if got := textOf(t, result); got != session.MCPRevocationMessage {
		t.Fatalf("%s error = %q, want sanitized revocation", name, got)
	}
	if authorizeCalls != 1 {
		t.Fatalf("%s authorization calls = %d, want 1", name, authorizeCalls)
	}
	if autoPullCalls != 0 {
		t.Fatalf("%s auto-pull calls = %d before authorization", name, autoPullCalls)
	}
}

func TestAllMCPToolsGuarded(t *testing.T) {
	assertMCPToolDenied(t, "senv_env_get", (*managers).envGet, envGetInput{Key: "SECRET"})
	assertMCPToolDenied(t, "senv_env_set", (*managers).envSet, envSetValueInput{Key: "SECRET", Value: "plaintext"})
	assertMCPToolDenied(t, "senv_env_delete", (*managers).envDelete, envKeyInput{Key: "SECRET"})
	assertMCPToolDenied(t, "senv_env_list", (*managers).envList, listInput{})
	assertMCPToolDenied(t, "senv_env_export", (*managers).envExport, struct{}{})
	assertMCPToolDenied(t, "senv_text_get", (*managers).textGet, envGetInput{Key: "SECRET"})
	assertMCPToolDenied(t, "senv_text_set", (*managers).textSet, envSetValueInput{Key: "SECRET", Value: "plaintext"})
	assertMCPToolDenied(t, "senv_text_delete", (*managers).textDelete, envKeyInput{Key: "SECRET"})
	assertMCPToolDenied(t, "senv_text_list", (*managers).textList, listInput{Group: "default"})
	assertMCPToolDenied(t, "senv_config_list", (*managers).configList, struct{}{})
	assertMCPToolDenied(t, "senv_config_get", (*managers).configGet, configNameInput{Name: "secret"})
	assertMCPToolDenied(t, "senv_config_export", (*managers).configExport, configNameInput{Name: "secret"})
	assertMCPToolDenied(t, "senv_group_list", (*managers).groupList, listInput{})
	assertMCPToolDenied(t, "senv_group_add", (*managers).groupAdd, groupKindInput{Kind: "env", Name: "prod"})
	assertMCPToolDenied(t, "senv_group_activate", (*managers).groupActivate, groupNameInput{Name: "prod"})
	assertMCPToolDenied(t, "senv_group_deactivate", (*managers).groupDeactivate, groupNameInput{Name: "prod"})
}

func TestMCPRevocationNoSideEffects(t *testing.T) {
	fixture := newMCPGuardFixture(t, "never")
	store := storage.NewManager(fixture.configPath, fixture.dataPath)
	passwordManager := env.NewManager(store, "correct-secret")
	const secret = "super-secret-plaintext"
	if err := passwordManager.Set("default", "API_KEY", secret); err != nil {
		t.Fatalf("seed env: %v", err)
	}

	cacheData, err := os.ReadFile(fixture.cachePath)
	if err != nil {
		t.Fatalf("read cache before clear: %v", err)
	}
	var cache session.SessionCache
	if err := json.Unmarshal(cacheData, &cache); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	cachedKeyText := cache.Key

	if err := fixture.session.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	autoPullCalls := 0
	get := guardMCPTool(fixture.authorize, func() { autoPullCalls++ }, (*managers).envGet)
	getResult, _, err := get(context.Background(), nil, envGetInput{Key: "API_KEY"})
	if err != nil {
		t.Fatalf("revoked get Go error: %v", err)
	}
	getText := textOf(t, getResult)
	if !getResult.IsError || getText != session.MCPRevocationMessage || strings.Contains(getText, secret) {
		t.Fatalf("revoked get leaked or returned wrong error: %q", getText)
	}

	set := guardMCPTool(fixture.authorize, func() { autoPullCalls++ }, (*managers).envSet)
	setResult, _, err := set(context.Background(), nil, envSetValueInput{Key: "API_KEY", Value: "mutated"})
	if err != nil {
		t.Fatalf("revoked set Go error: %v", err)
	}
	if !setResult.IsError || textOf(t, setResult) != session.MCPRevocationMessage {
		t.Fatalf("revoked set returned %q", textOf(t, setResult))
	}
	if autoPullCalls != 0 {
		t.Fatalf("revoked requests performed %d auto-pulls", autoPullCalls)
	}
	value, err := passwordManager.Get("default", "API_KEY")
	if err != nil || value != secret {
		t.Fatalf("revoked mutation changed value to %q (err=%v)", value, err)
	}

	auditPath := filepath.Join(os.Getenv("HOME"), ".log", "senv", "audit.log")
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	audit := string(auditData)
	if !strings.Contains(audit, string(session.AuditMCPRevocation)) || !strings.Contains(audit, session.MCPRevocationMessage) {
		t.Fatalf("audit missing sanitized revocation event: %s", audit)
	}
	for _, forbidden := range []string{secret, "mutated", cachedKeyText, fixture.configPath, fixture.dataPath} {
		if forbidden != "" && strings.Contains(audit, forbidden) {
			t.Fatalf("audit contains forbidden sensitive value %q", forbidden)
		}
	}
}
