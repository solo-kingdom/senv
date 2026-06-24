package tui

import (
	"fmt"
	"os/exec"
)

// copyToClipboard copies a value to the system clipboard using the first
// available clipboard tool (pbcopy/xclip/xsel/wl-copy). It mirrors the logic
// in text.Manager.GetToClipboard so the TUI does not depend on the manager's
// private implementation, and so it can be reused across all tabs.
func copyToClipboard(value string) error {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd = exec.Command("pbcopy")
	} else if _, err := exec.LookPath("xclip"); err == nil {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd = exec.Command("wl-copy")
	} else {
		return fmt.Errorf("no clipboard command found (install pbcopy, xclip, xsel, or wl-copy)")
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to open clipboard stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start clipboard command: %w", err)
	}
	if _, err := stdin.Write([]byte(value)); err != nil {
		return fmt.Errorf("failed to write to clipboard: %w", err)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clipboard command failed: %w", err)
	}
	return nil
}
