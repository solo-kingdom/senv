package provider

import (
	"context"
	"errors"
	"os"
	"testing"
)

func conflictFixture(t *testing.T, value string) (*fakeServer, *ServerProvider, *localCache, *SyncConflictError) {
	t.Helper()
	srv := newFakeServer()
	p, cache := newTestProvider(t, srv)
	ctx := context.Background()
	writeEnvVar(t, cache, "default", "A", "v1")
	if _, err := p.SyncWithReport(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, _, err := srv.Push(ctx, "main", []Entry{{
		Kind: KindEnv, Grp: "default", Key: "A",
		Ciphertext: []byte("remote-v2"), BaseRevision: 1,
	}}); err != nil {
		t.Fatalf("remote update: %v", err)
	}
	writeEnvVar(t, cache, "default", "A", value)
	_, err := p.SyncWithReport(ctx)
	var conflict *SyncConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("sync = %v, want conflict", err)
	}
	return srv, p, cache, conflict
}

func resolutionPlan(conflict *SyncConflictError, decision ResolutionDecision, merged []byte) ResolutionPlan {
	detail := conflict.Details[0]
	return ResolutionPlan{Items: []ConflictResolution{{
		Kind: detail.Kind, Grp: detail.Grp, Key: detail.Key,
		Decision: decision, MergedCiphertext: merged,
		ExpectedLocalHash:      detail.Local.Hash,
		ExpectedRemoteRevision: detail.Remote.Revision,
	}}}
}

func TestResolveConflictsPreflightRejectsStaleSides(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(srv *fakeServer, cache *localCache, plan *ResolutionPlan)
	}{
		{
			name: "local changed",
			mutate: func(_ *fakeServer, cache *localCache, _ *ResolutionPlan) {
				writeEnvVar(t, cache, "default", "A", "local-v3")
			},
		},
		{
			name: "remote changed",
			mutate: func(srv *fakeServer, _ *localCache, plan *ResolutionPlan) {
				_, _, _ = srv.Push(context.Background(), "main", []Entry{{
					Kind: KindEnv, Grp: "default", Key: "A",
					Ciphertext: []byte("remote-v3"), BaseRevision: 2,
				}})
				_ = plan
			},
		},
		{
			name: "candidate missing",
			mutate: func(_ *fakeServer, _ *localCache, plan *ResolutionPlan) {
				plan.Items[0].ExpectedRemoteRevision = 999
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, p, cache, conflict := conflictFixture(t, "local-v2")
			plan := resolutionPlan(conflict, ResolutionLocal, nil)
			tc.mutate(srv, cache, &plan)
			err := p.ResolveConflicts(context.Background(), plan)
			if err == nil {
				t.Fatal("stale resolution plan must fail")
			}
			data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
			if tc.name == "local changed" {
				if string(data) != "local-v3" {
					t.Fatalf("external local edit was overwritten: %q", data)
				}
			} else if string(data) != "local-v2" {
				t.Fatalf("local candidate was modified: %q", data)
			}
		})
	}
}

func TestResolveConflictsAppliesLocalRemoteAndMerged(t *testing.T) {
	cases := []struct {
		name       string
		decision   ResolutionDecision
		localWant  string
		remoteWant string
	}{
		{name: "local", decision: ResolutionLocal, localWant: "local-v2", remoteWant: "local-v2"},
		{name: "remote", decision: ResolutionRemote, localWant: "remote-v2", remoteWant: "remote-v2"},
		{name: "merged", decision: ResolutionMerged, localWant: "merged-v3", remoteWant: "merged-v3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, p, cache, conflict := conflictFixture(t, "local-v2")
			var merged []byte
			if tc.decision == ResolutionMerged {
				merged = []byte("merged-v3")
			}
			if err := p.ResolveConflicts(context.Background(), resolutionPlan(conflict, tc.decision, merged)); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
			if string(data) != tc.localWant {
				t.Fatalf("local = %q, want %q", data, tc.localWant)
			}
			got := srv.entries["main"][entryID(KindEnv, "default", "A")].Ciphertext
			if string(got) != tc.remoteWant {
				t.Fatalf("remote = %q, want %q", got, tc.remoteWant)
			}
			if _, err := p.SyncWithReport(context.Background()); err != nil {
				t.Fatalf("resolution must converge: %v", err)
			}
		})
	}
}

func TestResolveConflictsKeepsLocalWriteWhenPushFailsOrRemoteMoves(t *testing.T) {
	t.Run("network failure", func(t *testing.T) {
		srv, p, cache, conflict := conflictFixture(t, "local-v2")
		srv.failPush = true
		err := p.ResolveConflicts(context.Background(), resolutionPlan(conflict, ResolutionMerged, []byte("merged-v3")))
		if err == nil {
			t.Fatal("push failure must be reported")
		}
		data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
		if string(data) != "merged-v3" {
			t.Fatalf("confirmed local merge was rolled back: %q", data)
		}
		srv.failPush = false
		if _, syncErr := p.SyncWithReport(context.Background()); syncErr == nil {
			t.Fatal("stale remote must re-enter conflict")
		}
	})

	t.Run("remote moves during apply", func(t *testing.T) {
		srv, p, cache, conflict := conflictFixture(t, "local-v2")
		srv.beforePush = func() {
			srv.beforePush = nil
			_, _, _ = srv.Push(context.Background(), "main", []Entry{{
				Kind: KindEnv, Grp: "default", Key: "A",
				Ciphertext: []byte("remote-v3"), BaseRevision: 2,
			}})
		}
		err := p.ResolveConflicts(context.Background(), resolutionPlan(conflict, ResolutionMerged, []byte("merged-v3")))
		var renewed *SyncConflictError
		if !errors.As(err, &renewed) {
			t.Fatalf("resolve = %v, want renewed conflict", err)
		}
		data, _ := os.ReadFile(mustEntryPath(t, cache, KindEnv, "default", "A"))
		if string(data) != "merged-v3" {
			t.Fatalf("local merge must remain pending: %q", data)
		}
		if renewed.Details[0].Remote.Revision != 3 || string(renewed.Details[0].Remote.Ciphertext) != "remote-v3" {
			t.Fatalf("renewed detail = %+v, want remote-v3@3", renewed.Details[0])
		}
	})
}

func TestResolveConflictsMetadataWholeSide(t *testing.T) {
	srv, p, cache, _ := conflictFixture(t, "local-v2")
	localMeta := []byte("local-meta")
	remoteMeta := []byte("remote-meta")
	if err := os.WriteFile(cache.metadataPath(), localMeta, 0o600); err != nil {
		t.Fatal(err)
	}
	srv.metadata["main"] = remoteMeta
	// Refresh conflict so metadata diagnosis and hashes are current.
	_, err := p.SyncWithReport(context.Background())
	var withMeta *SyncConflictError
	if !errors.As(err, &withMeta) {
		t.Fatalf("sync = %v, want metadata conflict", err)
	}
	plan := resolutionPlan(withMeta, ResolutionLocal, nil)
	plan.Metadata = ResolutionLocal
	plan.ExpectedLocalMetadataHash = hashBytes(localMeta)
	plan.ExpectedRemoteMetadataHash = hashBytes(remoteMeta)
	if err := p.ResolveConflicts(context.Background(), plan); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(srv.metadata["main"]) != "local-meta" {
		t.Fatalf("server metadata = %q, want local", srv.metadata["main"])
	}
	if _, err := p.SyncWithReport(context.Background()); err != nil {
		t.Fatalf("resolve must converge: %v", err)
	}
}
