package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
	syncconflict "github.com/wii/senv/internal/tui/conflict"
)

// cachedConflictAuth returns a decrypting key only when one is already cached.
// The interactive resolver deliberately does not prompt before the user asks
// for plaintext; `senv sync` itself remains token-only.
func cachedConflictAuth(remoteMetadata []byte) renderconflict.Auth {
	configPath, dataPath := getConfigPath(), getDataPath()
	if auth := lookupAuthMemo(configPath, dataPath); auth != nil {
		if key, err := resolveKeyForAuth(auth); err == nil {
			return renderconflict.NewAuth(key, remoteMetadata)
		}
	}
	key, err := session.NewManager(configPath, dataPath).GetCachedKey()
	if err != nil || len(key) == 0 {
		return renderconflict.Auth{}
	}
	return renderconflict.NewAuth(key, remoteMetadata)
}

func providerResolutionPlan(
	plan syncconflict.Plan, conflict *provider.SyncConflictError,
) provider.ResolutionPlan {
	out := provider.ResolutionPlan{Items: make([]provider.ConflictResolution, 0, len(plan.Items))}
	for _, item := range plan.Items {
		decision := provider.ResolutionDecision(item.Decision)
		out.Items = append(out.Items, provider.ConflictResolution{
			Kind: item.Detail.Kind, Grp: item.Detail.Grp, Key: item.Detail.Key,
			Decision: decision, MergedCiphertext: item.MergedData,
			ExpectedLocalHash:      item.Detail.Local.Hash,
			ExpectedRemoteRevision: item.Detail.Remote.Revision,
		})
	}
	if plan.MetadataPresent {
		switch plan.Metadata {
		case syncconflict.DecisionLocal:
			out.Metadata = provider.ResolutionLocal
		case syncconflict.DecisionRemote:
			out.Metadata = provider.ResolutionRemote
		}
		if conflict.Metadata != nil {
			out.ExpectedLocalMetadataHash = provider.Hash(conflict.Metadata.Local)
			out.ExpectedRemoteMetadataHash = provider.Hash(conflict.Metadata.Remote)
		}
	}
	return out
}

func runSyncConflictResolver(
	cmd *cobra.Command, sp *provider.ServerProvider, conflict *provider.SyncConflictError,
) error {
	var remoteMetadata []byte
	if conflict.Metadata != nil {
		remoteMetadata = conflict.Metadata.Remote
	}
	model := syncconflict.New(conflict, cachedConflictAuth(remoteMetadata))
	program := tea.NewProgram(model, tea.WithAltScreen())
	final, runErr := program.Run()
	if runErr != nil {
		return fmt.Errorf("运行冲突解决器失败: %w", runErr)
	}
	result, ok := final.(syncconflict.Model)
	if !ok {
		return fmt.Errorf("冲突解决器返回了意外状态")
	}
	if !result.Confirmed() {
		fmt.Fprintln(cmd.OutOrStdout(), "已退出冲突解决器；两端数据均未修改")
		return nil
	}
	plan := providerResolutionPlan(result.Plan(), conflict)
	if !planCompleteForCLI(plan) {
		return fmt.Errorf("冲突解决计划不完整")
	}
	if err := sp.ResolveConflicts(cmd.Context(), plan); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "✓ 冲突解决完成")
	return nil
}

func planCompleteForCLI(plan provider.ResolutionPlan) bool {
	for _, item := range plan.Items {
		switch item.Decision {
		case provider.ResolutionLocal, provider.ResolutionRemote, provider.ResolutionMerged:
		default:
			return false
		}
	}
	return true
}
