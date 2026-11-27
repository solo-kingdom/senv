package conflict

import (
	"bytes"
	"encoding/json"

	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/storage"
)

// ConflictClassification 描述客户端对单条冲突的语义分类结果。
type ConflictClassification struct {
	Equivalent           bool   `json:"equivalent,omitempty"`
	Reason               string `json:"reason,omitempty"`
	DeleteVsFormatOnly   bool   `json:"delete_vs_format_only,omitempty"`
	RemoteHistoryChecked bool   `json:"remote_history_checked,omitempty"`
}

// ClassifyConflict 在客户端已解锁且远端密钥兼容的前提下，判定双端未删冲突是否内容等价
// （例如 AES-GCM 随机 nonce 重加密、裸 tar 与 tar.gz 压缩格式重压）。
// 对 ssh_host / ssh_keypair 永不判定等价。
func ClassifyConflict(detail provider.ConflictDetail, auth Auth) ConflictClassification {
	if detail.Local.Deleted || detail.Remote.Deleted {
		return ConflictClassification{}
	}
	if detail.Kind == provider.KindSSHHost || detail.Kind == provider.KindSSHKeypair {
		return ConflictClassification{}
	}
	if len(auth.Key) == 0 || !auth.RemoteKeyCompatible {
		return ConflictClassification{}
	}

	localPlain, err := DecryptEntryPayload(detail.Kind, detail.Grp, detail.Key, detail.Local.Ciphertext, Auth{
		Key: keyForLocal(auth), KeyEpoch: auth.KeyEpoch, RemoteKeyEpoch: auth.KeyEpoch, RemoteKeyCompatible: true,
	})
	if err != nil {
		return ConflictClassification{}
	}
	remotePlain, err := DecryptEntryPayload(detail.Kind, detail.Grp, detail.Key, detail.Remote.Ciphertext, auth)
	if err != nil {
		return ConflictClassification{}
	}

	if detail.Kind == provider.KindSkill {
		diff, err := CompareSkillArchives(localPlain, remotePlain)
		if err == nil && diff.ContentEquivalent {
			return ConflictClassification{
				Equivalent: true,
				Reason:     "归档文件内容一致（仅压缩格式或密文不同）",
			}
		}
		return ConflictClassification{}
	}

	if canonicalPlainEqual(detail.Kind, localPlain, remotePlain) {
		return ConflictClassification{
			Equivalent: true,
			Reason:     "解密后内容一致（仅密文不同）",
		}
	}
	return ConflictClassification{}
}

// CheckDeleteVsFormatOnly 在「本地删除 vs 远端更新」场景下，对比远端当前版本与本地删除前基线
// （history 中同 revision 的前像），判断远端是否仅做了格式重压/重加密而未改实际内容。
func CheckDeleteVsFormatOnly(detail provider.ConflictDetail, baseCiphertext []byte, auth Auth) (bool, string) {
	if !detail.Local.Deleted || detail.Remote.Deleted || len(baseCiphertext) == 0 {
		return false, ""
	}
	virtual := detail
	virtual.Local.Deleted = false
	virtual.Local.Ciphertext = baseCiphertext
	// 基线前像来自服务端历史，按 RemoteKeyEpoch 解密更稳妥
	baseAuth := auth
	baseAuth.KeyEpoch = auth.RemoteKeyEpoch
	cls := ClassifyConflict(virtual, baseAuth)
	if cls.Equivalent {
		return true, "远端相对本地删除前基线内容未变（仅格式重压/重加密），可安全选 local 推进删除"
	}
	return false, ""
}

func keyForLocal(auth Auth) []byte {
	return auth.Key
}

func canonicalPlainEqual(kind string, localPlain, remotePlain []byte) bool {
	if bytes.Equal(localPlain, remotePlain) {
		return true
	}
	switch kind {
	case provider.KindEnv:
		var a, b storage.EnvVarEntry
		if storage.FromJSON(localPlain, &a) == nil && storage.FromJSON(remotePlain, &b) == nil {
			return a.Value == b.Value && a.Description == b.Description
		}
	case provider.KindText, provider.KindBackup:
		var a, b storage.TextEntry
		if storage.FromJSON(localPlain, &a) == nil && storage.FromJSON(remotePlain, &b) == nil {
			return a.Value == b.Value && a.Description == b.Description
		}
	case provider.KindSkillIndex, provider.KindConfigIndex:
		var a, b any
		if json.Unmarshal(localPlain, &a) == nil && json.Unmarshal(remotePlain, &b) == nil {
			ca, _ := json.Marshal(a)
			cb, _ := json.Marshal(b)
			return bytes.Equal(ca, cb)
		}
	}
	return false
}
