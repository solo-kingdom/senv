package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/wii/senv/internal/storage"
)

// HostEditorSession holds a decrypted, non-secret host record in a temporary
// file while an external editor is active.
type HostEditorSession struct {
	Alias    string
	TmpPath  string
	Original string
}

// PrepareHostEditor renders the current (or new) host as editable text.
func (m *Manager) PrepareHostEditor(alias string) (*HostEditorSession, error) {
	var content string
	if host, err := m.loadHost(alias); err == nil {
		content = renderHostForEditor(host)
	} else if !isNotExist(err) {
		return nil, err
	}
	tmpFile, err := os.CreateTemp("", "senv-host-*.conf")
	if err != nil {
		return nil, fmt.Errorf("create host editor temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("write host editor temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	return &HostEditorSession{Alias: alias, TmpPath: tmpPath, Original: content}, nil
}

// HostEditorCommand returns the editor invocation for the pending session.
func (s *HostEditorSession) HostEditorCommand() *exec.Cmd {
	cmd := exec.Command(editorCommand(), s.TmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// FinishHostEditor parses and persists edited fields; unchanged content is a
// no-op. The temp file is always removed.
func (m *Manager) FinishHostEditor(session *HostEditorSession) (bool, error) {
	if session == nil {
		return false, fmt.Errorf("host editor session is nil")
	}
	defer os.Remove(session.TmpPath)
	content, err := os.ReadFile(session.TmpPath)
	if err != nil {
		return false, fmt.Errorf("read edited host: %w", err)
	}
	edited := string(content)
	if edited == session.Original {
		return false, nil
	}
	host, err := parseHostEditor(session.Alias, edited)
	if err != nil {
		return false, err
	}
	err = m.UpdateHost(session.Alias, func(current *storage.HostEntry) error {
		*current = *host
		return nil
	})
	if errors.Is(err, os.ErrNotExist) && session.Original == "" {
		err = m.AddHost(host)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func renderHostForEditor(host *storage.HostEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "hostname %s\n", host.Hostname)
	fmt.Fprintf(&b, "user %s\n", host.User)
	if host.Port != 0 {
		fmt.Fprintf(&b, "port %d\n", host.Port)
	}
	fmt.Fprintf(&b, "proxy-jump %s\n", host.ProxyJump)
	fmt.Fprintf(&b, "identity-key %s\n", host.IdentityKey)
	fmt.Fprintf(&b, "group %s\n", host.Group)
	fmt.Fprintf(&b, "tags %s\n", strings.Join(host.Tags, ","))
	fmt.Fprintf(&b, "description %s\n", host.Description)
	keys := make([]string, 0, len(host.Extra))
	for key := range host.Extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "attr %s=%s\n", key, host.Extra[key])
	}
	return b.String()
}

func parseHostEditor(alias, content string) (*storage.HostEntry, error) {
	host := &storage.HostEntry{Alias: alias, Extra: map[string]string{}}
	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Bare known directives (for example an intentionally blank
		// "proxy-jump") still clear the corresponding field.
		if !strings.Contains(line, " ") {
			line += " "
		}
		key, value, found := strings.Cut(line, " ")
		if !found {
			key, value, found = strings.Cut(line, "=")
			if !found {
				return nil, fmt.Errorf("host editor line %d: expected \"key value\"", number+1)
			}
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "hostname":
			host.Hostname = value
		case "user":
			host.User = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("host editor line %d: invalid port %q", number+1, value)
			}
			host.Port = port
		case "proxy-jump", "proxyjump":
			host.ProxyJump = value
		case "identity-key", "identitykey":
			host.IdentityKey = value
		case "group":
			host.Group = value
		case "tags":
			if value != "" {
				for _, tag := range strings.Split(value, ",") {
					if tag = strings.TrimSpace(tag); tag != "" {
						host.Tags = append(host.Tags, tag)
					}
				}
			}
		case "description":
			host.Description = value
		case "attr":
			attrKey, attrValue, found := strings.Cut(value, "=")
			if !found {
				return nil, fmt.Errorf("host editor line %d: attr must be key=value", number+1)
			}
			attrKey = strings.TrimSpace(attrKey)
			host.Extra[attrKey] = strings.TrimSpace(attrValue)
		default:
			host.Extra[key] = value
		}
	}
	return host, nil
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func editorCommand() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "vim"
}
