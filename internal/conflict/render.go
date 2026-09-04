// Package conflict renders server-sync conflicts without weakening the trust
// boundary between ciphertext, plaintext, and user-facing diagnostics.
package conflict

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wii/senv/internal/crypto"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// Auth carries an optionally available vault key and the compatibility result
// for remote metadata. A nil Key means plaintext rendering is unavailable.
type Auth struct {
	Key                 []byte
	RemoteKeyCompatible bool
}

func NewAuth(key []byte, remoteMetadata []byte) Auth {
	compatible := true
	if len(remoteMetadata) > 0 {
		compatible = MetadataKeyCompatible(remoteMetadata, key)
	}
	return Auth{Key: key, RemoteKeyCompatible: compatible}
}

func MetadataKeyCompatible(metadata []byte, key []byte) bool {
	if len(metadata) == 0 || len(key) == 0 {
		return false
	}
	md, err := storage.ParseMetadata(metadata)
	if err != nil {
		return false
	}
	_, err = crypto.Decrypt(key, md.PasswordKey)
	return err == nil
}

type decodedSide struct {
	Plaintext []byte
	UpdatedAt time.Time
	Size      int
}

func decodeSide(kind string, side provider.ConflictSide, key []byte) (decodedSide, error) {
	if side.Deleted {
		return decodedSide{}, nil
	}
	if kind == provider.KindConfigIndex {
		var index storage.ConfigIndex
		if err := storage.FromJSON(side.Ciphertext, &index); err != nil {
			return decodedSide{}, err
		}
		return decodedSide{Plaintext: side.Ciphertext, Size: len(side.Ciphertext)}, nil
	}
	if len(key) == 0 {
		return decodedSide{}, fmt.Errorf("vault key unavailable")
	}
	plaintext, err := crypto.Decrypt(key, string(side.Ciphertext))
	if err != nil {
		return decodedSide{}, err
	}
	out := decodedSide{Plaintext: plaintext, Size: len(plaintext)}
	switch kind {
	case provider.KindEnv:
		var entry storage.EnvVarEntry
		if err := storage.FromJSON(plaintext, &entry); err == nil {
			out.UpdatedAt = entry.UpdatedAt
		}
	case provider.KindText:
		var entry storage.TextEntry
		if err := storage.FromJSON(plaintext, &entry); err == nil {
			out.UpdatedAt = entry.UpdatedAt
			out.Size = entry.Size
		}
	case provider.KindEnvMeta:
		var meta storage.EnvGroupMeta
		if err := storage.FromJSON(plaintext, &meta); err == nil {
			out.Plaintext = []byte(meta.Name)
		}
	}
	return out, nil
}

func maskValue(value string) string {
	if value == "" {
		return "<empty>"
	}
	return fmt.Sprintf("<masked %d bytes>", len(value))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func contentPreview(value []byte) string {
	if !utf8.Valid(value) {
		return "<binary content hidden>"
	}
	return string(value)
}

func sideKey(kind string, side provider.ConflictSide, auth Auth, remote bool) []byte {
	if len(side.Ciphertext) == 0 || side.Deleted {
		return nil
	}
	if remote && !auth.RemoteKeyCompatible {
		return nil
	}
	if kind == provider.KindConfigIndex {
		return nil
	}
	return auth.Key
}

func renderSide(label, kind string, side provider.ConflictSide, decoded decodedSide, decodeErr error, reveal bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s: revision=%d deleted=%v ciphertext=%dB", label, side.Revision, side.Deleted, side.Size)
	if !decoded.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, " updated=%s", formatTime(decoded.UpdatedAt))
	}
	b.WriteString("\n")
	switch {
	case side.Deleted:
		b.WriteString("    <deleted>\n")
	case decodeErr != nil:
		b.WriteString("    <content unavailable>\n")
	case kind == provider.KindEnv:
		var entry storage.EnvVarEntry
		_ = storage.FromJSON(decoded.Plaintext, &entry)
		value := maskValue(entry.Value)
		if reveal {
			value = entry.Value
		}
		fmt.Fprintf(&b, "    value=%s plaintext=%dB\n", value, len(entry.Value))
	case kind == provider.KindConfigIndex:
		b.WriteString("    semantic differences are listed below\n")
	default:
		value := "<text hidden>"
		if reveal {
			value = contentPreview(decoded.Plaintext)
		} else if !utf8.Valid(decoded.Plaintext) {
			value = "<binary content>"
		}
		fmt.Fprintf(&b, "    content=%s plaintext=%dB\n", value, decoded.Size)
	}
	return b.String()
}

// RenderDetail renders one conflict. It never includes ciphertext bytes. Text
// and config plaintext require reveal=true; env is additionally masked unless
// explicitly revealed.
func RenderDetail(detail provider.ConflictDetail, auth Auth, reveal bool) string {
	local, localErr := decodeSide(detail.Kind, detail.Local, auth.Key)
	remoteKey := sideKey(detail.Kind, detail.Remote, auth, true)
	remote, remoteErr := decodeSide(detail.Kind, detail.Remote, remoteKey)

	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s/%s\n", detail.Kind, detail.Grp, detail.Key)
	b.WriteString(renderSide("local", detail.Kind, detail.Local, local, localErr, reveal))
	b.WriteString(renderSide("remote", detail.Kind, detail.Remote, remote, remoteErr, reveal))
	if remoteErr != nil && len(auth.Key) > 0 && !auth.RemoteKeyCompatible {
		b.WriteString("  warning: remote metadata uses a different vault key\n")
	}
	if detail.Kind == provider.KindConfigIndex {
		b.WriteString(RenderConfigIndexDiff(detail.Local.Ciphertext, detail.Remote.Ciphertext))
	}
	return b.String()
}

func RenderConfigIndexDiff(localBlob, remoteBlob []byte) string {
	var local, remote storage.ConfigIndex
	localErr := storage.FromJSON(localBlob, &local)
	remoteErr := storage.FromJSON(remoteBlob, &remote)
	if localErr != nil || remoteErr != nil {
		return "  config index: invalid JSON; merge is unavailable\n"
	}
	names := make(map[string]bool)
	for name := range local.Configs {
		names[name] = true
	}
	for name := range remote.Configs {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	var b strings.Builder
	b.WriteString("  config index differences:\n")
	for _, name := range ordered {
		l, hasLocal := local.Configs[name]
		r, hasRemote := remote.Configs[name]
		if !hasLocal {
			fmt.Fprintf(&b, "    %s: remote added (group=%s)\n", name, r.NormalizedGroup())
			continue
		}
		if !hasRemote {
			fmt.Fprintf(&b, "    %s: local only (group=%s)\n", name, l.NormalizedGroup())
			continue
		}
		if l.TargetPath != r.TargetPath {
			fmt.Fprintf(&b, "    %s target_path: local=%q remote=%q\n", name, l.TargetPath, r.TargetPath)
		}
		if l.NormalizedGroup() != r.NormalizedGroup() {
			fmt.Fprintf(&b, "    %s group: local=%s remote=%s\n", name, l.NormalizedGroup(), r.NormalizedGroup())
		}
		if l.Description != r.Description {
			fmt.Fprintf(&b, "    %s description changed\n", name)
		}
	}
	if !strings.Contains(b.String(), "    ") {
		b.WriteString("    no semantic differences\n")
	}
	return b.String()
}

func RenderMetadata(localBlob, remoteBlob []byte, auth Auth) string {
	local, localErr := storage.ParseMetadata(localBlob)
	remote, remoteErr := storage.ParseMetadata(remoteBlob)
	var b strings.Builder
	b.WriteString("vault metadata\n")
	if localErr != nil || remoteErr != nil {
		b.WriteString("  status: invalid metadata; choose one whole side\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  version: local=%s remote=%s\n", local.Version, remote.Version)
	fmt.Fprintf(&b, "  kdf_iterations: local=%d remote=%d\n", local.KDFIterations, remote.KDFIterations)
	fmt.Fprintf(&b, "  updated: local=%s remote=%s\n", formatTime(local.UpdatedAt), formatTime(remote.UpdatedAt))
	if len(auth.Key) > 0 {
		fmt.Fprintf(&b, "  remote key compatible: %v\n", auth.RemoteKeyCompatible)
	} else {
		b.WriteString("  remote key compatible: unknown (vault key unavailable)\n")
	}
	b.WriteString("  raw metadata editing is disabled\n")
	return b.String()
}
