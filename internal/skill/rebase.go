package skill

import (
	"fmt"

	"github.com/wii/senv/internal/storage"
)

// RebaseResult 描述一次 skill 版本号碰撞变基的结果。
type RebaseResult struct {
	ID               string
	Group            string
	CollisionVersion string
	RebasedVersion   string
}

// RebaseVersion 解决同一 skill 版本号在本地与远端内容碰撞的问题：
// 将远端碰撞版本快照保留为 collisionVer，并将本地当前新归档顺延重命名为合并版本集后的下一可用版本号。
func (m *Manager) RebaseVersion(id string, remoteEntry storage.SkillEntry, collisionVer string, remoteArchive []byte) (*RebaseResult, error) {
	if err := storage.ValidateSkillID(id); err != nil {
		return nil, err
	}
	if _, err := storage.ValidateSkillVersion(collisionVer); err != nil {
		return nil, err
	}
	if _, err := Validate(remoteArchive, id); err != nil {
		return nil, fmt.Errorf("validate remote skill archive: %w", err)
	}

	var res *RebaseResult
	err := m.mutate(func(locked *Manager) error {
		key, err := locked.cryptoKey()
		if err != nil {
			return err
		}
		index, err := locked.storage.LoadSkillIndexWithKey(key)
		if err != nil {
			return err
		}
		localEntry, ok := index.Skills[id]
		if !ok {
			return fmt.Errorf("skill %q not found in local index", id)
		}
		if localEntry.CurrentVersion != collisionVer {
			return fmt.Errorf("local current version of %q is %q, expected %q", id, localEntry.CurrentVersion, collisionVer)
		}
		group := localEntry.NormalizedGroup()
		localArchive, err := locked.storage.LoadSkillWithKey(group, id, key)
		if err != nil {
			return fmt.Errorf("load local skill archive: %w", err)
		}
		localVerMeta, hasLocalVer := localEntry.FindVersion(collisionVer)
		if !hasLocalVer {
			localVerMeta = storage.SkillVersion{
				Version: collisionVer,
				SHA256:  sha256Hex(localArchive),
				Size:    len(localArchive),
			}
		}

		// 合并版本历史：以远端历史为基底，保留其中所有快照，然后分配下一可用版本号给本地改动
		mergedVersions := make([]storage.SkillVersion, 0, len(remoteEntry.Versions)+1)
		seen := make(map[string]bool, len(remoteEntry.Versions))
		for _, rv := range remoteEntry.Versions {
			if seen[rv.Version] {
				continue
			}
			seen[rv.Version] = true
			mergedVersions = append(mergedVersions, rv)
		}
		for _, lv := range localEntry.Versions {
			if lv.Version == collisionVer || seen[lv.Version] {
				continue
			}
			seen[lv.Version] = true
			mergedVersions = append(mergedVersions, lv)
		}

		newVer := nextAutoVersion(mergedVersions)
		localVerMeta.Version = newVer
		if localVerMeta.SHA256 == "" {
			localVerMeta.SHA256 = sha256Hex(localArchive)
			localVerMeta.Size = len(localArchive)
		}
		mergedVersions = append(mergedVersions, localVerMeta)

		// 1. 落盘远端碰撞快照
		if err := locked.storage.SaveSkillVersionWithKey(group, id, collisionVer, remoteArchive, key); err != nil {
			return err
		}
		// 2. 落盘顺延后的本地新版本快照与活跃归档
		if err := locked.storage.SaveSkillVersionWithKey(group, id, newVer, localArchive, key); err != nil {
			return err
		}
		if err := locked.storage.SaveSkillWithKey(group, id, localArchive, key); err != nil {
			return err
		}

		localEntry.CurrentVersion = newVer
		localEntry.Versions = mergedVersions
		index.Skills[id] = localEntry
		if err := locked.storage.SaveSkillIndexWithKey(index, key); err != nil {
			return err
		}

		res = &RebaseResult{
			ID:               id,
			Group:            group,
			CollisionVersion: collisionVer,
			RebasedVersion:   newVer,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
