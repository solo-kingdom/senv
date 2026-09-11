package llm

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const metaCatalogPayload = `{
	"p1": {"id":"p1","name":"P1","models":{
		"m1": {"id":"m1","name":"Model One","description":"first model",
			"limit":{"context":300000,"output":8192},
			"modalities":{"input":["text","image"]},
			"reasoning_options":[{"type":"effort","values":["low","high"],"default":"high"},{"type":"budget_tokens","values":["1"]}]},
		"m2": {"id":"m2"}
	}}
}`

func TestLoadModelMetadataDefaultsWithoutCache(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		path     string
		provider string
	}{
		{"missing cache", filepath.Join(dir, "nope.json"), "p1"},
		{"no catalog path", "", "p1"},
		{"no provider id", filepath.Join(dir, "nope.json"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LoadModelMetadata(tc.path, tc.provider, []string{"m1"}); len(got) != 0 {
				t.Fatalf("metadata = %v, want empty", got)
			}
		})
	}
}

func TestLoadModelMetadataReadsKnownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "models-dev.json")
	writeTestCatalog(t, path, metaCatalogPayload, time.Now())

	got := LoadModelMetadata(path, "p1", []string{"m1", "m2", "m3"})
	m1, ok := got["m1"]
	if !ok {
		t.Fatalf("metadata = %v, want m1 present", got)
	}
	if m1.Name != "Model One" || m1.Description != "first model" {
		t.Fatalf("m1 = %+v", m1)
	}
	if m1.ContextLimit != 300000 || m1.OutputLimit != 8192 {
		t.Fatalf("m1 limits = %+v", m1)
	}
	// 只取 effort 维度，budget_tokens 不是 codex reasoning level。
	if !slices.Equal(m1.ReasoningEfforts, []string{"low", "high"}) {
		t.Fatalf("m1 efforts = %v", m1.ReasoningEfforts)
	}
	if m1.DefaultReasoning != "high" {
		t.Fatalf("m1 default reasoning = %q, want catalog default", m1.DefaultReasoning)
	}
	if !slices.Equal(m1.InputModalities, []string{"text", "image"}) {
		t.Fatalf("m1 modalities = %v", m1.InputModalities)
	}
	if m2, ok := got["m2"]; !ok || m2.Name != "" || m2.ContextLimit != 0 || len(m2.ReasoningEfforts) != 0 ||
		m2.DefaultReasoning != "" || len(m2.InputModalities) != 0 {
		t.Fatalf("m2 = %+v (ok=%v), want zero-value entry", m2, ok)
	}
	if _, ok := got["m3"]; ok {
		t.Fatalf("m3 should be absent from the catalog: %v", got)
	}
}

func TestLoadModelMetadataUnknownProviderOrCorruptCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "models-dev.json")
	writeTestCatalog(t, path, metaCatalogPayload, time.Now())

	if got := LoadModelMetadata(path, "other", []string{"m1"}); len(got) != 0 {
		t.Fatalf("unknown provider metadata = %v, want empty", got)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got := LoadModelMetadata(corrupt, "p1", []string{"m1"}); len(got) != 0 {
		t.Fatalf("corrupt cache metadata = %v, want empty", got)
	}
}

func TestDefaultModelCatalogPath(t *testing.T) {
	got := DefaultModelCatalogPath(filepath.Join("/home/u", ".config", "senv", "agent-pointers.json"))
	want := filepath.Join("/home/u", ".config", "senv", "cache", "models-dev.json")
	if got != want {
		t.Fatalf("DefaultModelCatalogPath = %q, want %q", got, want)
	}
	if got := DefaultModelCatalogPath(""); got != "" {
		t.Fatalf("DefaultModelCatalogPath(\"\") = %q, want empty", got)
	}
}
