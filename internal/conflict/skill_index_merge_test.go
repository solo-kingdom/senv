package conflict

import (
	"testing"

	"github.com/wii/senv/internal/storage"
)

func TestMergeSkillIndex3Way(t *testing.T) {
	base := &storage.SkillIndex{
		Groups: map[string]storage.SkillGroup{
			"base":      {Name: "base", Description: "base"},
			"on-demand": {Name: "on-demand", Description: "old"},
		},
		Skills: map[string]storage.SkillEntry{
			"butler": {ID: "butler", Group: "base", CurrentVersion: "v5"},
			"hela":   {ID: "hela", Group: "on-demand", CurrentVersion: "v1"},
			"old":    {ID: "old", Group: "on-demand", CurrentVersion: "v1"},
		},
	}

	// 1. 无冲突三方合并：local 删 old、改 butler；remote 改 hela、新增 publish
	local := &storage.SkillIndex{
		Groups: map[string]storage.SkillGroup{
			"base":      {Name: "base", Description: "base"},
			"on-demand": {Name: "on-demand", Description: "old"},
		},
		Skills: map[string]storage.SkillEntry{
			"butler": {ID: "butler", Group: "base", CurrentVersion: "v6"},
			"hela":   {ID: "hela", Group: "on-demand", CurrentVersion: "v1"},
		},
	}
	remote := &storage.SkillIndex{
		Groups: map[string]storage.SkillGroup{
			"base":      {Name: "base", Description: "base"},
			"on-demand": {Name: "on-demand", Description: "new"},
		},
		Skills: map[string]storage.SkillEntry{
			"butler":  {ID: "butler", Group: "base", CurrentVersion: "v5"},
			"hela":    {ID: "hela", Group: "on-demand", CurrentVersion: "v2"},
			"old":     {ID: "old", Group: "on-demand", CurrentVersion: "v1"},
			"publish": {ID: "publish", Group: "on-demand", CurrentVersion: "v1"},
		},
	}

	merged, conflicts, err := MergeSkillIndex3Way(base, local, remote)
	if err != nil {
		t.Fatalf("MergeSkillIndex3Way: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}
	if _, ok := merged.Skills["old"]; ok {
		t.Fatal("local deletion of old must be preserved")
	}
	if merged.Skills["butler"].CurrentVersion != "v6" {
		t.Fatalf("butler version = %q, want v6", merged.Skills["butler"].CurrentVersion)
	}
	if merged.Skills["hela"].CurrentVersion != "v2" {
		t.Fatalf("hela version = %q, want v2", merged.Skills["hela"].CurrentVersion)
	}
	if _, ok := merged.Skills["publish"]; !ok {
		t.Fatal("remote addition of publish must be preserved")
	}
	if merged.Groups["on-demand"].Description != "new" {
		t.Fatalf("on-demand group description = %q, want new", merged.Groups["on-demand"].Description)
	}

	// 2. 同 skill 在两侧均有不同修改 → fail-closed 返回冲突
	remoteAlsoButler := &storage.SkillIndex{
		Groups: remote.Groups,
		Skills: map[string]storage.SkillEntry{
			"butler": {ID: "butler", Group: "base", CurrentVersion: "v6-remote"},
			"hela":   {ID: "hela", Group: "on-demand", CurrentVersion: "v2"},
			"old":    {ID: "old", Group: "on-demand", CurrentVersion: "v1"},
		},
	}
	_, conflicts2, err := MergeSkillIndex3Way(base, local, remoteAlsoButler)
	if err != nil {
		t.Fatalf("MergeSkillIndex3Way conflict case: %v", err)
	}
	if len(conflicts2) != 1 || conflicts2[0] != "skill:butler" {
		t.Fatalf("conflicts = %v, want [skill:butler]", conflicts2)
	}
}
