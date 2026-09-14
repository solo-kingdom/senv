package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/storage"
)

func getSSHManager() (*ssh.Manager, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return nil, err
	}
	if auth.hasKey() {
		return ssh.NewManagerWithKey(auth.storage, auth.key), nil
	}
	return ssh.NewManager(auth.storage, auth.password), nil
}

// sshInteractiveSelection can be disabled by tests that invoke RunE directly.
var sshInteractiveSelection = true

var keypairCmd = &cobra.Command{
	Use:   "keypair",
	Short: "Manage encrypted SSH keypairs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	keypairImportFile    string
	keypairImportGroup   string
	keypairImportForce   bool
	keypairDeleteForce   bool
	keypairMaterialForce bool
)

var keypairImportCmd = &cobra.Command{
	Use:   "import <name> --file <private-key>",
	Short: "Import an existing SSH private key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if keypairImportFile == "" {
			return fmt.Errorf("--file is required")
		}
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		summary, err := mgr.ImportKeyPairWithGroup(args[0], keypairImportFile, keypairImportGroup, keypairImportForce)
		detail := "import"
		if keypairImportForce {
			detail = "import --force"
		}
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:"+args[0], false, detail+" 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:"+summary.Name, true, detail)
		if summary.Fingerprint != "" {
			fmt.Printf("✓ Imported keypair %s (%s)\n", summary.Name, summary.Fingerprint)
		} else {
			fmt.Printf("✓ Imported keypair %s (pubkey: none)\n", summary.Name)
		}
		return nil
	},
}

var keypairListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SSH keypair metadata",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		summaries, err := mgr.ListKeyPairs()
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			fmt.Println("No SSH keypairs found")
			return nil
		}
		for _, s := range summaries {
			publicKey := s.PublicKey
			if publicKey == "" {
				publicKey = "pubkey: none"
			} else {
				publicKey = strings.Fields(publicKey)[0] + " " + s.Fingerprint
			}
			line := fmt.Sprintf("  %-24s %s  imported %s", s.Name, publicKey, s.ImportedAt.Format("2006-01-02 15:04"))
			if s.Group != "" {
				line += fmt.Sprintf(" group:%s", s.Group)
			}
			fmt.Println(line)
		}
		return nil
	},
}

var keypairMaterializeCmd = &cobra.Command{
	Use:   "materialize <name>",
	Short: "Decrypt a private key to ~/.ssh/senv/<name>",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		path, err := mgr.Materialize(args[0], keypairMaterialForce)
		detail := "materialize"
		if keypairMaterialForce {
			detail = "materialize --force"
		}
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:"+args[0], false, detail+" 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:"+args[0], true, detail)
		fmt.Printf("✓ Materialized %s to %s (0600)\n", args[0], path)
		return nil
	},
}

var keypairDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an SSH keypair and clear host references with --force",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		references, err := mgr.DeleteKeyPair(args[0], keypairDeleteForce)
		detail := "delete"
		if keypairDeleteForce {
			detail = "delete --force"
		}
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:"+args[0], false, detail+" 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:"+args[0], true, detail)
		fmt.Printf("✓ Deleted keypair %s\n", args[0])
		if len(references) > 0 {
			fmt.Printf("✓ Cleared references on: %s\n", strings.Join(references, ", "))
		}
		return nil
	},
}

var keypairRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename an SSH keypair and update host references",
	Long: `Rename an SSH keypair. Hosts that reference it keep working: their
identityKey is rewritten in the same vault mutation. Key material is unchanged.

  senv keypair rename web-key prod-key`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		updated, err := mgr.RenameKeyPair(args[0], args[1])
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:"+args[0], false, "rename 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:"+args[1], true, "rename "+args[0])
		fmt.Printf("✓ Renamed keypair %s -> %s\n", args[0], args[1])
		if len(updated) > 0 {
			fmt.Printf("✓ Updated %d host reference(s): %s\n", len(updated), strings.Join(updated, ", "))
		}
		return nil
	},
}

var keypairEditGroup string

var keypairEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit keypair metadata (group) without touching key material",
	Long: `Edit an SSH keypair's group in place, without launching an editor.
Group is the only keypair metadata editable in place; key material never
changes. An empty value clears the group (ungrouped). Group changes only
affect future materialize/export paths; already materialized files are not
moved (use keypair prune to clean leftovers).

  senv keypair edit web-key --group prod   # move to group prod
  senv keypair edit web-key --group ""     # clear group`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("group") {
			return fmt.Errorf("--group is required: senv keypair edit <name> --group <group> (empty value clears the group)")
		}
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		if err := mgr.UpdateKeyPair(args[0], func(k *storage.KeyPairEntry) error {
			k.Group = keypairEditGroup
			return nil
		}); err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:"+args[0], false, "edit 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:"+args[0], true, "edit --group")
		fmt.Printf("✓ Updated keypair %s\n", args[0])
		return nil
	},
}

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Manage encrypted SSH host profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	hostAddHostname    string
	hostAddUser        string
	hostAddPort        int
	hostAddProxyJump   string
	hostAddKeypair     string
	hostAddKeyFile     string
	hostAddKeypairName string
	hostAddGroup       string
	hostAddTags        []string
	hostAddAttrs       []string
	hostForce          bool
	hostEditGroup      string
)

var hostAddCmd = &cobra.Command{
	Use:   "add <alias>",
	Short: "Add an SSH host profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		extra, err := parseAttrs(hostAddAttrs)
		if err != nil {
			return err
		}
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		identityKey, err := resolveHostKeypair(mgr, hostAddKeypair, hostAddKeyFile, hostAddKeypairName)
		if err != nil {
			return err
		}
		host := &storage.HostEntry{
			Alias:       args[0],
			Hostname:    hostAddHostname,
			User:        hostAddUser,
			Port:        hostAddPort,
			ProxyJump:   hostAddProxyJump,
			IdentityKey: identityKey,
			Group:       hostAddGroup,
			Tags:        hostAddTags,
			Extra:       extra,
		}
		if err := mgr.AddHost(host); err != nil {
			auditOp(session.AuditOpSSHHost, "host:"+args[0], false, "add 失败")
			return err
		}
		auditOp(session.AuditOpSSHHost, "host:"+args[0], true, "add")
		fmt.Printf("✓ Added host %s\n", args[0])
		return nil
	},
}

var hostGetCmd = &cobra.Command{
	Use:   "get <alias>",
	Short: "Show an SSH host profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		host, err := mgr.GetHost(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Host %s\n", host.Alias)
		fmt.Printf("  HostName: %s\n", host.Hostname)
		if host.User != "" {
			fmt.Printf("  User: %s\n", host.User)
		}
		if host.Port != 0 {
			fmt.Printf("  Port: %d\n", host.Port)
		}
		if host.ProxyJump != "" {
			fmt.Printf("  ProxyJump: %s\n", host.ProxyJump)
		}
		if host.IdentityKey != "" {
			fmt.Printf("  IdentityKey: %s\n", host.IdentityKey)
		}
		if host.Group != "" {
			fmt.Printf("  Group: %s\n", host.Group)
		}
		if len(host.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(host.Tags, ", "))
		}
		for key, value := range host.Extra {
			fmt.Printf("  %s: %s\n", key, value)
		}
		fmt.Printf("  Updated: %s\n", host.UpdatedAt.Format("2006-01-02 15:04"))
		return nil
	},
}

var hostEditCmd = &cobra.Command{
	Use:   "edit <alias>",
	Short: "Edit an SSH host profile in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		// --group set explicitly applies a single-field update without
		// launching the editor; other fields keep their current values.
		if cmd.Flags().Changed("group") {
			if err := mgr.UpdateHost(args[0], func(host *storage.HostEntry) error {
				host.Group = hostEditGroup
				return nil
			}); err != nil {
				auditOp(session.AuditOpSSHHost, "host:"+args[0], false, "edit 失败")
				return err
			}
			auditOp(session.AuditOpSSHHost, "host:"+args[0], true, "edit --group")
			fmt.Printf("✓ Updated host %s\n", args[0])
			return nil
		}
		editorSession, err := mgr.PrepareHostEditor(args[0])
		if err != nil {
			return err
		}
		if err := editorSession.HostEditorCommand().Run(); err != nil {
			os.Remove(editorSession.TmpPath)
			auditOp(session.AuditOpSSHHost, "host:"+args[0], false, "edit editor 失败")
			return fmt.Errorf("run editor: %w", err)
		}
		changed, err := mgr.FinishHostEditor(editorSession)
		if err != nil {
			auditOp(session.AuditOpSSHHost, "host:"+args[0], false, "edit 失败")
			return err
		}
		if changed {
			auditOp(session.AuditOpSSHHost, "host:"+args[0], true, "edit")
			fmt.Printf("✓ Updated host %s\n", args[0])
		} else {
			fmt.Println("No changes detected")
		}
		return nil
	},
}

var hostListCmd = &cobra.Command{
	Use:   "list",
	Short: "List SSH host profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		hosts, err := mgr.ListHosts()
		if err != nil {
			return err
		}
		if len(hosts) == 0 {
			fmt.Println("No SSH hosts found")
			return nil
		}
		for _, host := range hosts {
			identity := "-"
			if host.IdentityKey != "" {
				identity = host.IdentityKey
			}
			line := fmt.Sprintf("  %-24s %-30s %-16s key:%s", host.Alias, host.Hostname, host.User, identity)
			if host.Group != "" {
				line += fmt.Sprintf(" group:%s", host.Group)
			}
			fmt.Println(line)
		}
		return nil
	},
}

var hostDeleteCmd = &cobra.Command{
	Use:   "delete <alias>",
	Short: "Delete an SSH host profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		if err := mgr.DeleteHost(args[0]); err != nil {
			auditOp(session.AuditOpSSHHost, "host:"+args[0], false, "delete 失败")
			return err
		}
		auditOp(session.AuditOpSSHHost, "host:"+args[0], true, "delete")
		fmt.Printf("✓ Deleted host %s\n", args[0])
		return nil
	},
}

var (
	hostExportAlias string
	hostExportGroup string
	hostExportOut   string
)

var hostExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Apply host profiles to ~/.ssh/senv (render-only via --output)",
	Long: `Export SSH host profiles into the local OpenSSH setup.

By default (apply mode) export maintains the grouped layout under
~/.ssh/senv/: per-group fragments in groups/, missing referenced private
keys materialized into keys/<group>/, and a single glob Include line
registered at the top of ~/.ssh/config. After it runs, ssh resolves the
exported aliases directly — no manual wiring.

  senv host export                # rebuild all group fragments
  senv host export --group prod   # rebuild only groups/prod.conf
  senv host export --host web     # rebuild the group fragment hosting web
  senv host export --output -     # pure render to stdout, no side effects
  senv host unexport              # withdraw: remove Include + group fragments`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if hostExportAlias != "" && hostExportGroup != "" {
			return fmt.Errorf("--host and --group are mutually exclusive")
		}
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		filter := ssh.RenderFilter{Host: hostExportAlias, Group: hostExportGroup}
		detail := "export"
		switch {
		case hostExportGroup != "":
			detail = "export --group " + hostExportGroup
		case hostExportAlias != "":
			detail = "export --host " + hostExportAlias
		}
		if hostExportOut != "" {
			// 纯渲染模式：片段到 stdout 或指定文件，零文件副作用。
			rr, err := mgr.Render(filter)
			if err != nil {
				auditOp(session.AuditOpSSHHost, "host:export", false, detail+" --output 失败")
				return err
			}
			var b strings.Builder
			for _, group := range rr.Order {
				fragment := rr.Fragments[group]
				b.WriteString(fragment)
				if !strings.HasSuffix(fragment, "\n") {
					b.WriteString("\n")
				}
			}
			if hostExportOut == "-" {
				fmt.Print(b.String())
			} else if err := storage.WriteSensitiveFile(hostExportOut, []byte(b.String()), 0o700, 0o600); err != nil {
				auditOp(session.AuditOpSSHHost, "host:export", false, detail+" --output 失败")
				return err
			}
			auditOp(session.AuditOpSSHHost, "host:export", true, detail+" --output")
			return nil
		}
		res, applyErr := mgr.Apply(filter)
		if res != nil {
			printApplySummary(res)
			for _, w := range res.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
		}
		if applyErr != nil {
			auditOp(session.AuditOpSSHHost, "host:export", false, detail+" 失败")
			return applyErr
		}
		auditOp(session.AuditOpSSHHost, "host:export", true, detail)
		return nil
	},
}

var hostUnexportCmd = &cobra.Command{
	Use:   "unexport",
	Short: "Withdraw the export: remove the senv Include line and group fragments",
	Long: `Remove the senv Include line from ~/.ssh/config and delete the group
fragments under ~/.ssh/senv/groups/. Vault archives and materialized
private keys under keys/ are left untouched (use keypair prune for those).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		unregistered, groupsRemoved, err := mgr.Unexport()
		if err != nil {
			auditOp(session.AuditOpSSHHost, "host:unexport", false, "unexport 失败")
			return err
		}
		if unregistered {
			fmt.Println("✓ removed senv Include from ~/.ssh/config")
		}
		if groupsRemoved {
			fmt.Println("✓ deleted group fragments under ~/.ssh/senv/groups")
		}
		if !unregistered && !groupsRemoved {
			fmt.Println("nothing to unexport")
		}
		auditOp(session.AuditOpSSHHost, "host:unexport", true, "unexport")
		return nil
	},
}

var keypairPruneForce bool

var keypairPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete materialized private keys that no host references",
	Long: `List materialized private keys under ~/.ssh/senv/ that no host's
identityKey references (including leftovers at old paths after a keypair
changed groups) and delete them after confirmation.

Files are only deleted after an explicit confirmation; in a non-interactive
terminal re-run with --force. Vault archives are never touched.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		candidates, err := mgr.PruneCandidates()
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:prune", false, "prune 失败")
			return err
		}
		if len(candidates) == 0 {
			fmt.Println("no unreferenced materialized keys")
			return nil
		}
		for _, c := range candidates {
			line := "  " + c.Path
			if c.InVault {
				line += " (keypair still in vault)"
			}
			fmt.Println(line)
		}
		if !keypairPruneForce {
			if !stdinIsTerminal() {
				err := fmt.Errorf("non-interactive terminal: re-run with --force to delete %d file(s)", len(candidates))
				auditOp(session.AuditOpSSHKey, "keypair:prune", false, "prune 需 --force")
				return err
			}
			fmt.Printf("delete %d file(s)? [y/N]: ", len(candidates))
			var answer string
			if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
				fmt.Println("aborted")
				return nil
			}
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("aborted")
				return nil
			}
		}
		paths := make([]string, 0, len(candidates))
		for _, c := range candidates {
			paths = append(paths, c.Path)
		}
		deleted, err := ssh.DeletePrunedFiles(paths)
		if err != nil {
			auditOp(session.AuditOpSSHKey, "keypair:prune", false, "prune 失败")
			return err
		}
		auditOp(session.AuditOpSSHKey, "keypair:prune", true, fmt.Sprintf("prune %d 个文件", len(deleted)))
		fmt.Printf("✓ deleted %d file(s)\n", len(deleted))
		return nil
	},
}

// printApplySummary 输出应用导出的结果摘要（各节仅在有内容时出现）。
func printApplySummary(res *ssh.ApplyResult) {
	baseNames := func(paths []string) string {
		names := make([]string, 0, len(paths))
		for _, p := range paths {
			names = append(names, filepath.Base(p))
		}
		return strings.Join(names, ", ")
	}
	if len(res.Written) > 0 {
		fmt.Printf("✓ groups rewritten: %s\n", baseNames(res.Written))
	}
	if len(res.Pruned) > 0 {
		fmt.Printf("✓ pruned stale fragments: %s\n", baseNames(res.Pruned))
	}
	if len(res.Materialized) > 0 {
		fmt.Printf("✓ materialized keys: %s\n", strings.Join(res.Materialized, ", "))
	}
	if len(res.KeysSkipped) > 0 {
		fmt.Printf("· skipped (already materialized): %s\n", strings.Join(res.KeysSkipped, ", "))
	}
	switch {
	case res.Registered:
		fmt.Println("✓ registered Include in ~/.ssh/config")
	case res.IncludeExisted:
		fmt.Println("· Include already registered in ~/.ssh/config")
	}
}

func resolveHostKeypair(mgr *ssh.Manager, keypairName, keyFile, importName string) (string, error) {
	switch {
	case keyFile != "":
		if importName == "" {
			return "", fmt.Errorf("--key-file requires --keypair-name")
		}
		if _, err := mgr.ImportKeyPair(importName, keyFile, false); err != nil {
			return "", err
		}
		return importName, nil
	case keypairName != "":
		if _, err := mgr.GetKeyPairSummary(keypairName); err != nil {
			return "", err
		}
		return keypairName, nil
	default:
		if !stdinIsTerminal() || !sshInteractiveSelection {
			return "", nil
		}
		summaries, err := mgr.ListKeyPairs()
		if err != nil || len(summaries) == 0 {
			return "", err
		}
		fmt.Println("Available SSH keypairs:")
		for i, summary := range summaries {
			fmt.Printf("  %d. %s", i+1, summary.Name)
			if summary.Fingerprint != "" {
				fmt.Printf(" (%s)", summary.Fingerprint)
			}
			fmt.Println()
		}
		fmt.Printf("  %d. Skip identity\n", len(summaries)+1)
		fmt.Print("Select keypair [1-" + strconv.Itoa(len(summaries)+1) + "]: ")
		var answer string
		if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
			return "", nil
		}
		choice, err := strconv.Atoi(answer)
		if err != nil || choice < 1 || choice > len(summaries)+1 {
			return "", fmt.Errorf("invalid keypair selection %q", answer)
		}
		if choice == len(summaries)+1 {
			return "", nil
		}
		return summaries[choice-1].Name, nil
	}
}

func parseAttrs(values []string) (map[string]string, error) {
	extra := make(map[string]string, len(values))
	for _, value := range values {
		key, attrValue, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid --attr %q: expected key=value", value)
		}
		extra[key] = attrValue
	}
	return extra, nil
}

func init() {
	rootCmd.AddCommand(keypairCmd, hostCmd)
	keypairCmd.AddCommand(keypairImportCmd, keypairListCmd, keypairMaterializeCmd, keypairRenameCmd, keypairEditCmd, keypairPruneCmd, keypairDeleteCmd)
	hostCmd.AddCommand(hostAddCmd, hostGetCmd, hostEditCmd, hostListCmd, hostDeleteCmd, hostExportCmd, hostUnexportCmd)

	keypairImportCmd.Flags().StringVar(&keypairImportFile, "file", "", "path to an existing private key")
	keypairImportCmd.Flags().StringVar(&keypairImportGroup, "group", "", "keypair group (single value, empty = ungrouped)")
	keypairImportCmd.Flags().BoolVar(&keypairImportForce, "force", false, "overwrite an existing keypair")
	keypairEditCmd.Flags().StringVar(&keypairEditGroup, "group", "", "set the keypair group without launching an editor (empty = ungrouped)")
	keypairMaterializeCmd.Flags().BoolVar(&keypairMaterialForce, "force", false, "overwrite an existing materialized file")
	keypairDeleteCmd.Flags().BoolVar(&keypairDeleteForce, "force", false, "delete even if referenced and clear references")

	hostAddCmd.Flags().StringVar(&hostAddHostname, "hostname", "", "remote hostname or IP")
	hostAddCmd.Flags().StringVar(&hostAddUser, "user", "", "remote login user")
	hostAddCmd.Flags().IntVar(&hostAddPort, "port", 0, "SSH port")
	hostAddCmd.Flags().StringVar(&hostAddProxyJump, "proxy-jump", "", "existing host alias to use as ProxyJump")
	hostAddCmd.Flags().StringVar(&hostAddKeypair, "keypair", "", "existing keypair name")
	hostAddCmd.Flags().StringVar(&hostAddKeyFile, "key-file", "", "import this private key before creating the host")
	hostAddCmd.Flags().StringVar(&hostAddKeypairName, "keypair-name", "", "name for the key imported from --key-file")
	hostAddCmd.Flags().StringVar(&hostAddGroup, "group", "", "host group (single value, empty = ungrouped)")
	hostAddCmd.Flags().StringSliceVar(&hostAddTags, "tag", nil, "host tag (repeatable)")
	hostAddCmd.Flags().StringSliceVar(&hostAddAttrs, "attr", nil, "extra OpenSSH key=value (repeatable)")
	hostEditCmd.Flags().StringVar(&hostEditGroup, "group", "", "set the host group without launching the editor")
	hostDeleteCmd.Flags().BoolVar(&hostForce, "force", false, "acknowledge deletion")
	hostExportCmd.Flags().StringVar(&hostExportAlias, "host", "", "export only this host alias (apply mode: rebuild its group fragment)")
	hostExportCmd.Flags().StringVar(&hostExportGroup, "group", "", "export only this host group (apply mode: rebuild only this group fragment)")
	hostExportCmd.Flags().StringVar(&hostExportOut, "output", "", "render-only mode: write the fragment to this file ('-' for stdout), no side effects")
	keypairPruneCmd.Flags().BoolVar(&keypairPruneForce, "force", false, "delete without interactive confirmation (required in non-interactive terminals)")
	addRefreshFlag(keypairListCmd)
	addRefreshFlag(hostListCmd)
	addRefreshFlag(hostGetCmd)
	addRefreshFlag(hostExportCmd)
}
