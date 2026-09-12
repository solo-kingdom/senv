package cmd

import (
	"fmt"
	"os"
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
		summary, err := mgr.ImportKeyPair(args[0], keypairImportFile, keypairImportForce)
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
			fmt.Printf("  %-24s %s  imported %s\n", s.Name, publicKey, s.ImportedAt.Format("2006-01-02 15:04"))
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
	hostAddTags        []string
	hostAddAttrs       []string
	hostForce          bool
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
			fmt.Printf("  %-24s %-30s %-16s key:%s\n", host.Alias, host.Hostname, host.User, identity)
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
	hostExportOut   string
)

var hostExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Render an OpenSSH config fragment",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		mgr, err := getSSHManager()
		if err != nil {
			return err
		}
		config, warnings, err := mgr.Export(hostExportAlias)
		if err != nil {
			return err
		}
		for _, w := range warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
		}
		if hostExportOut == "" {
			fmt.Print(config)
			return nil
		}
		return storage.WriteSensitiveFile(hostExportOut, []byte(config), 0o700, 0o600)
	},
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
	keypairCmd.AddCommand(keypairImportCmd, keypairListCmd, keypairMaterializeCmd, keypairRenameCmd, keypairDeleteCmd)
	hostCmd.AddCommand(hostAddCmd, hostGetCmd, hostEditCmd, hostListCmd, hostDeleteCmd, hostExportCmd)

	keypairImportCmd.Flags().StringVar(&keypairImportFile, "file", "", "path to an existing private key")
	keypairImportCmd.Flags().BoolVar(&keypairImportForce, "force", false, "overwrite an existing keypair")
	keypairMaterializeCmd.Flags().BoolVar(&keypairMaterialForce, "force", false, "overwrite an existing materialized file")
	keypairDeleteCmd.Flags().BoolVar(&keypairDeleteForce, "force", false, "delete even if referenced and clear references")

	hostAddCmd.Flags().StringVar(&hostAddHostname, "hostname", "", "remote hostname or IP")
	hostAddCmd.Flags().StringVar(&hostAddUser, "user", "", "remote login user")
	hostAddCmd.Flags().IntVar(&hostAddPort, "port", 0, "SSH port")
	hostAddCmd.Flags().StringVar(&hostAddProxyJump, "proxy-jump", "", "existing host alias to use as ProxyJump")
	hostAddCmd.Flags().StringVar(&hostAddKeypair, "keypair", "", "existing keypair name")
	hostAddCmd.Flags().StringVar(&hostAddKeyFile, "key-file", "", "import this private key before creating the host")
	hostAddCmd.Flags().StringVar(&hostAddKeypairName, "keypair-name", "", "name for the key imported from --key-file")
	hostAddCmd.Flags().StringSliceVar(&hostAddTags, "tag", nil, "host tag (repeatable)")
	hostAddCmd.Flags().StringSliceVar(&hostAddAttrs, "attr", nil, "extra OpenSSH key=value (repeatable)")
	hostDeleteCmd.Flags().BoolVar(&hostForce, "force", false, "acknowledge deletion")
	hostExportCmd.Flags().StringVar(&hostExportAlias, "host", "", "export only this host alias")
	hostExportCmd.Flags().StringVar(&hostExportOut, "output", "", "write to a private file instead of stdout")
	addRefreshFlag(keypairListCmd)
	addRefreshFlag(hostListCmd)
	addRefreshFlag(hostGetCmd)
	addRefreshFlag(hostExportCmd)
}
