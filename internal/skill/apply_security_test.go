package skill

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func makeSkillArchiveForSecurityTest(t *testing.T, id string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("---\nname: " + id + "\ndescription: security test skill\n---\n\n# " + id + "\n")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "SKILL.md",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSkillApplyAndUnapplyRejectParentSymlink(t *testing.T) {
	projectDir := t.TempDir()
	outsideSkills := t.TempDir()
	outsideVictim := filepath.Join(outsideSkills, "demo-skill")
	if err := os.MkdirAll(outsideVictim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideVictim, OwnedMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideVictim, "SKILL.md"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(projectDir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideSkills, filepath.Join(projectDir, ".agents", "skills")); err != nil {
		t.Fatal(err)
	}
	// .claude is itself a symlink
	if err := os.Symlink(t.TempDir(), filepath.Join(projectDir, ".claude")); err != nil {
		t.Fatal(err)
	}

	applier := NewApplier(projectDir)
	applier.StateDir = filepath.Join(t.TempDir(), "state")

	archive := makeSkillArchiveForSecurityTest(t, "demo-skill")
	plan, err := applier.Plan("demo-skill", archive)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	for _, it := range plan {
		if it.Action != ActionSkipConflict {
			t.Fatalf("expected ActionSkipConflict for symlink parent root %s, got %s (%s)", it.Root, it.Action, it.Reason)
		}
	}

	res, err := applier.Apply("demo-skill", archive)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	for _, r := range res {
		if r.Action != ActionSkipConflict {
			t.Fatalf("Apply must skip conflict on symlink parent, got %+v", r)
		}
	}

	// UnapplyPlan / Unapply must not touch outsideVictim
	uPlan := applier.UnapplyPlan("demo-skill")
	for _, it := range uPlan {
		if it.Action != ActionSkipConflict {
			t.Fatalf("UnapplyPlan expected ActionSkipConflict, got %+v", it)
		}
	}
	uRes := applier.Unapply("demo-skill")
	for _, r := range uRes {
		if r.Action != ActionSkipConflict {
			t.Fatalf("Unapply must skip conflict, got %+v", r)
		}
	}
	got, err := os.ReadFile(filepath.Join(outsideVictim, "SKILL.md"))
	if err != nil || string(got) != "keep" {
		t.Fatalf("outside skill was modified/deleted via symlink parent: data=%q err=%v", string(got), err)
	}
}

func TestSkillApplyAllowsSiblingSymlinkInSkillsRoot(t *testing.T) {
	base := t.TempDir()
	skillsDir := filepath.Join(base, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Unrelated sibling symlink skill must not block managing other skills.
	if err := os.Symlink(t.TempDir(), filepath.Join(skillsDir, "other-linked-skill")); err != nil {
		t.Fatal(err)
	}

	applier := NewApplier(base)
	applier.Roots = []string{".agents/skills"}
	archive := makeSkillArchiveForSecurityTest(t, "real-skill")
	res, err := applier.Apply("real-skill", archive)
	if err != nil || len(res) != 1 || res[0].Err != nil || res[0].Action != ActionCreate {
		t.Fatalf("expected normal apply with sibling symlink, got res=%+v err=%v", res, err)
	}
	uRes := applier.Unapply("real-skill")
	if len(uRes) != 1 || uRes[0].Err != nil || uRes[0].Action != ActionRemove {
		t.Fatalf("expected normal unapply with sibling symlink, got uRes=%+v", uRes)
	}
}

func TestSkillUnapplyRejectsInvalidID(t *testing.T) {
	applier := NewApplier(t.TempDir())
	for _, bad := range []string{"", ".", "..", "../evil", "a/b"} {
		for _, it := range applier.UnapplyPlan(bad) {
			if it.Action != ActionSkipConflict {
				t.Fatalf("id %q expected ActionSkipConflict, got %s", bad, it.Action)
			}
		}
		for _, r := range applier.Unapply(bad) {
			if r.Action != ActionSkipConflict {
				t.Fatalf("Unapply(%q) expected ActionSkipConflict, got %s", bad, r.Action)
			}
		}
	}
}
