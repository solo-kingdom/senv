package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/skill"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage Agent Skills stored in the vault",
	Long: `Manage Agent Skills (a directory containing SKILL.md) as encrypted vault entries.
Each skill is stored as an encrypted tar archive and synchronized by 'senv sync'.
Skill ids are unique across groups. Plaintext archives are limited to 1 MiB.`,
}

var (
	skillSource      string
	skillGroup       string
	skillDescription string
	skillForce       bool
	skillListGroup   string

	skillApplyGroup   string
	skillApplyAll     bool
	skillApplyDryRun  bool
	skillApplyYes     bool
	skillApplyScope   string
	skillApplyTargets []string
)

func getSkillManager() (*skill.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return skill.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return skill.NewManager(auth.storage, auth.password), nil
}

var skillAddCmd = &cobra.Command{
	Use:   "add <id> --source <dir>",
	Short: "Add a skill directory to the vault",
	Long: `Pack a skill directory into an encrypted vault entry.
The directory content becomes the skill root, so SKILL.md must sit directly
inside --source and its frontmatter 'name' must equal <id>. Symbolic links and
special files are rejected. Use --force to replace an existing skill.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		entry, err := mgr.Add(skill.AddOptions{
			ID:          id,
			SourceDir:   skillSource,
			Group:       skillGroup,
			Description: skillDescription,
			Force:       skillForce,
		})
		if err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id, false, "add 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id, true, "add")
		fmt.Printf("✓ Added skill %s [%s]\n", entry.ID, entry.NormalizedGroup())
		return nil
	},
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		entries, err := mgr.List(skillListGroup)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("No skills found")
			return nil
		}
		fmt.Println("Skills:")
		for _, e := range entries {
			fmt.Printf("  %s [%s]\n", e.ID, e.NormalizedGroup())
			if e.Description != "" {
				fmt.Printf("    Description: %s\n", e.Description)
			}
			fmt.Printf("    Updated: %s\n", e.UpdatedAt)
		}
		return nil
	},
}

var skillShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show skill metadata and its file list",
	Long:  `Show a skill's metadata and the files in its archive. File contents are not printed.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		detail, err := mgr.Show(args[0])
		if err != nil {
			return err
		}
		e := detail.Entry
		fmt.Printf("ID: %s\n", e.ID)
		fmt.Printf("Group: %s\n", e.NormalizedGroup())
		fmt.Printf("Description: %s\n", e.Description)
		fmt.Printf("Created: %s\n", e.CreatedAt)
		fmt.Printf("Updated: %s\n", e.UpdatedAt)
		fmt.Printf("Archive: %d bytes\n", detail.ArchiveSize)
		fmt.Println("Files:")
		for _, f := range detail.Entries {
			if f.Dir {
				fmt.Printf("  %s/\n", f.Path)
				continue
			}
			exec := ""
			if f.Mode&0o111 != 0 {
				exec = " (executable)"
			}
			fmt.Printf("  %s  %d bytes%s\n", f.Path, f.Size, exec)
		}
		return nil
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:     "remove <id>",
	Aliases: []string{"rm"},
	Short:   "Remove a skill from the vault",
	Long:    `Remove a skill from the vault. Directories already applied to this machine are left untouched.`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		if err := mgr.Remove(id); err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id, false, "remove 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill:"+id, true, "remove")
		fmt.Printf("✓ Removed skill %s\n", id)
		return nil
	},
}

var skillApplyCmd = &cobra.Command{
	Use:   "apply [id ...] | --group <group> | --all",
	Short: "Write skills from the vault into local agent skills directories",
	Long: `Expand skills from the vault into agent skills directories.

Skill selection (exactly one):
  apply <id> [<id> ...]   one or more skill ids (multi-select)
  --group <group>         every skill in that group
  --all                   every skill in the vault (select-all)

Install location:
  --scope global (default)  $HOME/.agents/skills and $HOME/.claude/skills
  --scope project           ./.agents/skills and ./.claude/skills under the
                            current working directory

Target roots (optional; default all for the scope):
  --target agents|claude|all   repeatable or comma-separated
                               agents → .agents/skills
                               claude → .claude/skills
                               all    → both (select-all targets)

Per target directory the plan is one of:
  create           target does not exist
  skip             a senv-managed directory already has identical content
  skip-conflict    target is a symbolic link, or an existing directory without
                   a .senv-owned marker; left untouched, with a warning
  backup+overwrite a senv-managed directory (has .senv-owned) whose content differs;
                   the old tree moves to $HOME/.local/state/senv/skill-apply/backups/
                   (outside the skills directories) before the new one is swapped in

Use --dry-run to print the plan only. Without --yes a confirmation is required.
Skills are never uninstalled by this command; dotf is not involved.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSkillApply,
}

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillAddCmd, skillListCmd, skillShowCmd, skillRemoveCmd, skillApplyCmd)

	skillApplyCmd.Flags().StringVar(&skillApplyGroup, "group", "", "apply every skill in this group")
	skillApplyCmd.Flags().BoolVar(&skillApplyAll, "all", false, "apply every skill in the vault")
	skillApplyCmd.Flags().StringVar(&skillApplyScope, "scope", "global", "install location: global ($HOME) or project (current directory)")
	skillApplyCmd.Flags().StringArrayVar(&skillApplyTargets, "target", nil, "skills root to write: agents, claude, or all (repeatable / comma-separated; default all)")
	skillApplyCmd.Flags().BoolVar(&skillApplyDryRun, "dry-run", false, "print the plan without writing anything")
	skillApplyCmd.Flags().BoolVarP(&skillApplyYes, "yes", "y", false, "do not ask for confirmation")
	addRefreshFlag(skillApplyCmd)

	skillAddCmd.Flags().StringVar(&skillSource, "source", "", "skill source directory (SKILL.md at its root)")
	skillAddCmd.Flags().StringVar(&skillGroup, "group", "", "group name (default: default; with --force keeps the existing group)")
	skillAddCmd.Flags().StringVar(&skillDescription, "description", "", "human-readable description (visible in the plaintext index)")
	skillAddCmd.Flags().BoolVar(&skillForce, "force", false, "replace an existing skill with the same id")
	_ = skillAddCmd.MarkFlagRequired("source")

	skillListCmd.Flags().StringVar(&skillListGroup, "group", "", "filter by group")
	addRefreshFlag(skillListCmd)
	addRefreshFlag(skillShowCmd)
}

func runSkillApply(cmd *cobra.Command, args []string) error {
	selectors := 0
	if len(args) > 0 {
		selectors++
	}
	if skillApplyGroup != "" {
		selectors++
	}
	if skillApplyAll {
		selectors++
	}
	if selectors != 1 {
		return fmt.Errorf("specify exactly one of: skill id(s), --group <group>, --all")
	}
	scope := strings.ToLower(strings.TrimSpace(skillApplyScope))
	if scope == "" {
		scope = "global"
	}
	if scope != "global" && scope != "project" {
		return fmt.Errorf("--scope must be global or project (got %q)", skillApplyScope)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	base := home
	if scope == "project" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve project directory: %w", err)
		}
		base = cwd
	}
	roots, err := skill.ResolveRoots(skillApplyTargets)
	if err != nil {
		return err
	}

	autoPull(cmd, refreshRequested(cmd))
	mgr, err := getSkillManager()
	if err != nil {
		return err
	}
	var ids []string
	switch {
	case len(args) > 0:
		seen := map[string]bool{}
		for _, raw := range args {
			entry, err := mgr.Get(raw)
			if err != nil {
				return err
			}
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			ids = append(ids, entry.ID)
		}
	default:
		entries, err := mgr.List(skillApplyGroup)
		if err != nil {
			return err
		}
		for _, e := range entries {
			ids = append(ids, e.ID)
		}
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if len(ids) == 0 {
		fmt.Fprintln(out, "No skills to apply")
		return nil
	}

	applier := skill.NewApplier(base)
	applier.Roots = roots
	if scope == "project" {
		// Keep backups out of the project tree so agents do not load them as skills.
		applier.StateDir = filepath.Join(home, ".local", "state", "senv", "skill-apply")
	}
	archives := map[string][]byte{}
	failures := 0
	changes := 0
	fmt.Fprintln(out, "Plan:")
	for _, id := range ids {
		_, archive, err := mgr.Archive(id)
		if err == nil {
			var items []skill.PlanItem
			items, err = applier.Plan(id, archive)
			if err == nil {
				archives[id] = archive
				for _, it := range items {
					fmt.Fprintf(out, "  [%s] %s -> %s\n", it.Action, it.ID, it.Target)
					switch {
					case it.Action == skill.ActionSkipConflict:
						fmt.Fprintf(errOut, "warning: skipped %s: %s\n", it.Target, it.Reason)
					case it.Action.Changes():
						changes++
					}
				}
				continue
			}
		}
		failures++
		fmt.Fprintf(errOut, "error: skill %s: %v\n", id, err)
	}

	finish := func() error {
		if failures > 0 {
			return fmt.Errorf("%d skill(s) failed", failures)
		}
		return nil
	}
	if skillApplyDryRun {
		fmt.Fprintln(out, "Dry run: nothing was written")
		return finish()
	}
	if changes == 0 {
		fmt.Fprintln(out, "Nothing to apply")
		return finish()
	}
	if !skillApplyYes {
		if !stdinIsTerminal() {
			return fmt.Errorf("refusing to apply %d change(s) without confirmation on a non-interactive stdin; pass --yes", changes)
		}
		if !confirmPrompt(fmt.Sprintf("Apply %d change(s)? [y/N] ", changes)) {
			fmt.Fprintln(out, "Aborted")
			return nil
		}
	}

	for _, id := range ids {
		archive, ok := archives[id]
		if !ok {
			continue
		}
		results, err := applier.Apply(id, archive)
		if err != nil {
			failures++
			auditOp(session.AuditOpSkill, "skill:"+id, false, "apply 失败")
			fmt.Fprintf(errOut, "error: skill %s: %v\n", id, err)
			continue
		}
		failed := false
		for _, r := range results {
			switch {
			case r.Err != nil:
				failed = true
				fmt.Fprintf(errOut, "error: %s: %v\n", r.Target, r.Err)
			case r.Action.Changes():
				fmt.Fprintf(out, "✓ %s %s -> %s\n", r.Action, r.ID, r.Target)
				if r.Backup != "" {
					fmt.Fprintf(out, "  old tree backed up at %s\n", r.Backup)
				}
			}
		}
		if failed {
			failures++
			auditOp(session.AuditOpSkill, "skill:"+id, false, "apply 失败")
		} else {
			auditOp(session.AuditOpSkill, "skill:"+id, true, "apply")
		}
	}
	return finish()
}
