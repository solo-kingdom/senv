package conflict

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// MergeSkillIndex3Way 基于精确的共同祖先 base 对 local 与 remote 的 SkillIndex 做严格三方合并。
// 只有当同一 group 或同一 skill 在两侧相对 base 均发生不同修改时才返回 conflicts，绝不静默覆盖。
func MergeSkillIndex3Way(base, local, remote *storage.SkillIndex) (*storage.SkillIndex, []string, error) {
	if base == nil || local == nil || remote == nil {
		return nil, nil, fmt.Errorf("3-way merge requires non-nil base, local, and remote skill_index")
	}

	merged := storage.NewSkillIndex()
	var conflicts []string

	// 1. 合并 Groups
	groupNames := make(map[string]bool)
	for g := range base.Groups {
		groupNames[g] = true
	}
	for g := range local.Groups {
		groupNames[g] = true
	}
	for g := range remote.Groups {
		groupNames[g] = true
	}
	orderedGroups := make([]string, 0, len(groupNames))
	for g := range groupNames {
		orderedGroups = append(orderedGroups, g)
	}
	sort.Strings(orderedGroups)

	for _, g := range orderedGroups {
		bg, hasB := base.Groups[g]
		lg, hasL := local.Groups[g]
		rg, hasR := remote.Groups[g]

		lChanged := hasL != hasB || (hasL && !reflect.DeepEqual(lg, bg))
		rChanged := hasR != hasB || (hasR && !reflect.DeepEqual(rg, bg))

		switch {
		case !lChanged && !rChanged:
			if hasB {
				merged.Groups[g] = bg
			}
		case lChanged && !rChanged:
			if hasL {
				merged.Groups[g] = lg
			}
		case !lChanged && rChanged:
			if hasR {
				merged.Groups[g] = rg
			}
		default:
			if hasL == hasR && (!hasL || reflect.DeepEqual(lg, rg)) {
				if hasL {
					merged.Groups[g] = lg
				}
			} else {
				conflicts = append(conflicts, "group:"+g)
			}
		}
	}

	// 2. 合并 Skills
	skillIDs := make(map[string]bool)
	for id := range base.Skills {
		skillIDs[id] = true
	}
	for id := range local.Skills {
		skillIDs[id] = true
	}
	for id := range remote.Skills {
		skillIDs[id] = true
	}
	orderedSkills := make([]string, 0, len(skillIDs))
	for id := range skillIDs {
		orderedSkills = append(orderedSkills, id)
	}
	sort.Strings(orderedSkills)

	for _, id := range orderedSkills {
		bs, hasB := base.Skills[id]
		ls, hasL := local.Skills[id]
		rs, hasR := remote.Skills[id]

		lChanged := hasL != hasB || (hasL && !reflect.DeepEqual(ls, bs))
		rChanged := hasR != hasB || (hasR && !reflect.DeepEqual(rs, bs))

		switch {
		case !lChanged && !rChanged:
			if hasB {
				merged.Skills[id] = bs
			}
		case lChanged && !rChanged:
			if hasL {
				merged.Skills[id] = ls
			}
		case !lChanged && rChanged:
			if hasR {
				merged.Skills[id] = rs
			}
		default:
			if hasL == hasR && (!hasL || reflect.DeepEqual(ls, rs)) {
				if hasL {
					merged.Skills[id] = ls
				}
			} else {
				conflicts = append(conflicts, "skill:"+id)
			}
		}
	}

	if len(conflicts) > 0 {
		return nil, conflicts, nil
	}
	return merged, nil, nil
}

// EncryptMergedSkillIndex 把合并后的 SkillIndex 序列化并按当前 epoch 加密（或保留明文模式）。
func EncryptMergedSkillIndex(merged *storage.SkillIndex, auth Auth, encryptV2 bool) ([]byte, error) {
	plain, err := storage.ToJSON(merged)
	if err != nil {
		return nil, err
	}
	if !encryptV2 {
		return plain, nil
	}
	if len(auth.Key) == 0 {
		return nil, fmt.Errorf("encrypt merged skill_index requires vault key")
	}
	aad := crypto.BuildEntryAAD(provider.KindSkillIndex, "", "", auth.KeyEpoch)
	cipher, err := crypto.EncryptWithAAD(auth.Key, plain, aad)
	if err != nil {
		return nil, err
	}
	return []byte(cipher), nil
}
