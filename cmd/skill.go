package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage Agent Skills stored in the vault",
	Long: `Manage Agent Skills (a directory containing SKILL.md) as encrypted vault entries.
Each skill is stored as an encrypted tar archive and synchronized by 'senv sync'.
Skill ids are unique across groups. Plaintext archives are limited to 1 MiB.

Skills support versioned snapshots ('add --version/--note', 'version list|use|remove',
and 'show/apply <id>@<version>'). Skills and groups can also declare install metadata
(default_install, install_scope, install_path, targets). Use 'senv skill apply --default'
for routine machine setup.`,
}

var (
	skillSource           string
	skillGroup            string
	skillDescription      string
	skillForce            bool
	skillAddDefault       bool
	skillAddNoDefault     bool
	skillAddScope         string
	skillAddInstallPath   string
	skillAddTargets       []string
	skillAddExtra         string
	skillAddExtraFile     string
	skillAddVersion       string
	skillAddNote          string

	skillEditGroup        string
	skillEditDescription  string
	skillEditDefault      string
	skillEditScope        string
	skillEditInstallPath  string
	skillEditTargets      []string
	skillEditExtra        string
	skillEditExtraFile    string

	skillListGroup        string
	skillListDefaultOnly  bool
	skillListScope        string
	skillListJSON         bool
	skillVersionListJSON  bool

	skillGrpDescription   string
	skillGrpDefault       bool
	skillGrpEditDefault   string
	skillGrpScope         string
	skillGrpInstallPath   string
	skillGrpTargets       []string

	skillApplyGroup       string
	skillApplyDefault     bool
	skillApplyAll         bool
	skillApplyDryRun      bool
	skillApplyYes         bool
	skillApplyScope       string
	skillApplyDir         string
	skillApplyForceScope  bool
	skillApplyTargets     []string
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

func parseTriStateBool(raw string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inherit", "clear", "none":
		return nil, nil
	case "true", "yes", "1", "on":
		v := true
		return &v, nil
	case "false", "no", "0", "off":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf("invalid boolean value %q (want true, false, or inherit)", raw)
	}
}

func isInheritToken(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "inherit", "clear", "none":
		return true
	default:
		return false
	}
}

func readSkillExtraInput(inline, filePath string, inlineSet, fileSet bool) (string, bool, error) {
	if inlineSet && fileSet {
		return "", false, fmt.Errorf("--extra and --extra-file cannot be used together")
	}
	if fileSet {
		var data []byte
		var err error
		if filePath == "-" {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, storage.MaxSkillExtraBytes+1))
		} else {
			p := filePath
			if strings.HasPrefix(p, "~/") {
				if home, herr := os.UserHomeDir(); herr == nil {
					p = filepath.Join(home, p[2:])
				}
			}
			data, err = os.ReadFile(p)
		}
		if err != nil {
			return "", false, fmt.Errorf("read --extra-file: %w", err)
		}
		return string(data), true, nil
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

var skillAddCmd = &cobra.Command{
	Use:   "add <id> --source <dir>",
	Short: "Add a skill directory to the vault",
	Long: `Pack a skill directory into an encrypted vault entry.
The directory content becomes the skill root, so SKILL.md must sit directly
inside --source and its frontmatter 'name' must equal <id>. Symbolic links and
special files are rejected.

Use --force to replace an existing skill's archive; omitted metadata flags
(group, description, install policy) are preserved from the existing entry.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		if skillAddDefault && skillAddNoDefault {
			return fmt.Errorf("--default-install and --no-default-install cannot be used together")
		}
		var defaultPtr *bool
		if skillAddDefault {
			v := true
			defaultPtr = &v
		} else if skillAddNoDefault {
			v := false
			defaultPtr = &v
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		descSet := cmd != nil && cmd.Flags().Changed("description")
		extraInlineSet := (cmd != nil && cmd.Flags().Changed("extra")) || skillAddExtra != ""
		extraFileSet := (cmd != nil && cmd.Flags().Changed("extra-file")) || skillAddExtraFile != ""
		extraVal, extraSet, err := readSkillExtraInput(skillAddExtra, skillAddExtraFile, extraInlineSet, extraFileSet)
		if err != nil {
			return err
		}
		entry, err := mgr.Add(skill.AddOptions{
			ID:             id,
			SourceDir:      skillSource,
			Group:          skillGroup,
			Description:    skillDescription,
			DescriptionSet: descSet,
			DefaultInstall: defaultPtr,
			InstallScope:   skillAddScope,
			InstallPath:    skillAddInstallPath,
			Targets:        skillAddTargets,
			ExtraInfo:      extraVal,
			ExtraInfoSet:   extraSet,
			Version:        skillAddVersion,
			Note:           skillAddNote,
			Force:          skillForce,
		})
		if err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id, false, "add 失败")
			return err
		}
		target := "skill:" + entry.NormalizedGroup() + ":" + id
		verSuffix := ""
		if entry.CurrentVersion != "" {
			target += "@" + entry.CurrentVersion
			verSuffix = "@" + entry.CurrentVersion
		}
		auditOp(session.AuditOpSkill, target, true, "add")
		fmt.Printf("✓ Added skill %s%s [%s]\n", entry.ID, verSuffix, entry.NormalizedGroup())
		return nil
	},
}

var skillEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit skill metadata without touching its archive",
	Long: `Update a skill's group, description, or install policy (default-install,
scope, install-path, target) without re-packing its tar archive.
Changing --group moves the encrypted archive atomically.
Pass 'inherit' (or empty string) to reset a policy field back to group inheritance.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		opts := skill.EditOptions{ID: id}
		changed := 0
		if cmd.Flags().Changed("group") {
			g := skillEditGroup
			opts.Group = &g
			changed++
		}
		if cmd.Flags().Changed("description") {
			d := skillEditDescription
			opts.Description = &d
			changed++
		}
		if cmd.Flags().Changed("default-install") {
			v, err := parseTriStateBool(skillEditDefault)
			if err != nil {
				return err
			}
			opts.DefaultInstallSet = true
			opts.DefaultInstall = v
			changed++
		}
		if cmd.Flags().Changed("scope") {
			opts.InstallScopeSet = true
			if !isInheritToken(skillEditScope) {
				opts.InstallScope = skillEditScope
			}
			changed++
		}
		if cmd.Flags().Changed("install-path") {
			opts.InstallPathSet = true
			if !isInheritToken(skillEditInstallPath) {
				opts.InstallPath = skillEditInstallPath
			}
			changed++
		}
		if cmd.Flags().Changed("target") {
			opts.TargetsSet = true
			if !(len(skillEditTargets) == 1 && isInheritToken(skillEditTargets[0])) {
				opts.Targets = skillEditTargets
			}
			changed++
		}
		if cmd.Flags().Changed("extra") || cmd.Flags().Changed("extra-file") {
			extraVal, _, err := readSkillExtraInput(skillEditExtra, skillEditExtraFile, cmd.Flags().Changed("extra"), cmd.Flags().Changed("extra-file"))
			if err != nil {
				return err
			}
			opts.ExtraInfoSet = true
			if !cmd.Flags().Changed("extra-file") && isInheritToken(extraVal) {
				opts.ExtraInfo = ""
			} else {
				opts.ExtraInfo = extraVal
			}
			changed++
		}
		if changed == 0 {
			return fmt.Errorf("specify at least one field to edit (--group, --description, --default-install, --scope, --install-path, --target, --extra, --extra-file)")
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		entry, err := mgr.Edit(opts)
		if err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id, false, "edit 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id, true, "edit")
		fmt.Printf("✓ Updated skill %s [%s]\n", entry.ID, entry.NormalizedGroup())
		return nil
	},
}

func formatInstallLocation(policy storage.SkillEffectivePolicy) string {
	loc := policy.InstallScope
	if policy.InstallScope == storage.SkillScopeProject {
		if policy.InstallPath != "" {
			loc = "project:" + policy.InstallPath
		} else {
			loc = "project (cwd)"
		}
	}
	if len(policy.Targets) > 0 && len(policy.Targets) < 2 {
		loc += " [" + strings.Join(policy.Targets, ",") + "]"
	}
	return loc
}

func formatGroupPolicySummary(grp storage.SkillGroup) string {
	var parts []string
	if grp.DefaultInstall != nil {
		if *grp.DefaultInstall {
			parts = append(parts, "default=yes")
		} else {
			parts = append(parts, "default=no")
		}
	}
	if grp.InstallScope != "" {
		s := grp.InstallScope
		if grp.InstallScope == storage.SkillScopeProject && grp.InstallPath != "" {
			s += ":" + grp.InstallPath
		}
		parts = append(parts, "install="+s)
	}
	if len(grp.Targets) > 0 {
		parts = append(parts, "targets="+strings.Join(grp.Targets, ","))
	}
	return strings.Join(parts, ", ")
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills grouped by group with install policy",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		items, groups, err := mgr.ListWithPolicy(skill.ListFilter{
			Group:       skillListGroup,
			DefaultOnly: skillListDefaultOnly,
			Scope:       skillListScope,
		})
		if err != nil {
			return err
		}
		if skillListJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		}
		if len(items) == 0 {
			fmt.Println("No skills found")
			return nil
		}

		byGroup := map[string][]skill.ListedSkill{}
		var groupNames []string
		for _, it := range items {
			g := it.Entry.NormalizedGroup()
			if _, ok := byGroup[g]; !ok {
				groupNames = append(groupNames, g)
			}
			byGroup[g] = append(byGroup[g], it)
		}
		sort.Slice(groupNames, func(i, j int) bool {
			if groupNames[i] == storage.SkillDefaultGroup {
				return true
			}
			if groupNames[j] == storage.SkillDefaultGroup {
				return false
			}
			return groupNames[i] < groupNames[j]
		})

		fmt.Println("Skills:")
		for _, g := range groupNames {
			grpMeta := groups[g]
			header := fmt.Sprintf("  [%s] (%d)", g, len(byGroup[g]))
			if summary := formatGroupPolicySummary(grpMeta); summary != "" {
				header += "  " + summary
			}
			if grpMeta.Description != "" {
				header += "  — " + grpMeta.Description
			}
			fmt.Println(header)
			for _, it := range byGroup[g] {
				defTag := "manual"
				if it.Policy.DefaultInstall {
					defTag = "default"
				}
				label := it.Entry.ID
				if it.Entry.CurrentVersion != "" {
					label += "@" + it.Entry.CurrentVersion
				}
				fmt.Printf("    %s [%s] (%s, %s)\n", label, g, defTag, formatInstallLocation(it.Policy))
				if it.Entry.Description != "" {
					fmt.Printf("      Description: %s\n", it.Entry.Description)
				}
				fmt.Printf("      Updated: %s\n", it.Entry.UpdatedAt)
			}
		}
		return nil
	},
}

func inheritSuffix(inherited bool) string {
	if inherited {
		return " (inherited)"
	}
	return ""
}

var skillShowCmd = &cobra.Command{
	Use:   "show <id>[@<version>]",
	Short: "Show skill metadata, effective install policy, and file list",
	Long:  `Show a skill's metadata, effective install policy, version history, and the files in its archive. File contents are not printed.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, reqVer, err := skill.ParseSkillRef(args[0])
		if err != nil {
			return err
		}
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		detail, err := mgr.ShowVersion(id, reqVer)
		if err != nil {
			return err
		}
		e := detail.Entry
		p := detail.Policy
		fmt.Printf("ID: %s\n", e.ID)
		fmt.Printf("Group: %s\n", e.NormalizedGroup())
		if detail.DisplayVersion != "" {
			if detail.DisplayVersion == e.CurrentVersion {
				fmt.Printf("Version: %s (active)\n", detail.DisplayVersion)
			} else {
				fmt.Printf("Version: %s (snapshot; active: %s)\n", detail.DisplayVersion, e.CurrentVersion)
			}
		}
		fmt.Printf("Description: %s\n", e.Description)
		fmt.Printf("Default Install: %t%s\n", p.DefaultInstall, inheritSuffix(p.DefaultInstallInherited))
		fmt.Printf("Install Scope: %s%s\n", p.InstallScope, inheritSuffix(p.InstallScopeInherited))
		if p.InstallScope == storage.SkillScopeProject {
			pathDisp := p.InstallPath
			if pathDisp == "" {
				pathDisp = "(cwd / --dir required)"
			}
			fmt.Printf("Install Path: %s%s\n", pathDisp, inheritSuffix(p.InstallPathInherited))
		}
		fmt.Printf("Targets: %s%s\n", strings.Join(p.Targets, ", "), inheritSuffix(p.TargetsInherited))
		if e.ExtraInfo != "" {
			fmt.Println("Extra Info:")
			for _, line := range strings.Split(e.ExtraInfo, "\n") {
				fmt.Printf("  %s\n", line)
			}
		}
		fmt.Printf("Created: %s\n", e.CreatedAt)
		fmt.Printf("Updated: %s\n", e.UpdatedAt)
		if len(e.Versions) > 0 {
			fmt.Printf("Versions (%d):\n", len(e.Versions))
			for _, v := range e.Versions {
				mark := " "
				if v.Version == e.CurrentVersion {
					mark = "*"
				}
				line := fmt.Sprintf("  %s %s  %d bytes  %s", mark, v.Version, v.Size, v.CreatedAt.Format("2006-01-02 15:04:05"))
				if v.Note != "" {
					line += "  — " + v.Note
				}
				fmt.Println(line)
			}
		}
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

var skillVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Manage skill version snapshots (list, use, remove)",
}

func parseSkillVersionArgs(args []string) (id, version string, err error) {
	if len(args) == 1 {
		id, ver, err := skill.ParseSkillRef(args[0])
		if err != nil {
			return "", "", err
		}
		if ver == "" {
			return "", "", fmt.Errorf("version is required (use <id>@<version> or <id> <version>)")
		}
		return id, ver, nil
	}
	if err := storage.ValidateSkillID(args[0]); err != nil {
		return "", "", err
	}
	ver, err := storage.ValidateSkillVersion(args[1])
	if err != nil {
		return "", "", err
	}
	return args[0], ver, nil
}

var skillVersionListCmd = &cobra.Command{
	Use:     "list <id>",
	Aliases: []string{"ls"},
	Short:   "List all saved versions of a skill",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		versions, current, err := mgr.ListVersions(id)
		if err != nil {
			return err
		}
		if skillVersionListJSON {
			payload := struct {
				ID             string                 `json:"id"`
				CurrentVersion string                 `json:"current_version,omitempty"`
				Versions       []storage.SkillVersion `json:"versions"`
			}{
				ID:             id,
				CurrentVersion: current,
				Versions:       versions,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(payload)
		}
		if len(versions) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Skill %s has no version snapshots (legacy unversioned entry)\n", id)
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Versions for %s (active: %s):\n", id, current)
		for _, v := range versions {
			mark := " "
			if v.Version == current {
				mark = "*"
			}
			hashShort := v.SHA256
			if len(hashShort) > 12 {
				hashShort = hashShort[:12]
			}
			line := fmt.Sprintf("  %s %-12s  %7d B  %-12s  %s", mark, v.Version, v.Size, hashShort, v.CreatedAt.Format("2006-01-02 15:04:05"))
			if v.Note != "" {
				line += "  — " + v.Note
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return nil
	},
}

var skillVersionUseCmd = &cobra.Command{
	Use:   "use <id> <version> | <id>@<version>",
	Short: "Switch the active version of a skill",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, ver, err := parseSkillVersionArgs(args)
		if err != nil {
			return err
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		entry, err := mgr.UseVersion(id, ver)
		if err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id+"@"+ver, false, "version use 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id+"@"+ver, true, "version use")
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Switched skill %s active version to %s [%s]\n", entry.ID, entry.CurrentVersion, entry.NormalizedGroup())
		return nil
	},
}

var skillVersionRemoveCmd = &cobra.Command{
	Use:     "remove <id> <version> | <id>@<version>",
	Aliases: []string{"rm"},
	Short:   "Remove a non-active version snapshot of a skill",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, ver, err := parseSkillVersionArgs(args)
		if err != nil {
			return err
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		entry, err := mgr.RemoveVersion(id, ver)
		if err != nil {
			auditOp(session.AuditOpSkill, "skill:"+id+"@"+ver, false, "version remove 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill:"+entry.NormalizedGroup()+":"+id+"@"+ver, true, "version remove")
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed version %s of skill %s (active: %s)\n", ver, entry.ID, entry.CurrentVersion)
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

var skillGroupCmd = &cobra.Command{
	Use:   "group",
	Short: "Manage skill groups and group-level default install policies",
}

var skillGroupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skill groups",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		groups, err := mgr.ListGroups()
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			fmt.Println("No skill groups found")
			return nil
		}
		fmt.Println("Skill Groups:")
		for _, g := range groups {
			line := fmt.Sprintf("  %s (%d skill(s))", g.Group.Name, g.Count)
			if summary := formatGroupPolicySummary(g.Group); summary != "" {
				line += "  [" + summary + "]"
			}
			fmt.Println(line)
			if g.Group.Description != "" {
				fmt.Printf("    Description: %s\n", g.Group.Description)
			}
		}
		return nil
	},
}

var skillGroupAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register a skill group with description and default install policy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if strings.TrimSpace(skillGrpDescription) == "" {
			return fmt.Errorf("--description is required and must be non-empty")
		}
		var defaultPtr *bool
		if cmd.Flags().Changed("default-install") {
			v := skillGrpDefault
			defaultPtr = &v
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		grp, err := mgr.AddGroup(skill.GroupOptions{
			Name:           name,
			Description:    skillGrpDescription,
			DefaultInstall: defaultPtr,
			InstallScope:   skillGrpScope,
			InstallPath:    skillGrpInstallPath,
			Targets:        skillGrpTargets,
		})
		if err != nil {
			auditOp(session.AuditOpSkill, "skill-group:"+name, false, "group add 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill-group:"+grp.Name, true, "group add")
		fmt.Printf("✓ Added skill group %s\n", grp.Name)
		return nil
	},
}

var skillGroupEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a skill group's description or default install policy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		opts := skill.GroupEditOptions{Name: name}
		changed := 0
		if cmd.Flags().Changed("description") {
			d := skillGrpDescription
			opts.Description = &d
			changed++
		}
		if cmd.Flags().Changed("default-install") {
			v, err := parseTriStateBool(skillGrpEditDefault)
			if err != nil {
				return err
			}
			opts.DefaultInstallSet = true
			opts.DefaultInstall = v
			changed++
		}
		if cmd.Flags().Changed("scope") {
			opts.InstallScopeSet = true
			if !isInheritToken(skillGrpScope) {
				opts.InstallScope = skillGrpScope
			}
			changed++
		}
		if cmd.Flags().Changed("install-path") {
			opts.InstallPathSet = true
			if !isInheritToken(skillGrpInstallPath) {
				opts.InstallPath = skillGrpInstallPath
			}
			changed++
		}
		if cmd.Flags().Changed("target") {
			opts.TargetsSet = true
			if !(len(skillGrpTargets) == 1 && isInheritToken(skillGrpTargets[0])) {
				opts.Targets = skillGrpTargets
			}
			changed++
		}
		if changed == 0 {
			return fmt.Errorf("specify at least one field to edit (--description, --default-install, --scope, --install-path, --target)")
		}
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		grp, err := mgr.EditGroup(opts)
		if err != nil {
			auditOp(session.AuditOpSkill, "skill-group:"+name, false, "group edit 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill-group:"+grp.Name, true, "group edit")
		fmt.Printf("✓ Updated skill group %s\n", grp.Name)
		return nil
	},
}

var skillGroupDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete an empty skill group",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		mgr, err := getSkillManager()
		if err != nil {
			return err
		}
		if err := mgr.DeleteGroup(name); err != nil {
			auditOp(session.AuditOpSkill, "skill-group:"+name, false, "group delete 失败")
			return err
		}
		auditOp(session.AuditOpSkill, "skill-group:"+name, true, "group delete")
		fmt.Printf("✓ Deleted skill group %s\n", name)
		return nil
	},
}

var skillApplyCmd = &cobra.Command{
	Use:   "apply [id[@version] ...] | --group <group> | --default | --all",
	Short: "Write skills from the vault into local agent skills directories",
	Long: `Expand skills from the vault into agent skills directories.

Skill selection (exactly one):
  apply <id>[@<version>] ...   one or more skill ids (optionally pinned to a version snapshot)
  --group <group>         every skill in that group
  --default               every skill whose effective default_install is true
  --all                   every skill in the vault (select-all)

Install location:
  (omitted)                 auto-route each skill by its effective install_scope /
                            install_path (global -> $HOME; project with path ->
                            that directory if it exists, otherwise skip-missing-path;
                            project without path -> skip-project-only)
  --scope global            force $HOME; project-scoped skills are skipped with
                            skip-scope-mismatch unless --force-scope is passed
  --scope project           install into current working directory (or --dir <path>)

Target roots (optional; default is each skill's effective targets):
  --target agents|claude|all   repeatable or comma-separated
                               agents → .agents/skills
                               claude → .claude/skills
                               all    → both (select-all targets)

Per target directory the plan is one of:
  create               target does not exist
  skip                 a senv-managed directory already has identical content
  skip-conflict        target is a symbolic link, or an existing directory without
                       a .senv-owned marker; left untouched, with a warning
  skip-missing-path    project install_path does not exist on this machine
  skip-project-only    project skill has no install_path; run with --scope project
  skip-scope-mismatch  project skill skipped under --scope global (use --force-scope)
  backup+overwrite     a senv-managed directory (has .senv-owned) whose content differs;
                       the old tree moves to $HOME/.local/state/senv/skill-apply/backups/
                       (outside the skills directories) before the new one is swapped in

Use --dry-run to print the plan only. Without --yes a confirmation is required.
This command never uninstalls skills; use 'skill unapply' to remove managed trees.
dotf is not involved.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSkillApply,
}

var skillUnapplyCmd = &cobra.Command{
	Use:   "unapply [id ...] | --group <group> | --default | --all",
	Short: "Remove senv-managed skill directories from local agent skills roots",
	Long: `Remove skill directories that were written by 'skill apply' (directories that
contain a .senv-owned marker). Selection, --scope, --dir, and --target match 'skill apply'.

Per target directory the plan is one of:
  remove               senv-managed directory will be deleted
  skip                 target is not present
  skip-conflict        symbolic link, or directory without .senv-owned; left untouched
  skip-missing-path    project install_path does not exist on this machine
  skip-project-only    project skill has no install_path; run with --scope project
  skip-scope-mismatch  project skill skipped under --scope global (use --force-scope)

Vault entries are not deleted. Use --dry-run to print the plan only.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSkillUnapply,
}

func init() {
	rootCmd.AddCommand(skillCmd)
	skillCmd.AddCommand(skillAddCmd, skillEditCmd, skillListCmd, skillShowCmd, skillRemoveCmd, skillGroupCmd, skillVersionCmd, skillApplyCmd, skillUnapplyCmd)
	skillGroupCmd.AddCommand(skillGroupListCmd, skillGroupAddCmd, skillGroupEditCmd, skillGroupDeleteCmd)
	skillVersionCmd.AddCommand(skillVersionListCmd, skillVersionUseCmd, skillVersionRemoveCmd)

	for _, c := range []*cobra.Command{skillApplyCmd, skillUnapplyCmd} {
		c.Flags().StringVar(&skillApplyGroup, "group", "", "every skill in this group")
		c.Flags().BoolVar(&skillApplyDefault, "default", false, "every skill with effective default_install=true")
		c.Flags().BoolVar(&skillApplyAll, "all", false, "every skill in the vault")
		c.Flags().StringVar(&skillApplyScope, "scope", "", "override install scope: global ($HOME) or project (CWD/--dir); omitted = route by skill policy")
		c.Flags().StringVar(&skillApplyDir, "dir", "", "project directory for --scope project (supports ~/...)")
		c.Flags().BoolVar(&skillApplyForceScope, "force-scope", false, "allow --scope global to apply project-scoped skills")
		c.Flags().StringArrayVar(&skillApplyTargets, "target", nil, "skills root: agents, claude, or all (repeatable / comma-separated; omitted = skill policy)")
		c.Flags().BoolVar(&skillApplyDryRun, "dry-run", false, "print the plan without writing anything")
		c.Flags().BoolVarP(&skillApplyYes, "yes", "y", false, "do not ask for confirmation")
		addRefreshFlag(c)
	}

	skillAddCmd.Flags().StringVar(&skillSource, "source", "", "skill source directory (SKILL.md at its root)")
	skillAddCmd.Flags().StringVar(&skillGroup, "group", "", "group name (default: default; with --force keeps the existing group)")
	skillAddCmd.Flags().StringVar(&skillDescription, "description", "", "human-readable description")
	skillAddCmd.Flags().BoolVar(&skillAddDefault, "default-install", false, "mark this skill for 'skill apply --default'")
	skillAddCmd.Flags().BoolVar(&skillAddNoDefault, "no-default-install", false, "explicitly exclude this skill from 'skill apply --default'")
	skillAddCmd.Flags().StringVar(&skillAddScope, "scope", "", "install scope: global or project (omitted = inherit group)")
	skillAddCmd.Flags().StringVar(&skillAddInstallPath, "install-path", "", "recommended project directory for --scope project (e.g. ~/code)")
	skillAddCmd.Flags().StringArrayVar(&skillAddTargets, "target", nil, "target roots: agents, claude, or all (omitted = inherit group)")
	skillAddCmd.Flags().StringVar(&skillAddExtra, "extra", "", "extra Markdown context appended to SKILL.md on apply (do not put secrets)")
	skillAddCmd.Flags().StringVar(&skillAddExtraFile, "extra-file", "", "read extra Markdown context from file (or '-' for stdin)")
	skillAddCmd.Flags().StringVar(&skillAddVersion, "version", "", "explicit version label (overrides SKILL.md frontmatter)")
	skillAddCmd.Flags().StringVar(&skillAddNote, "note", "", "short release note for this version")
	skillAddCmd.Flags().BoolVar(&skillForce, "force", false, "replace an existing skill or version snapshot")
	_ = skillAddCmd.MarkFlagRequired("source")

	skillEditCmd.Flags().StringVar(&skillEditGroup, "group", "", "move skill to another group")
	skillEditCmd.Flags().StringVar(&skillEditDescription, "description", "", "update description")
	skillEditCmd.Flags().StringVar(&skillEditDefault, "default-install", "", "true, false, or inherit")
	skillEditCmd.Flags().StringVar(&skillEditScope, "scope", "", "global, project, or inherit")
	skillEditCmd.Flags().StringVar(&skillEditInstallPath, "install-path", "", "project directory (e.g. ~/code) or empty/inherit to clear")
	skillEditCmd.Flags().StringArrayVar(&skillEditTargets, "target", nil, "agents, claude, all, or inherit")
	skillEditCmd.Flags().StringVar(&skillEditExtra, "extra", "", "extra Markdown context appended to SKILL.md on apply (empty/clear to remove)")
	skillEditCmd.Flags().StringVar(&skillEditExtraFile, "extra-file", "", "read extra Markdown context from file (or '-' for stdin)")

	skillListCmd.Flags().StringVar(&skillListGroup, "group", "", "filter by group")
	skillListCmd.Flags().BoolVar(&skillListDefaultOnly, "default", false, "only list skills with effective default_install=true")
	skillListCmd.Flags().StringVar(&skillListScope, "scope", "", "filter by effective install scope (global or project)")
	skillListCmd.Flags().BoolVar(&skillListJSON, "json", false, "output as JSON")
	addRefreshFlag(skillListCmd)
	addRefreshFlag(skillShowCmd)
	addRefreshFlag(skillGroupListCmd)

	skillVersionListCmd.Flags().BoolVar(&skillVersionListJSON, "json", false, "output as JSON")
	addRefreshFlag(skillVersionListCmd)

	skillGroupAddCmd.Flags().StringVar(&skillGrpDescription, "description", "", "group description (required)")
	skillGroupAddCmd.Flags().BoolVar(&skillGrpDefault, "default-install", false, "group default for 'skill apply --default'")
	skillGroupAddCmd.Flags().StringVar(&skillGrpScope, "scope", "", "group default install scope: global or project")
	skillGroupAddCmd.Flags().StringVar(&skillGrpInstallPath, "install-path", "", "group default project directory (requires --scope project)")
	skillGroupAddCmd.Flags().StringArrayVar(&skillGrpTargets, "target", nil, "group default target roots: agents, claude, or all")

	skillGroupEditCmd.Flags().StringVar(&skillGrpDescription, "description", "", "update group description")
	skillGroupEditCmd.Flags().StringVar(&skillGrpEditDefault, "default-install", "", "true, false, or inherit")
	skillGroupEditCmd.Flags().StringVar(&skillGrpScope, "scope", "", "global, project, or inherit")
	skillGroupEditCmd.Flags().StringVar(&skillGrpInstallPath, "install-path", "", "project directory or empty/inherit to clear")
	skillGroupEditCmd.Flags().StringArrayVar(&skillGrpTargets, "target", nil, "agents, claude, all, or inherit")
}

type resolvedSkillTarget struct {
	entry   storage.SkillEntry
	policy  storage.SkillEffectivePolicy
	version string // 非空表示显式指定了版本快照
	applier *skill.Applier // nil 时表示该条目被前置路由跳过
	skip    *skill.PlanItem
}

type skillApplyContext struct {
	mgr   *skill.Manager
	items []resolvedSkillTarget
}

func resolveSkillApplyContext(cmd *cobra.Command, args []string) (*skillApplyContext, error) {
	selectors := 0
	if len(args) > 0 {
		selectors++
	}
	if skillApplyGroup != "" {
		selectors++
	}
	if skillApplyDefault {
		selectors++
	}
	if skillApplyAll {
		selectors++
	}
	if selectors != 1 {
		return nil, fmt.Errorf("specify exactly one of: skill id(s), --group <group>, --default, --all")
	}

	scope := strings.ToLower(strings.TrimSpace(skillApplyScope))
	if scope != "" && scope != storage.SkillScopeGlobal && scope != storage.SkillScopeProject {
		return nil, fmt.Errorf("--scope must be global or project (got %q)", skillApplyScope)
	}
	if skillApplyDir != "" {
		if scope == storage.SkillScopeGlobal {
			return nil, fmt.Errorf("--dir cannot be used with --scope global")
		}
		scope = storage.SkillScopeProject
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	var explicitProjectBase string
	if scope == storage.SkillScopeProject {
		if skillApplyDir != "" {
			expanded, err := skill.ExpandInstallPath(home, skillApplyDir)
			if err != nil {
				// 允许相对路径（相对于 CWD）
				if !filepath.IsAbs(skillApplyDir) && !strings.HasPrefix(skillApplyDir, "~") {
					abs, absErr := filepath.Abs(skillApplyDir)
					if absErr != nil {
						return nil, fmt.Errorf("resolve --dir %q: %w", skillApplyDir, absErr)
					}
					expanded = abs
				} else {
					return nil, err
				}
			}
			fi, statErr := os.Stat(expanded)
			if statErr != nil || !fi.IsDir() {
				return nil, fmt.Errorf("--dir %q does not exist or is not a directory", expanded)
			}
			explicitProjectBase = expanded
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("resolve project directory: %w", err)
			}
			explicitProjectBase = cwd
		}
	}

	var explicitRoots []string
	if len(skillApplyTargets) > 0 {
		roots, err := skill.ResolveRoots(skillApplyTargets)
		if err != nil {
			return nil, err
		}
		explicitRoots = roots
	}

	autoPull(cmd, refreshRequested(cmd))
	mgr, err := getSkillManager()
	if err != nil {
		return nil, err
	}

	type selectedSkill struct {
		item    skill.ListedSkill
		version string
	}
	var selected []selectedSkill
	switch {
	case len(args) > 0:
		seenVer := map[string]string{}
		seen := map[string]bool{}
		for _, raw := range args {
			id, reqVer, err := skill.ParseSkillRef(raw)
			if err != nil {
				return nil, err
			}
			if seen[id] {
				if seenVer[id] != reqVer {
					return nil, fmt.Errorf("conflicting versions requested for skill %q in one apply command", id)
				}
				continue
			}
			entry, policy, err := mgr.GetWithPolicy(id)
			if err != nil {
				return nil, err
			}
			if reqVer != "" {
				if _, ok := entry.FindVersion(reqVer); !ok {
					return nil, fmt.Errorf("version %q not found for skill %q", reqVer, id)
				}
			}
			seen[id] = true
			seenVer[id] = reqVer
			selected = append(selected, selectedSkill{
				item:    skill.ListedSkill{Entry: *entry, Policy: policy},
				version: reqVer,
			})
		}
	case skillApplyDefault:
		listed, _, err := mgr.ListWithPolicy(skill.ListFilter{DefaultOnly: true})
		if err != nil {
			return nil, err
		}
		for _, it := range listed {
			selected = append(selected, selectedSkill{item: it})
		}
	default:
		listed, _, err := mgr.ListWithPolicy(skill.ListFilter{Group: skillApplyGroup})
		if err != nil {
			return nil, err
		}
		for _, it := range listed {
			selected = append(selected, selectedSkill{item: it})
		}
	}

	stateDir := filepath.Join(home, ".local", "state", "senv", "skill-apply")
	resolved := make([]resolvedSkillTarget, 0, len(selected))
	for _, sel := range selected {
		it := sel.item
		target := resolvedSkillTarget{entry: it.Entry, policy: it.Policy, version: sel.version}
		var base string
		switch scope {
		case storage.SkillScopeGlobal:
			if it.Policy.InstallScope == storage.SkillScopeProject && !skillApplyForceScope {
				target.skip = &skill.PlanItem{
					ID:     it.Entry.ID,
					Target: home,
					Action: skill.ActionSkipScopeMismatch,
					Reason: "skill is project-scoped; pass --force-scope to apply globally",
				}
				resolved = append(resolved, target)
				continue
			}
			base = home
		case storage.SkillScopeProject:
			base = explicitProjectBase
		default: // auto-route by effective policy
			if it.Policy.InstallScope == storage.SkillScopeProject {
				if it.Policy.InstallPath == "" {
					target.skip = &skill.PlanItem{
						ID:     it.Entry.ID,
						Target: "(project)",
						Action: skill.ActionSkipProjectOnly,
						Reason: "project-scoped skill has no install_path; run with --scope project [--dir <path>]",
					}
					resolved = append(resolved, target)
					continue
				}
				expanded, err := skill.ExpandInstallPath(home, it.Policy.InstallPath)
				if err != nil {
					target.skip = &skill.PlanItem{
						ID:     it.Entry.ID,
						Target: it.Policy.InstallPath,
						Action: skill.ActionSkipMissingPath,
						Reason: err.Error(),
					}
					resolved = append(resolved, target)
					continue
				}
				fi, statErr := os.Stat(expanded)
				if statErr != nil || !fi.IsDir() {
					target.skip = &skill.PlanItem{
						ID:     it.Entry.ID,
						Target: expanded,
						Action: skill.ActionSkipMissingPath,
						Reason: "install_path does not exist on this machine",
					}
					resolved = append(resolved, target)
					continue
				}
				base = expanded
			} else {
				base = home
			}
		}

		roots := explicitRoots
		if len(roots) == 0 {
			r, err := skill.ResolveRoots(it.Policy.Targets)
			if err != nil {
				return nil, fmt.Errorf("skill %s targets: %w", it.Entry.ID, err)
			}
			roots = r
		}
		applier := skill.NewApplier(base)
		applier.Roots = roots
		if base != home {
			// Keep backups out of the project tree so agents do not load them as skills.
			applier.StateDir = stateDir
		}
		target.applier = applier
		resolved = append(resolved, target)
	}

	return &skillApplyContext{mgr: mgr, items: resolved}, nil
}

func isWarningSkip(a skill.Action) bool {
	return a == skill.ActionSkipConflict ||
		a == skill.ActionSkipMissingPath ||
		a == skill.ActionSkipProjectOnly ||
		a == skill.ActionSkipScopeMismatch
}

func formatApplyVersionSuffix(it skill.PlanItem) string {
	switch {
	case it.TargetVersion != "" && it.InstalledVersion != "" && it.InstalledVersion != it.TargetVersion:
		return fmt.Sprintf(" (%s -> %s)", it.InstalledVersion, it.TargetVersion)
	case it.TargetVersion != "":
		return fmt.Sprintf(" (%s)", it.TargetVersion)
	default:
		return ""
	}
}

func runSkillApply(cmd *cobra.Command, args []string) error {
	ctx, err := resolveSkillApplyContext(cmd, args)
	if err != nil {
		return err
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if len(ctx.items) == 0 {
		fmt.Fprintln(out, "No skills to apply")
		return nil
	}

	type applyArchive struct {
		data    []byte
		version string
	}
	archives := map[string]applyArchive{}
	failures := 0
	changes := 0
	fmt.Fprintln(out, "Plan:")
	for _, tgt := range ctx.items {
		id := tgt.entry.ID
		if tgt.skip != nil {
			fmt.Fprintf(out, "  [%s] %s -> %s\n", tgt.skip.Action, tgt.skip.ID, tgt.skip.Target)
			fmt.Fprintf(errOut, "warning: skipped %s (%s): %s\n", tgt.skip.ID, tgt.skip.Target, tgt.skip.Reason)
			continue
		}
		_, archive, targetVer, err := ctx.mgr.ArchiveVersion(id, tgt.version)
		if err == nil {
			var items []skill.PlanItem
			items, err = tgt.applier.PlanWithOptions(id, archive, skill.ApplyOptions{
				ExtraInfo: tgt.entry.ExtraInfo,
				Version:   targetVer,
			})
			if err == nil {
				archives[id] = applyArchive{data: archive, version: targetVer}
				for _, it := range items {
					fmt.Fprintf(out, "  [%s] %s -> %s%s\n", it.Action, it.ID, it.Target, formatApplyVersionSuffix(it))
					switch {
					case isWarningSkip(it.Action):
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

	for _, tgt := range ctx.items {
		id := tgt.entry.ID
		if tgt.skip != nil || tgt.applier == nil {
			continue
		}
		arc, ok := archives[id]
		if !ok {
			continue
		}
		results, err := tgt.applier.ApplyWithOptions(id, arc.data, skill.ApplyOptions{
			ExtraInfo: tgt.entry.ExtraInfo,
			Version:   arc.version,
		})
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
				fmt.Fprintf(out, "✓ %s %s -> %s%s\n", r.Action, r.ID, r.Target, formatApplyVersionSuffix(r.PlanItem))
				if r.Backup != "" {
					fmt.Fprintf(out, "  old tree backed up at %s\n", r.Backup)
				}
			}
		}
		if failed {
			failures++
			auditOp(session.AuditOpSkill, "skill:"+id, false, "apply 失败")
		} else {
			auditTarget := "skill:" + id
			if arc.version != "" {
				auditTarget += "@" + arc.version
			}
			auditOp(session.AuditOpSkill, auditTarget, true, "apply")
		}
	}
	return finish()
}

func runSkillUnapply(cmd *cobra.Command, args []string) error {
	ctx, err := resolveSkillApplyContext(cmd, args)
	if err != nil {
		return err
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if len(ctx.items) == 0 {
		fmt.Fprintln(out, "No skills to unapply")
		return nil
	}

	failures := 0
	changes := 0
	fmt.Fprintln(out, "Plan:")
	for _, tgt := range ctx.items {
		if tgt.skip != nil {
			fmt.Fprintf(out, "  [%s] %s -> %s\n", tgt.skip.Action, tgt.skip.ID, tgt.skip.Target)
			fmt.Fprintf(errOut, "warning: skipped %s (%s): %s\n", tgt.skip.ID, tgt.skip.Target, tgt.skip.Reason)
			continue
		}
		items := tgt.applier.UnapplyPlan(tgt.entry.ID)
		for _, it := range items {
			fmt.Fprintf(out, "  [%s] %s -> %s\n", it.Action, it.ID, it.Target)
			switch {
			case isWarningSkip(it.Action):
				fmt.Fprintf(errOut, "warning: skipped %s: %s\n", it.Target, it.Reason)
			case it.Action.Changes():
				changes++
			}
		}
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
		fmt.Fprintln(out, "Nothing to unapply")
		return finish()
	}
	if !skillApplyYes {
		if !stdinIsTerminal() {
			return fmt.Errorf("refusing to unapply %d change(s) without confirmation on a non-interactive stdin; pass --yes", changes)
		}
		if !confirmPrompt(fmt.Sprintf("Unapply %d change(s)? [y/N] ", changes)) {
			fmt.Fprintln(out, "Aborted")
			return nil
		}
	}

	for _, tgt := range ctx.items {
		id := tgt.entry.ID
		if tgt.skip != nil || tgt.applier == nil {
			continue
		}
		results := tgt.applier.Unapply(id)
		failed := false
		for _, r := range results {
			switch {
			case r.Err != nil:
				failed = true
				fmt.Fprintf(errOut, "error: %s: %v\n", r.Target, r.Err)
			case r.Action.Changes():
				fmt.Fprintf(out, "✓ %s %s -> %s\n", r.Action, r.ID, r.Target)
			}
		}
		if failed {
			failures++
			auditOp(session.AuditOpSkill, "skill:"+id, false, "unapply 失败")
		} else {
			auditOp(session.AuditOpSkill, "skill:"+id, true, "unapply")
		}
	}
	return finish()
}
