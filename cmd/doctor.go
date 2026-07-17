package cmd

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose metadata/data consistency",
	Long: "Check whether metadata.json and the encrypted data files (env, text, config)\n" +
		"share the same key.\n\n" +
		"Reuses a valid session when available; otherwise prompts for a one-time\n" +
		"password (does not write a session). When the project is desynced, it uses\n" +
		"the cached session key as a recovery key to pinpoint which files are affected.\n\n" +
		"Run this when you see a desync error or after a `senv git pull` that touched metadata.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctorAt(getConfigPath(), getDataPath(), authPrompt, nil)
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// runDoctorAt is the path/prompter-injectable core, used by tests.
// A nil out defaults to os.Stdout.
func runDoctorAt(configPath, dataPath string, prompt passwordPrompter, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}

	key, store, warn, err := resolveDiagnosticKey(configPath, dataPath, prompt)
	if err != nil {
		return err
	}
	if warn != "" {
		fmt.Fprintln(out, warn)
	}

	report, err := store.CheckConsistency(key)
	if err != nil {
		return err
	}
	printDoctorReport(out, report)

	if !report.AllOK() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Recovery guidance:")
		fmt.Fprintln(out, "  - If metadata.json was replaced (git pull / re-init), restore the")
		fmt.Fprintln(out, "    metadata that matches your data files, or re-encrypt the data.")
		fmt.Fprintln(out, "  - Files listed above cannot be decrypted with the current key.")
		fmt.Fprintln(out, "  - Your session key (if any) is preserved; do not run `senv session clear`")
		fmt.Fprintln(out, "    until you have recovered the data.")
	}
	return nil
}

// resolveDiagnosticKey obtains a key for probing. On a desync (normal auth
// refuses), it falls back to the cached session key as a recovery key so doctor
// can still pinpoint the affected files.
func resolveDiagnosticKey(configPath, dataPath string, prompt passwordPrompter) (key []byte, store *storage.Manager, warn string, err error) {
	auth, aerr := resolveAuth(configPath, dataPath, prompt)
	if aerr == nil {
		if auth.hasKey() {
			return auth.key, auth.storage, "", nil
		}
		md, err := auth.storage.LoadMetadata()
		if err != nil {
			return nil, auth.storage, "", err
		}
		salt, derr := base64.StdEncoding.DecodeString(md.Salt)
		if derr != nil {
			return nil, auth.storage, "", fmt.Errorf("failed to decode salt: %w", derr)
		}
		return crypto.DeriveKey(auth.password, salt), auth.storage, "", nil
	}

	// Desync: use the cached session key to diagnose. Doctor's whole point is to
	// be useful precisely when normal auth fails because of a desync.
	if errors.Is(aerr, storage.ErrDataDesync) {
		sm := session.NewManager(configPath, dataPath)
		peeked, _, perr := sm.PeekCachedKey()
		if perr == nil {
			return peeked, storage.NewManager(configPath, dataPath), "⚠ " + aerr.Error(), nil
		}
	}
	return nil, nil, "", aerr
}

// printDoctorReport writes a human-readable consistency report. It deliberately
// emits only counts, OK/NOT-OK flags and file names — never plaintext.
func printDoctorReport(out io.Writer, r *storage.ConsistencyReport) {
	status := func(ok bool) string {
		if ok {
			return "OK"
		}
		return "NOT OK"
	}
	fmt.Fprintf(out, "metadata <-> key:     %s\n", status(r.MetadataKeyOK))
	printFileProbes(out, "env files", r.EnvFiles)
	printFileProbes(out, "text files", r.TextFiles)
	printFileProbes(out, "config files", r.ConfigFiles)
}

func printFileProbes(out io.Writer, label string, p storage.FileProbes) {
	if p.Total == 0 {
		fmt.Fprintf(out, "%-16s (none)\n", label+":")
		return
	}
	fmt.Fprintf(out, "%-16s (%d/%d)\n", label+":", p.OK, p.Total)
	for _, name := range p.Failed {
		fmt.Fprintf(out, "  ! %s: cannot decrypt with current key\n", name)
	}
}
