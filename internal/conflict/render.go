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
	"github.com/wii/senv/internal/skill"
	"github.com/wii/senv/internal/storage"
)

// Auth carries an optionally available vault key and the compatibility result
// for remote metadata. A nil Key means plaintext rendering is unavailable.
type Auth struct {
	Key                 []byte
	KeyEpoch            int
	RemoteKeyEpoch      int
	RemoteKeyCompatible bool
}

func NewAuth(key []byte, remoteMetadata []byte) Auth {
	return NewAuthWithLocalMetadata(key, nil, remoteMetadata)
}

func NewAuthWithLocalMetadata(key []byte, localMetadata []byte, remoteMetadata []byte) Auth {
	auth := Auth{Key: key, RemoteKeyCompatible: true}
	if len(localMetadata) > 0 {
		if md, err := storage.ParseMetadata(localMetadata); err == nil {
			auth.KeyEpoch = md.KeyEpoch
		}
	}
	if len(remoteMetadata) > 0 {
		if md, err := storage.ParseMetadata(remoteMetadata); err == nil {
			auth.RemoteKeyEpoch = md.KeyEpoch
		}
		auth.RemoteKeyCompatible = MetadataKeyCompatible(remoteMetadata, key)
	} else {
		auth.RemoteKeyEpoch = auth.KeyEpoch
	}
	return auth
}

func MetadataKeyCompatible(metadata []byte, key []byte) bool {
	if len(metadata) == 0 || len(key) == 0 {
		return false
	}
	md, err := storage.ParseMetadata(metadata)
	if err != nil {
		return false
	}
	_, err = crypto.DecryptWithAAD(key, md.PasswordKey, crypto.BuildEntryAAD("metadata_verifier", "", "", md.KeyEpoch))
	return err == nil
}

type decodedSide struct {
	Plaintext []byte
	UpdatedAt time.Time
	Size      int
}

func decodeSide(detail provider.ConflictDetail, side provider.ConflictSide, key []byte, keyEpoch int) (decodedSide, error) {
	kind := detail.Kind
	if side.Deleted {
		return decodedSide{}, nil
	}
	if kind == provider.KindSSHHost || kind == provider.KindSSHKeypair {
		// SSH 档案含私钥本体：冲突对比不解密、不产出明文，只保留元数据（ADR-0020）。
		return decodedSide{Size: side.Size}, nil
	}
	if kind == provider.KindSkillIndex {
		if crypto.IsV2Ciphertext(string(side.Ciphertext)) {
			if len(key) == 0 {
				return decodedSide{}, fmt.Errorf("vault key unavailable")
			}
			plain, err := decryptWithEpochFallback(key, string(side.Ciphertext), kind, "", "", keyEpoch)
			if err != nil {
				return decodedSide{}, err
			}
			return decodedSide{Plaintext: plain, Size: len(plain)}, nil
		}
		return decodedSide{Plaintext: side.Ciphertext, Size: len(side.Ciphertext)}, nil
	}
	if kind == provider.KindConfigIndex {
		payload := side.Ciphertext
		if crypto.IsV2Ciphertext(string(side.Ciphertext)) {
			if len(key) == 0 {
				return decodedSide{}, fmt.Errorf("vault key unavailable")
			}
			plain, err := decryptWithEpochFallback(key, string(side.Ciphertext), kind, "", "", keyEpoch)
			if err != nil {
				return decodedSide{}, err
			}
			payload = plain
		}
		var index storage.ConfigIndex
		if err := storage.FromJSON(payload, &index); err != nil {
			return decodedSide{}, err
		}
		return decodedSide{Plaintext: payload, Size: len(payload)}, nil
	}
	if len(key) == 0 {
		return decodedSide{}, fmt.Errorf("vault key unavailable")
	}
	plaintext, err := decryptWithEpochFallback(key, string(side.Ciphertext), kind, detail.Grp, detail.Key, keyEpoch)
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
	case provider.KindText, provider.KindBackup:
		var entry storage.TextEntry
		if err := storage.FromJSON(plaintext, &entry); err == nil {
			out.UpdatedAt = entry.UpdatedAt
			out.Size = entry.Size
		}
	case provider.KindEnvMeta, provider.KindTextMeta, provider.KindBackupMeta, provider.KindTreeMeta:
		var meta storage.EnvGroupMeta
		if err := storage.FromJSON(plaintext, &meta); err == nil {
			out.Plaintext = []byte(meta.Name)
		}
	case provider.KindTree:
		var entry struct {
			Encoding  string    `json:"encoding"`
			Content   string    `json:"content"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		if err := storage.FromJSON(plaintext, &entry); err == nil {
			out.UpdatedAt = entry.UpdatedAt
			if entry.Encoding == "base64" {
				out.Plaintext = []byte(fmt.Sprintf("<binary base64 (%d bytes)>", len(entry.Content)))
			} else {
				out.Plaintext = []byte(entry.Content)
				out.Size = len(entry.Content)
			}
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

func decryptWithEpochFallback(key []byte, encoded, kind, grp, entryKey string, keyEpoch int) ([]byte, error) {
	if !crypto.IsV2Ciphertext(encoded) {
		return crypto.Decrypt(key, encoded)
	}
	aad := crypto.BuildEntryAAD(kind, grp, entryKey, keyEpoch)
	if plain, err := crypto.DecryptWithAAD(key, encoded, aad); err == nil {
		return plain, nil
	}
	for e := keyEpoch - 1; e >= 0; e-- {
		if plain, err := crypto.DecryptWithAAD(key, encoded, crypto.BuildEntryAAD(kind, grp, entryKey, e)); err == nil {
			return plain, nil
		}
	}
	return nil, crypto.ErrDecryptionFailed
}

// DecryptEntryPayload 在客户端已解锁状态下解密单条非私钥密文（供 sync diff / 导出使用）。
// 对 ssh_host / ssh_keypair 永不解密并显式拒绝。
func DecryptEntryPayload(kind, grp, entryKey string, ciphertext []byte, auth Auth) ([]byte, error) {
	if kind == provider.KindSSHHost || kind == provider.KindSSHKeypair {
		return nil, fmt.Errorf("出于安全边界，禁止解密或导出 %s 条目", kind)
	}
	if (kind == provider.KindConfigIndex || kind == provider.KindSkillIndex) && !crypto.IsV2Ciphertext(string(ciphertext)) {
		return ciphertext, nil
	}
	if len(auth.Key) == 0 {
		return nil, fmt.Errorf("vault key unavailable")
	}
	return decryptWithEpochFallback(auth.Key, string(ciphertext), kind, grp, entryKey, auth.RemoteKeyEpoch)
}

func sideKey(kind string, side provider.ConflictSide, auth Auth, remote bool) []byte {
	if len(side.Ciphertext) == 0 || side.Deleted {
		return nil
	}
	if remote && !auth.RemoteKeyCompatible {
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
	case kind == provider.KindSSHHost || kind == provider.KindSSHKeypair:
		// SSH 档案含私钥本体：无论是否 reveal 都只渲染元数据（ADR-0005/0020）。
		b.WriteString("    content=<metadata only> plaintext not decoded\n")
	case kind == provider.KindSkill:
		if info, err := skill.Inspect(decoded.Plaintext); err == nil {
			fmt.Fprintf(&b, "    archive=%s files=%d payload=%dB uncompressed=%dB\n",
				archiveFormatLabel(info.Compressed), info.FileCount(), len(decoded.Plaintext), info.UncompressedSize)
		} else {
			fmt.Fprintf(&b, "    content=<skill archive %dB>\n", len(decoded.Plaintext))
		}
	case kind == provider.KindConfigIndex, kind == provider.KindSkillIndex:
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
	local, localErr := decodeSide(detail, detail.Local, auth.Key, auth.KeyEpoch)
	remoteKey := sideKey(detail.Kind, detail.Remote, auth, true)
	remote, remoteErr := decodeSide(detail, detail.Remote, remoteKey, auth.RemoteKeyEpoch)

	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s/%s\n", detail.Kind, detail.Grp, detail.Key)
	b.WriteString(renderSide("local", detail.Kind, detail.Local, local, localErr, reveal))
	b.WriteString(renderSide("remote", detail.Kind, detail.Remote, remote, remoteErr, reveal))
	if remoteErr != nil && len(auth.Key) > 0 && !auth.RemoteKeyCompatible {
		b.WriteString("  warning: remote metadata uses a different vault key\n")
	}
	if detail.Kind == provider.KindConfigIndex {
		if localErr == nil && remoteErr == nil {
			b.WriteString(RenderConfigIndexDiff(local.Plaintext, remote.Plaintext))
		} else {
			b.WriteString("  config index: encrypted or unavailable; unlock vault to view diff\n")
		}
	}
	if detail.Kind == provider.KindSkillIndex {
		if localErr == nil && remoteErr == nil {
			b.WriteString(RenderSkillIndexDiff(local.Plaintext, remote.Plaintext))
		} else {
			b.WriteString("  skill index: encrypted or unavailable; unlock vault to view diff\n")
		}
	}
	if detail.Kind == provider.KindSkill && !detail.Local.Deleted && !detail.Remote.Deleted {
		if localErr == nil && remoteErr == nil {
			if diff, err := CompareSkillArchives(local.Plaintext, remote.Plaintext); err == nil {
				b.WriteString(RenderSkillDiff(diff, reveal))
			} else {
				fmt.Fprintf(&b, "  skill archive diff unavailable: %v\n", err)
			}
		} else {
			b.WriteString("  skill archive: encrypted or unavailable; unlock vault to view diff\n")
		}
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
