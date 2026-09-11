package tui

import "testing"

func TestSnapshotRegistrySharedAndInvalidated(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if m.mgr.snap == nil {
		t.Fatal("New should install a snapshot registry")
	}
	snap1, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap1.Vars["default"]["A"] != "1" {
		t.Fatalf("A = %q, want 1", snap1.Vars["default"]["A"])
	}

	if err := mgrs.Env.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	snap2, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap2.Vars["default"]["B"]; ok {
		t.Fatal("write must not appear in the cached snapshot before invalidate")
	}

	m.mgr.snap.Invalidate()
	snap3, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap3.Vars["default"]["B"] != "2" {
		t.Fatalf("B = %q after invalidate, want 2", snap3.Vars["default"]["B"])
	}
}

func TestWriteDoneInvalidatesSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "A", "1"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.Get(); err != nil {
		t.Fatal(err)
	}
	if err := mgrs.Env.Set("default", "B", "2"); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(envReloadMsg{})
	m = out.(Model)
	snap, err := m.mgr.snap.Get()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Vars["default"]["B"] != "2" {
		t.Fatalf("B = %q after write-done invalidate, want 2", snap.Vars["default"]["B"])
	}
}

func TestSearchReadsSharedEnvSnapshot(t *testing.T) {
	mgrs := newTestManagers(t)
	if err := mgrs.Env.Set("default", "API_KEY", "secret"); err != nil {
		t.Fatal(err)
	}
	m := New(mgrs)
	if _, err := m.mgr.snap.Get(); err != nil {
		t.Fatal(err)
	}
	s := newSearchTab(m.mgr)
	all := func() []searchResult {
		msg := s.gather()()
		gm, ok := msg.(searchGatheredMsg)
		if !ok {
			t.Fatalf("gather returned %T", msg)
		}
		return gm.all
	}()
	found := false
	for _, r := range all {
		if r.resultType == typeEnv && r.key == "API_KEY" {
			found = true
			if r.preview == "secret" {
				t.Fatal("search preview leaked env value")
			}
		}
	}
	if !found {
		t.Fatal("expected API_KEY from shared snapshot")
	}
}
