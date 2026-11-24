package ref

import (
	"fmt"
	"strings"
	"testing"
)

type mapGetter map[string]string

func (m mapGetter) GetEnvValue(group, key string) (string, error) {
	v, ok := m[group+":"+key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (m mapGetter) GetTextValue(group, key string) (string, error) {
	return m.GetEnvValue(group, key)
}

func TestResolveBlocksBillionLaughsExpansion(t *testing.T) {
	g := mapGetter{
		"g:K0": "aaaaaaaaaa",
		"g:K1": "{{env:g:K0}}{{env:g:K0}}{{env:g:K0}}{{env:g:K0}}{{env:g:K0}}",
		"g:K2": "{{env:g:K1}}{{env:g:K1}}{{env:g:K1}}{{env:g:K1}}{{env:g:K1}}",
		"g:K3": "{{env:g:K2}}{{env:g:K2}}{{env:g:K2}}{{env:g:K2}}{{env:g:K2}}",
		"g:K4": "{{env:g:K3}}{{env:g:K3}}{{env:g:K3}}{{env:g:K3}}{{env:g:K3}}",
	}
	_, err := Resolve("{{env:g:K4}}", g, ResolveOptions{MaxExpansions: 50})
	if err == nil || !strings.Contains(err.Error(), "expansions exceeded") {
		t.Fatalf("expected expansion limit error, got: %v", err)
	}

	_, err = Resolve("{{env:g:K2}}", g, ResolveOptions{MaxOutputBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("expected output size limit error, got: %v", err)
	}
}
