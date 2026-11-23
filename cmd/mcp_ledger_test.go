package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/wii/senv/internal/ledger"
	"github.com/wii/senv/internal/storage"
)

// newLedgerManagersForTest builds managers with a ledger manager backed by a
// freshly initialized temp project.
func newLedgerManagersForTest(t *testing.T, password string) *managers {
	t.Helper()
	m, _, _ := newManagersForTest(t, password)
	dir := t.TempDir()
	store := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := store.Initialize(password); err != nil {
		t.Fatalf("init ledger store: %v", err)
	}
	key, err := store.DeriveKeyFromPassword(password)
	if err != nil {
		t.Fatalf("derive key: %v", err)
	}
	m.ledger = ledger.NewManager(store, key)
	return m
}

func TestMCPLedgerCatalogue(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range toolCatalogue() {
		names[tl.Name] = true
	}
	for _, want := range []string{"senv_ledger_list", "senv_ledger_get", "senv_ledger_append", "senv_ledger_update"} {
		if !names[want] {
			t.Fatalf("catalogue missing %s", want)
		}
	}
	if names["senv_ledger_delete"] {
		t.Fatal("ledger must expose no delete tool")
	}
}

func TestMCPLedgerAppendGetAndUpdate(t *testing.T) {
	m := newLedgerManagersForTest(t, "pw")
	ctx := context.Background()

	h, err := ledger.NewHead("device", "lnv", "self-hosted node", []string{"homelab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ledger.SaveHead("personal", h); err != nil {
		t.Fatal(err)
	}

	// list
	res, _, err := m.ledgerList(ctx, nil, ledgerListInput{Group: "personal"})
	if err != nil || res.IsError {
		t.Fatalf("list = %v err=%v", res, err)
	}
	if !strings.Contains(textOf(t, res), `"id": "lnv"`) {
		t.Fatalf("list payload missing entity: %s", textOf(t, res))
	}

	// append
	res, _, err = m.ledgerAppend(ctx, nil, ledgerAppendInput{
		Entity: "device.lnv", Group: "personal", Summary: "upgraded", Body: "did things", Agent: "test",
	})
	if err != nil || res.IsError {
		t.Fatalf("append = %v err=%v", res, err)
	}

	// get
	res, _, err = m.ledgerGet(ctx, nil, ledgerGetInput{Entity: "device.lnv", Group: "personal"})
	if err != nil || res.IsError {
		t.Fatalf("get = %v err=%v", res, err)
	}
	if !strings.Contains(textOf(t, res), "upgraded") {
		t.Fatalf("get payload missing record: %s", textOf(t, res))
	}

	// update status
	res, _, err = m.ledgerUpdate(ctx, nil, ledgerUpdateInput{
		Entity: "device.lnv", Group: "personal", Status: "online", Level: "ok",
	})
	if err != nil || res.IsError {
		t.Fatalf("update status = %v err=%v", res, err)
	}
	// update field
	res, _, err = m.ledgerUpdate(ctx, nil, ledgerUpdateInput{
		Entity: "device.lnv", Group: "personal", Field: "location", Value: "home",
	})
	if err != nil || res.IsError {
		t.Fatalf("update field = %v err=%v", res, err)
	}

	head, err := m.ledger.GetHead("personal", "device.lnv")
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Status) != 1 || head.Status[0].Text != "online" {
		t.Fatalf("status = %+v", head.Status)
	}
	if len(head.Fields) != 1 || head.Fields[0].Name != "location" || head.Fields[0].Value != "home" {
		t.Fatalf("fields = %+v", head.Fields)
	}
}

func TestMCPLedgerUpdateRequiresFieldOrStatus(t *testing.T) {
	m := newLedgerManagersForTest(t, "pw")
	h, _ := ledger.NewHead("device", "lnv", "node", nil)
	if err := m.ledger.SaveHead("default", h); err != nil {
		t.Fatal(err)
	}
	res, _, err := m.ledgerUpdate(context.Background(), nil, ledgerUpdateInput{Entity: "device.lnv"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("update with neither field nor status must be a tool error")
	}
}
