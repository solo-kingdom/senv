package cmd

import (
	"testing"

	"github.com/wii/senv/internal/provider"
	syncconflict "github.com/wii/senv/internal/tui/conflict"
)

func TestProviderResolutionPlanFromUI(t *testing.T) {
	conflict := &provider.SyncConflictError{
		MetadataConflict: true,
		Details: []provider.ConflictDetail{{
			Kind: "env", Grp: "default", Key: "API",
			Local:  provider.ConflictSide{Hash: "local-hash", Revision: 1},
			Remote: provider.ConflictSide{Hash: "remote-hash", Revision: 2},
		}},
		Metadata: &provider.MetadataConflictDetail{Local: []byte("local"), Remote: []byte("remote")},
	}
	uiPlan := syncconflict.Plan{
		Items: []syncconflict.Item{{
			Detail:     conflict.Details[0],
			Decision:   syncconflict.DecisionMerged,
			MergedData: []byte("merged"),
		}},
		Metadata:        syncconflict.DecisionRemote,
		MetadataPresent: true,
	}
	plan := providerResolutionPlan(uiPlan, conflict)
	if !planCompleteForCLI(plan) {
		t.Fatalf("plan unexpectedly incomplete: %+v", plan)
	}
	item := plan.Items[0]
	if item.Decision != provider.ResolutionMerged || string(item.MergedCiphertext) != "merged" ||
		item.ExpectedLocalHash != "local-hash" || item.ExpectedRemoteRevision != 2 {
		t.Fatalf("mapped item = %+v", item)
	}
	if plan.Metadata != provider.ResolutionRemote ||
		plan.ExpectedLocalMetadataHash != provider.Hash([]byte("local")) ||
		plan.ExpectedRemoteMetadataHash != provider.Hash([]byte("remote")) {
		t.Fatalf("mapped metadata plan = %+v", plan)
	}
}
