package cmd

import (
	"context"
	"fmt"
	"strings"

	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

type resolveRule struct {
	selector string
	decision provider.ResolutionDecision
	exact    bool
}

func parseResolveFlags(rawRules []string, rawDefault string) ([]resolveRule, provider.ResolutionDecision, error) {
	var defaultDecision provider.ResolutionDecision
	if strings.TrimSpace(rawDefault) != "" {
		d, err := parseResolutionDecision(rawDefault)
		if err != nil {
			return nil, "", fmt.Errorf("--resolve-default %w", err)
		}
		defaultDecision = d
	}

	rules := make([]resolveRule, 0, len(rawRules))
	for _, raw := range rawRules {
		sel, val, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(sel) == "" {
			return nil, "", fmt.Errorf("无效的 --resolve 参数 %q，期望格式 <selector>=<local|remote>", raw)
		}
		d, err := parseResolutionDecision(val)
		if err != nil {
			return nil, "", fmt.Errorf("--resolve %q %w", raw, err)
		}
		sel = strings.TrimSpace(sel)
		rules = append(rules, resolveRule{
			selector: sel,
			decision: d,
			exact:    !strings.ContainsAny(sel, "*?["),
		})
	}
	return rules, defaultDecision, nil
}

func parseResolutionDecision(raw string) (provider.ResolutionDecision, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "local":
		return provider.ResolutionLocal, nil
	case "remote":
		return provider.ResolutionRemote, nil
	default:
		return "", fmt.Errorf("决策值 %q 无效（仅支持 local 或 remote）", raw)
	}
}

func tryHealEquivalentConflicts(
	ctx context.Context,
	sp *provider.ServerProvider,
	conflictErr *provider.SyncConflictError,
	auth renderconflict.Auth,
) (int, *provider.SyncConflictError, error) {
	if conflictErr == nil || len(conflictErr.Details) == 0 || len(auth.Key) == 0 || !auth.RemoteKeyCompatible {
		return 0, conflictErr, nil
	}

	var healItems []provider.ConflictResolution
	var remDetails []provider.ConflictDetail
	var remConflicts []provider.Conflict
	conflictByID := make(map[string]provider.Conflict, len(conflictErr.Conflicts))
	for _, c := range conflictErr.Conflicts {
		conflictByID[c.Kind+"\x00"+c.Grp+"\x00"+c.Key] = c
	}

	for _, d := range conflictErr.Details {
		cls := renderconflict.ClassifyConflict(d, auth)
		if cls.Equivalent {
			healItems = append(healItems, provider.ConflictResolution{
				Kind:                   d.Kind,
				Grp:                    d.Grp,
				Key:                    d.Key,
				Decision:               provider.ResolutionRemote,
				ExpectedLocalHash:      d.Local.Hash,
				ExpectedRemoteRevision: d.Remote.Revision,
			})
			continue
		}
		remDetails = append(remDetails, d)
		if c, ok := conflictByID[d.Kind+"\x00"+d.Grp+"\x00"+d.Key]; ok {
			remConflicts = append(remConflicts, c)
		}
	}

	if len(healItems) == 0 {
		return 0, conflictErr, nil
	}

	plan := provider.ResolutionPlan{Items: healItems}
	if err := sp.ResolveConflicts(ctx, plan); err != nil {
		return 0, conflictErr, err
	}

	if len(remDetails) == 0 && !conflictErr.MetadataConflict {
		return len(healItems), nil, nil
	}
	return len(healItems), &provider.SyncConflictError{
		Conflicts:        remConflicts,
		MetadataConflict: conflictErr.MetadataConflict,
		Details:          remDetails,
		Metadata:         conflictErr.Metadata,
	}, nil
}

func buildResolvePlan(
	conflictErr *provider.SyncConflictError,
	rules []resolveRule,
	defaultDecision provider.ResolutionDecision,
	auth renderconflict.Auth,
) (provider.ResolutionPlan, error) {
	plan := provider.ResolutionPlan{
		Items: make([]provider.ConflictResolution, 0, len(conflictErr.Details)),
	}
	var unresolved []string

	chosen := make(map[string]provider.ConflictResolution, len(conflictErr.Details))
	var skillIndexDetail *provider.ConflictDetail
	var skillIndexDecision provider.ResolutionDecision

	for i := range conflictErr.Details {
		d := conflictErr.Details[i]
		dec, matched := selectDecisionForDetail(d, rules, defaultDecision)
		if !matched {
			unresolved = append(unresolved, fmt.Sprintf("%s:%s:%s", d.Kind, displayConflictGroup(d.Grp), displayConflictKey(d.Kind, d.Key)))
			continue
		}
		res := provider.ConflictResolution{
			Kind:                   d.Kind,
			Grp:                    d.Grp,
			Key:                    d.Key,
			Decision:               dec,
			ExpectedLocalHash:      d.Local.Hash,
			ExpectedRemoteRevision: d.Remote.Revision,
		}
		plan.Items = append(plan.Items, res)
		chosen[d.Kind+"\x00"+d.Grp+"\x00"+d.Key] = res
		if d.Kind == provider.KindSkillIndex {
			skillIndexDetail = &conflictErr.Details[i]
			skillIndexDecision = dec
		}
	}

	if conflictErr.MetadataConflict {
		dec, matched := selectMetadataDecision(rules, defaultDecision)
		if !matched {
			unresolved = append(unresolved, "metadata")
		} else {
			plan.Metadata = dec
			if conflictErr.Metadata != nil {
				plan.ExpectedLocalMetadataHash = provider.Hash(conflictErr.Metadata.Local)
				plan.ExpectedRemoteMetadataHash = provider.Hash(conflictErr.Metadata.Remote)
			}
		}
	}

	if len(unresolved) > 0 {
		return provider.ResolutionPlan{}, fmt.Errorf("以下冲突未被 --resolve 或 --resolve-default 覆盖：%s", strings.Join(unresolved, ", "))
	}

	// 护栏 2：skill_index 与 skill/* 引用一致性校验
	if skillIndexDetail != nil && len(auth.Key) > 0 {
		if err := validateSkillIndexConsistency(*skillIndexDetail, skillIndexDecision, conflictErr.Details, chosen, auth); err != nil {
			return provider.ResolutionPlan{}, err
		}
	}

	return plan, nil
}

func selectDecisionForDetail(d provider.ConflictDetail, rules []resolveRule, defaultDecision provider.ResolutionDecision) (provider.ResolutionDecision, bool) {
	var wildMatch provider.ResolutionDecision
	hasWild := false
	var exactMatch provider.ResolutionDecision
	hasExact := false

	for _, r := range rules {
		if !matchConflictSelector(r.selector, d) {
			continue
		}
		if r.exact {
			exactMatch = r.decision
			hasExact = true
		} else {
			wildMatch = r.decision
			hasWild = true
		}
	}
	if hasExact {
		return exactMatch, true
	}
	if hasWild {
		return wildMatch, true
	}
	if defaultDecision != "" {
		return defaultDecision, true
	}
	return "", false
}

func selectMetadataDecision(rules []resolveRule, defaultDecision provider.ResolutionDecision) (provider.ResolutionDecision, bool) {
	for _, r := range rules {
		if strings.EqualFold(r.selector, "metadata") {
			return r.decision, true
		}
	}
	if defaultDecision != "" {
		return defaultDecision, true
	}
	return "", false
}

func validateSkillIndexConsistency(
	indexDetail provider.ConflictDetail,
	indexDecision provider.ResolutionDecision,
	details []provider.ConflictDetail,
	chosen map[string]provider.ConflictResolution,
	auth renderconflict.Auth,
) error {
	var side provider.ConflictSide
	switch indexDecision {
	case provider.ResolutionLocal:
		side = indexDetail.Local
	case provider.ResolutionRemote:
		side = indexDetail.Remote
	default:
		return nil
	}
	if side.Deleted || len(side.Ciphertext) == 0 {
		return nil
	}
	plain, err := renderconflict.DecryptEntryPayload(provider.KindSkillIndex, "", "", side.Ciphertext, auth)
	if err != nil {
		return nil
	}
	var idx storage.SkillIndex
	if err := storage.FromJSON(plain, &idx); err != nil {
		return nil
	}
	detailMap := make(map[string]provider.ConflictDetail, len(details))
	for _, d := range details {
		detailMap[d.Kind+"\x00"+d.Grp+"\x00"+d.Key] = d
	}

	for id, entry := range idx.Skills {
		grp := entry.NormalizedGroup()
		checkKey := func(key string) error {
			mapID := provider.KindSkill + "\x00" + grp + "\x00" + key
			res, inConflict := chosen[mapID]
			if !inConflict {
				return nil
			}
			d := detailMap[mapID]
			willDelete := (res.Decision == provider.ResolutionLocal && d.Local.Deleted) ||
				(res.Decision == provider.ResolutionRemote && d.Remote.Deleted)
			if willDelete {
				return fmt.Errorf("裁决组合会导致断链：skill_index（选 %s）保留了 %s/%s，但 %s 却被裁决为删除",
					indexDecision, grp, id, key)
			}
			return nil
		}
		if err := checkKey(id); err != nil {
			return err
		}
		for _, v := range entry.Versions {
			if err := checkKey(storage.SkillVersionStorageKey(id, v.Version)); err != nil {
				return err
			}
		}
	}
	return nil
}

func tryMergeSkillIndexConflict(
	ctx context.Context,
	sp *provider.ServerProvider,
	conflictErr *provider.SyncConflictError,
	auth renderconflict.Auth,
) (bool, *provider.SyncConflictError, error) {
	if conflictErr == nil || len(auth.Key) == 0 || !auth.RemoteKeyCompatible {
		return false, conflictErr, nil
	}
	var idxDetail *provider.ConflictDetail
	for i := range conflictErr.Details {
		if conflictErr.Details[i].Kind == provider.KindSkillIndex {
			idxDetail = &conflictErr.Details[i]
			break
		}
	}
	if idxDetail == nil || idxDetail.Local.Deleted || idxDetail.Remote.Deleted || idxDetail.Local.Revision <= 0 {
		return false, conflictErr, nil
	}

	hist, err := sp.History(ctx, provider.HistoryFilter{Kind: provider.KindSkillIndex, Limit: 10})
	if err != nil {
		return false, conflictErr, nil
	}
	var baseCipher []byte
	for _, hv := range hist {
		if hv.Revision == idxDetail.Local.Revision && !hv.Deleted {
			baseCipher = hv.Ciphertext
			break
		}
	}
	if len(baseCipher) == 0 {
		return false, conflictErr, nil
	}

	localPlain, err := renderconflict.DecryptEntryPayload(provider.KindSkillIndex, "", "", idxDetail.Local.Ciphertext, renderconflict.Auth{
		Key: auth.Key, KeyEpoch: auth.KeyEpoch, RemoteKeyEpoch: auth.KeyEpoch, RemoteKeyCompatible: true,
	})
	if err != nil {
		return false, conflictErr, nil
	}
	remotePlain, err := renderconflict.DecryptEntryPayload(provider.KindSkillIndex, "", "", idxDetail.Remote.Ciphertext, auth)
	if err != nil {
		return false, conflictErr, nil
	}
	basePlain, err := renderconflict.DecryptEntryPayload(provider.KindSkillIndex, "", "", baseCipher, auth)
	if err != nil {
		return false, conflictErr, nil
	}

	var baseIdx, localIdx, remoteIdx storage.SkillIndex
	if storage.FromJSON(basePlain, &baseIdx) != nil ||
		storage.FromJSON(localPlain, &localIdx) != nil ||
		storage.FromJSON(remotePlain, &remoteIdx) != nil {
		return false, conflictErr, nil
	}

	merged, conflicts, err := renderconflict.MergeSkillIndex3Way(&baseIdx, &localIdx, &remoteIdx)
	if err != nil || len(conflicts) > 0 {
		return false, conflictErr, nil
	}

	encryptV2 := auth.KeyEpoch > 0 ||
		crypto.IsV2Ciphertext(string(idxDetail.Local.Ciphertext)) ||
		crypto.IsV2Ciphertext(string(idxDetail.Remote.Ciphertext))
	mergedCipher, err := renderconflict.EncryptMergedSkillIndex(merged, auth, encryptV2)
	if err != nil {
		return false, conflictErr, nil
	}

	plan := provider.ResolutionPlan{
		Items: []provider.ConflictResolution{{
			Kind:                   provider.KindSkillIndex,
			Grp:                    idxDetail.Grp,
			Key:                    idxDetail.Key,
			Decision:               provider.ResolutionMerged,
			MergedCiphertext:       mergedCipher,
			ExpectedLocalHash:      idxDetail.Local.Hash,
			ExpectedRemoteRevision: idxDetail.Remote.Revision,
		}},
	}
	if err := sp.ResolveConflicts(ctx, plan); err != nil {
		return false, conflictErr, err
	}

	var remDetails []provider.ConflictDetail
	var remConflicts []provider.Conflict
	for _, d := range conflictErr.Details {
		if d.Kind == provider.KindSkillIndex {
			continue
		}
		remDetails = append(remDetails, d)
	}
	for _, c := range conflictErr.Conflicts {
		if c.Kind == provider.KindSkillIndex {
			continue
		}
		remConflicts = append(remConflicts, c)
	}
	if len(remDetails) == 0 && !conflictErr.MetadataConflict {
		return true, nil, nil
	}
	return true, &provider.SyncConflictError{
		Conflicts:        remConflicts,
		MetadataConflict: conflictErr.MetadataConflict,
		Details:          remDetails,
		Metadata:         conflictErr.Metadata,
	}, nil
}
