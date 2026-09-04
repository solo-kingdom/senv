package conflict

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

const (
	localMarker  = "<<<<<<< SENV_LOCAL"
	sepMarker    = "======="
	remoteMarker = ">>>>>>> SENV_REMOTE"
)

// removeAll is a narrow test seam for cleanup-failure reporting.
var removeAll = os.RemoveAll

// MergeSession owns a one-shot private editor buffer. Closing it removes the
// whole directory so editor swap/backup files are not left behind.
type MergeSession struct {
	Kind       string
	Dir        string
	Path       string
	LocalData  []byte
	RemoteData []byte
	editor     string
}

func editorCommandName() string {
	for _, name := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if name != "" {
			return name
		}
	}
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	return "vim"
}

func mergePlaintext(kind string, side provider.ConflictSide, key []byte) ([]byte, error) {
	decoded, err := decodeSide(kind, side, key)
	if err != nil {
		return nil, err
	}
	return decoded.Plaintext, nil
}

func PrepareMergeEditor(detail provider.ConflictDetail, key []byte) (*MergeSession, error) {
	if detail.Local.Deleted || detail.Remote.Deleted {
		return nil, fmt.Errorf("deleted conflicts cannot be editor-merged")
	}
	switch detail.Kind {
	case provider.KindEnv, provider.KindText, provider.KindConfig, provider.KindConfigIndex:
	default:
		return nil, fmt.Errorf("kind %s does not support editor merge", detail.Kind)
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("manual merge requires a vault key")
	}

	localPlain, err := mergePlaintext(detail.Kind, detail.Local, key)
	if err != nil {
		return nil, fmt.Errorf("decrypt local conflict: %w", err)
	}
	remotePlain, err := mergePlaintext(detail.Kind, detail.Remote, key)
	if err != nil {
		return nil, fmt.Errorf("decrypt remote conflict: %w", err)
	}
	if detail.Kind == provider.KindConfigIndex {
		if localPlain, err = prettyJSON(localPlain); err != nil {
			return nil, fmt.Errorf("format local config index: %w", err)
		}
		if remotePlain, err = prettyJSON(remotePlain); err != nil {
			return nil, fmt.Errorf("format remote config index: %w", err)
		}
	}
	if !utf8.Valid(localPlain) || !utf8.Valid(remotePlain) {
		return nil, fmt.Errorf("binary conflicts cannot be editor-merged")
	}

	dir, err := os.MkdirTemp("", "senv-conflict-")
	if err != nil {
		return nil, fmt.Errorf("create private merge directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = removeAll(dir)
		return nil, fmt.Errorf("secure private merge directory: %w", err)
	}
	path := filepath.Join(dir, "merge")
	if err := os.WriteFile(path, []byte(TwoWayBuffer(string(localPlain), string(remotePlain))), 0o600); err != nil {
		_ = removeAll(dir)
		return nil, fmt.Errorf("write merge buffer: %w", err)
	}
	return &MergeSession{
		Kind: detail.Kind, Dir: dir, Path: path,
		LocalData: detail.Local.Ciphertext, RemoteData: detail.Remote.Ciphertext,
		editor: editorCommandName(),
	}, nil
}

func prettyJSON(data []byte) ([]byte, error) {
	var value any
	if err := storage.FromJSON(data, &value); err != nil {
		return nil, err
	}
	return storage.ToJSON(value)
}

func TwoWayBuffer(local, remote string) string {
	return localMarker + "\n" + local + "\n" + sepMarker + "\n" + remote + "\n" + remoteMarker + "\n"
}

func (s *MergeSession) EditorCommand() *exec.Cmd {
	editor := s.editor
	if editor == "" {
		editor = editorCommandName()
	}
	cmd := exec.Command(editor, s.Path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

func (s *MergeSession) RunEditor() error {
	if err := s.EditorCommand().Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}

func (s *MergeSession) Close() error {
	if s == nil || s.Dir == "" {
		return nil
	}
	if err := removeAll(s.Dir); err != nil {
		return fmt.Errorf("remove private merge directory: %w", err)
	}
	return nil
}

func ValidateMergedBuffer(kind string, data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("merged content must be valid UTF-8")
	}
	if len(data) > storage.MaxTextSize {
		return fmt.Errorf("merged content exceeds %d bytes", storage.MaxTextSize)
	}
	for _, marker := range []string{localMarker, sepMarker, remoteMarker} {
		if strings.Contains(string(data), marker) {
			return fmt.Errorf("merge buffer still contains unresolved marker %s", marker)
		}
	}
	if kind == provider.KindConfigIndex {
		var index storage.ConfigIndex
		if err := storage.FromJSON(data, &index); err != nil {
			return fmt.Errorf("merged config index is invalid JSON: %w", err)
		}
		if index.Configs == nil {
			return fmt.Errorf("merged config index has no configs object")
		}
		for name, cfg := range index.Configs {
			if err := storage.ValidateName(name); err != nil {
				return fmt.Errorf("invalid merged config name: %w", err)
			}
			if err := storage.ValidateName(cfg.NormalizedGroup()); err != nil {
				return fmt.Errorf("invalid merged config group: %w", err)
			}
		}
	}
	return nil
}

func decryptEntry(kind string, ciphertext []byte, key []byte) ([]byte, error) {
	return crypto.Decrypt(key, string(ciphertext))
}

func (s *MergeSession) Finish(key []byte) ([]byte, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("read merged buffer: %w", err)
	}
	if err := ValidateMergedBuffer(s.Kind, data); err != nil {
		return nil, err
	}

	var plaintext []byte
	now := time.Now()
	switch s.Kind {
	case provider.KindEnv:
		var local, remote storage.EnvVarEntry
		if localBlob, err := decryptEntry(s.Kind, s.LocalData, key); err == nil {
			_ = storage.FromJSON(localBlob, &local)
		}
		if remoteBlob, err := decryptEntry(s.Kind, s.RemoteData, key); err == nil {
			_ = storage.FromJSON(remoteBlob, &remote)
		}
		entry := local
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = remote.CreatedAt
		}
		entry.Value = string(data)
		entry.UpdatedAt = now
		if plaintext, err = storage.ToJSON(entry); err != nil {
			return nil, err
		}
	case provider.KindText:
		var local, remote storage.TextEntry
		if localBlob, err := decryptEntry(s.Kind, s.LocalData, key); err == nil {
			_ = storage.FromJSON(localBlob, &local)
		}
		if remoteBlob, err := decryptEntry(s.Kind, s.RemoteData, key); err == nil {
			_ = storage.FromJSON(remoteBlob, &remote)
		}
		entry := local
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = remote.CreatedAt
		}
		entry.Value = string(data)
		entry.Size = len(data)
		entry.UpdatedAt = now
		if plaintext, err = storage.ToJSON(entry); err != nil {
			return nil, err
		}
	case provider.KindConfigIndex:
		var index storage.ConfigIndex
		if err := storage.FromJSON(data, &index); err != nil {
			return nil, err
		}
		plaintext, err = storage.ToJSON(index)
		if err != nil {
			return nil, err
		}
		return plaintext, nil
	default:
		plaintext = data
	}
	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt merged conflict: %w", err)
	}
	return []byte(ciphertext), nil
}
