package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wii/senv/internal/search"
	"github.com/wii/senv/internal/session"
)

func TestMCPSearchHitsWithoutValue(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	ctx := context.Background()
	secret := "only-in-secret-body"
	desc := "payment gateway token"
	if err := m.env.AddGroup("svc", "services"); err != nil {
		t.Fatal(err)
	}
	if err := m.env.SetWithDescription("svc", "PAY_TOKEN", secret, &desc); err != nil {
		t.Fatal(err)
	}

	res, _, err := m.search(ctx, nil, searchInput{Query: "payment"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", textOf(t, res))
	}
	body := textOf(t, res)
	if strings.Contains(body, secret) || strings.Contains(body, `"value"`) {
		t.Fatalf("leaked value: %s", body)
	}
	var out search.Result
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 1 || out.Hits[0].Key != "PAY_TOKEN" {
		t.Fatalf("hits = %+v", out.Hits)
	}
}

func TestMCPSearchEmptyQuery(t *testing.T) {
	m, _, _ := newManagersForTest(t, "pw")
	res, _, err := m.search(context.Background(), nil, searchInput{Query: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("want tool error for empty query")
	}
}

func TestMCPSearchGuardedWithoutSession(t *testing.T) {
	assertMCPToolDenied(t, "senv_search", (*managers).search, searchInput{Query: "x"})
}

func TestMCPCatalogueIncludesSearch(t *testing.T) {
	found := false
	for _, tl := range toolCatalogue() {
		if tl.Name == "senv_search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("catalogue missing senv_search")
	}
}

func TestMCPSearchRevokedSession(t *testing.T) {
	// ensure guard path uses same revocation message as other tools
	authorize := func() (*managers, func(), error) {
		return nil, nil, session.ErrMCPRevoked
	}
	wrapped := guardMCPTool("senv_search", authorize, nil, (*managers).search)
	res, _, err := wrapped(context.Background(), nil, searchInput{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || textOf(t, res) != session.MCPRevocationMessage {
		t.Fatalf("got %q", textOf(t, res))
	}
}
