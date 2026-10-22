package cmd

import (
	"fmt"

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

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillAddCmd, skillListCmd, skillShowCmd, skillRemoveCmd)

	skillAddCmd.Flags().StringVar(&skillSource, "source", "", "skill source directory (SKILL.md at its root)")
	skillAddCmd.Flags().StringVar(&skillGroup, "group", "", "group name (default: default; with --force keeps the existing group)")
	skillAddCmd.Flags().StringVar(&skillDescription, "description", "", "human-readable description (visible in the plaintext index)")
	skillAddCmd.Flags().BoolVar(&skillForce, "force", false, "replace an existing skill with the same id")
	_ = skillAddCmd.MarkFlagRequired("source")

	skillListCmd.Flags().StringVar(&skillListGroup, "group", "", "filter by group")
	addRefreshFlag(skillListCmd)
	addRefreshFlag(skillShowCmd)
}
