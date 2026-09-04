package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/wii/senv/internal/syncschema"
)

type ResolutionDecision string

const (
	ResolutionLocal  ResolutionDecision = "local"
	ResolutionRemote ResolutionDecision = "remote"
	ResolutionMerged ResolutionDecision = "merged"
)

type ConflictResolution struct {
	Kind                   string
	Grp                    string
	Key                    string
	Decision               ResolutionDecision
	MergedCiphertext       []byte
	ExpectedLocalHash      string
	ExpectedRemoteRevision int64
}

type ResolutionPlan struct {
	Items                      []ConflictResolution
	Metadata                   ResolutionDecision
	ExpectedLocalMetadataHash  string
	ExpectedRemoteMetadataHash string
}

// Hash exposes the provider's ciphertext hash helper to the command layer so
// resolution plans can record exact metadata preflight values without another
// package-local crypto implementation.
func Hash(data []byte) string { return hashBytes(data) }

func (p *ServerProvider) ResolveConflicts(ctx context.Context, plan ResolutionPlan) error {
	release, err := p.lockBlocking()
	if err != nil {
		return err
	}
	defer release()
	return p.withVaultMutation(func() error { return p.resolveConflictsLocked(ctx, plan) })
}

func (p *ServerProvider) resolveConflictsLocked(ctx context.Context, plan ResolutionPlan) error {
	if len(plan.Items) == 0 && plan.Metadata != ResolutionLocal && plan.Metadata != ResolutionRemote {
		return fmt.Errorf("empty conflict resolution plan")
	}
	seen := make(map[string]bool, len(plan.Items))
	for _, item := range plan.Items {
		if err := syncschema.ValidateIdentity(item.Kind, item.Grp, item.Key); err != nil {
			return fmt.Errorf("invalid resolution identity: %w", err)
		}
		id := entryID(item.Kind, item.Grp, item.Key)
		if seen[id] {
			return fmt.Errorf("duplicate resolution for %s/%s/%s", item.Kind, item.Grp, item.Key)
		}
		seen[id] = true
		switch item.Decision {
		case ResolutionLocal, ResolutionRemote:
		case ResolutionMerged:
			if len(item.MergedCiphertext) == 0 {
				return fmt.Errorf("merged resolution for %s/%s/%s has no ciphertext", item.Kind, item.Grp, item.Key)
			}
		default:
			return fmt.Errorf("invalid resolution decision %q", item.Decision)
		}
	}

	st, err := p.cache.loadState()
	if err != nil {
		return err
	}
	current, err := p.cache.collect()
	if err != nil {
		return err
	}
	dirty := p.collectDirty(st, current)
	dirtyByID := entryMap(dirty)

	remote, remoteLatest, err := p.api.Pull(ctx, p.vault, 0)
	if err != nil {
		return err
	}
	if err := validateRemoteEntries(remote); err != nil {
		return err
	}
	remoteByID := entryMap(remote)

	var writes, pushes []Entry
	for _, item := range plan.Items {
		id := entryID(item.Kind, item.Grp, item.Key)
		local, hasLocal := dirtyByID[id]
		if !hasLocal {
			return fmt.Errorf("conflict %s/%s/%s is no longer dirty; refresh conflicts", item.Kind, item.Grp, item.Key)
		}
		if got := hashBytes(local.Ciphertext); got != item.ExpectedLocalHash {
			return fmt.Errorf("local conflict %s/%s/%s changed during resolution", item.Kind, item.Grp, item.Key)
		}
		remoteEntry, hasRemote := remoteByID[id]
		remoteRevision := int64(0)
		if hasRemote {
			remoteRevision = remoteEntry.Revision
		}
		if remoteRevision != item.ExpectedRemoteRevision {
			return fmt.Errorf("remote conflict %s/%s/%s changed during resolution", item.Kind, item.Grp, item.Key)
		}

		switch item.Decision {
		case ResolutionRemote:
			writes = append(writes, remoteEntry)
			if !remoteEntry.Deleted {
				st.Entries[id] = syncEntryState{Revision: remoteEntry.Revision, Hash: hashBytes(remoteEntry.Ciphertext)}
			} else {
				delete(st.Entries, id)
			}
		case ResolutionLocal:
			writes = append(writes, local)
			pushes = append(pushes, Entry{
				Kind: item.Kind, Grp: item.Grp, Key: item.Key,
				Ciphertext: local.Ciphertext, BaseRevision: remoteRevision, Deleted: local.Deleted,
			})
		case ResolutionMerged:
			merged := Entry{Kind: item.Kind, Grp: item.Grp, Key: item.Key, Ciphertext: item.MergedCiphertext}
			writes = append(writes, merged)
			pushes = append(pushes, Entry{
				Kind: item.Kind, Grp: item.Grp, Key: item.Key,
				Ciphertext: item.MergedCiphertext, BaseRevision: remoteRevision,
			})
		}
	}

	localMeta, err := p.cache.readMetadata()
	if err != nil {
		return err
	}
	remoteMeta, err := p.api.GetMetadata(ctx, p.vault)
	if err != nil && !errors.Is(err, ErrVaultNotFound) {
		return err
	}
	if plan.Metadata == ResolutionLocal || plan.Metadata == ResolutionRemote {
		if got := hashBytes(localMeta); got != plan.ExpectedLocalMetadataHash {
			return fmt.Errorf("local metadata changed during resolution")
		}
		if got := hashBytes(remoteMeta); got != plan.ExpectedRemoteMetadataHash {
			return fmt.Errorf("remote metadata changed during resolution")
		}
	}

	st.LastSyncedRevision = remoteLatest
	var metadataToApply []byte
	applyMetadata := false
	if plan.Metadata == ResolutionRemote {
		metadataToApply, applyMetadata = remoteMeta, true
		st.MetadataHash = hashBytes(remoteMeta)
	}
	if err := p.cache.applyRemote(writes, metadataToApply, applyMetadata, st); err != nil {
		return err
	}

	if len(pushes) > 0 {
		pushed, latest, err := p.api.Push(ctx, p.vault, pushes)
		if err != nil {
			var conflictErr *ConflictError
			if errors.As(err, &conflictErr) {
				return p.buildConflictError(ctx, conflictErr.Conflicts, false, pushes, remote, localMeta, remoteMeta)
			}
			return err
		}
		st.LastSyncedRevision = latest
		for _, entry := range pushed {
			id := entryID(entry.Kind, entry.Grp, entry.Key)
			if entry.Deleted {
				delete(st.Entries, id)
			} else {
				st.Entries[id] = syncEntryState{Revision: entry.Revision, Hash: hashBytes(entry.Ciphertext)}
			}
		}
	}

	if plan.Metadata == ResolutionLocal {
		if err := p.api.PutMetadata(ctx, p.vault, localMeta); err != nil {
			return err
		}
		st.MetadataHash = hashBytes(localMeta)
	}
	return p.cache.saveState(st)
}
