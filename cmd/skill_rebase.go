package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	renderconflict "github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/provider"
	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

var skillRebaseCmd = &cobra.Command{
	Use:   "rebase <id>",
	Short: "解决 skill 同版本号并发碰撞（保留远端版本，将本地改动顺延至下一版本号）",
	Long: `当两台设备同时对同一 skill 产生相同版本号（如 butler@v6）且内容不同时：
  1. 拉取远端冲突版本快照并保留到本地；
  2. 将本地新归档自动顺延到下一可用版本号（如 v7）并设为活跃版；
  3. 之后运行 senv sync 即可无碰撞推送。`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillRebase,
}

func init() {
	skillCmd.AddCommand(skillRebaseCmd)
}

func runSkillRebase(cmd *cobra.Command, args []string) error {
	id := args[0]
	if err := storage.ValidateSkillID(id); err != nil {
		return err
	}
	sp, err := getServerProvider()
	if err != nil {
		return err
	}
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return err
	}
	key, err := resolveKeyForAuth(auth)
	if err != nil {
		return err
	}
	defer session.ZeroKey(key)

	mgr, err := getSkillManager()
	if err != nil {
		return err
	}
	localDetail, err := mgr.Show(id)
	if err != nil {
		return err
	}
	localEntry := localDetail.Entry
	if localEntry.CurrentVersion == "" {
		return fmt.Errorf("skill %q 本地尚未版本化，无需 rebase", id)
	}
	collisionVer := localEntry.CurrentVersion
	group := localEntry.NormalizedGroup()

	ctx := commandContext(cmd)
	remoteEntries, _, err := sp.LookupEntries(ctx, []provider.EntryKey{
		{Kind: provider.KindSkillIndex},
		{Kind: provider.KindSkill, Grp: group, Key: storage.SkillVersionStorageKey(id, collisionVer)},
		{Kind: provider.KindSkill, Grp: group, Key: id},
	}, false)
	if err != nil {
		return err
	}

	var remoteIdxCipher, remoteVerCipher, remoteActiveCipher []byte
	var remoteVerEntry *provider.Entry
	for i := range remoteEntries {
		e := remoteEntries[i]
		if e.Deleted {
			continue
		}
		switch {
		case e.Kind == provider.KindSkillIndex:
			remoteIdxCipher = e.Ciphertext
		case e.Kind == provider.KindSkill && e.Grp == group && e.Key == storage.SkillVersionStorageKey(id, collisionVer):
			remoteVerCipher = e.Ciphertext
			copyEntry := e
			remoteVerEntry = &copyEntry
		case e.Kind == provider.KindSkill && e.Grp == group && e.Key == id:
			remoteActiveCipher = e.Ciphertext
		}
	}
	if len(remoteIdxCipher) == 0 {
		return fmt.Errorf("远端不存在 skill_index，无法检查版本碰撞")
	}

	cAuth := renderconflict.Auth{Key: key, RemoteKeyCompatible: true}
	if md, merr := auth.storage.LoadMetadata(); merr == nil && md != nil {
		cAuth.KeyEpoch = md.KeyEpoch
		cAuth.RemoteKeyEpoch = md.KeyEpoch
	}
	remoteIdxPlain, err := renderconflict.DecryptEntryPayload(provider.KindSkillIndex, "", "", remoteIdxCipher, cAuth)
	if err != nil {
		return fmt.Errorf("解密远端 skill_index 失败: %w", err)
	}
	var remoteIdx storage.SkillIndex
	if err := storage.FromJSON(remoteIdxPlain, &remoteIdx); err != nil {
		return fmt.Errorf("解析远端 skill_index 失败: %w", err)
	}
	remoteSkill, ok := remoteIdx.Skills[id]
	if !ok {
		return fmt.Errorf("远端 skill_index 中不存在 %q，无版本碰撞", id)
	}
	rv, hasRV := remoteSkill.FindVersion(collisionVer)
	lv, hasLV := localEntry.FindVersion(collisionVer)
	if !hasRV || !hasLV || rv.SHA256 == lv.SHA256 {
		return fmt.Errorf("skill %q 在 %s 版本无内容碰撞，无需 rebase", id, collisionVer)
	}

	targetCipher := remoteVerCipher
	targetKey := storage.SkillVersionStorageKey(id, collisionVer)
	if len(targetCipher) == 0 {
		targetCipher = remoteActiveCipher
		targetKey = id
	}
	if len(targetCipher) == 0 {
		return fmt.Errorf("远端缺少 %s@%s 归档密文", id, collisionVer)
	}
	remoteArchive, err := renderconflict.DecryptEntryPayload(provider.KindSkill, group, targetKey, targetCipher, cAuth)
	if err != nil {
		return fmt.Errorf("解密远端 %s@%s 失败: %w", id, collisionVer, err)
	}

	res, err := mgr.RebaseVersion(id, remoteSkill, collisionVer, remoteArchive)
	if err != nil {
		auditOp(session.AuditOpSkill, "skill:"+id, false, "rebase 失败")
		return err
	}
	if remoteVerEntry != nil {
		if err := sp.AdoptRemoteEntry(*remoteVerEntry); err != nil {
			return fmt.Errorf("同步远端快照 %s@%s 状态失败: %w", id, collisionVer, err)
		}
	}
	auditOp(session.AuditOpSkill, "skill:"+group+":"+id+"@"+res.RebasedVersion, true, "rebase")
	fmt.Fprintf(cmd.OutOrStdout(), "✓ 已变基 %s [%s]：保留远端 %s，本地版本顺延为 %s（运行 senv sync 推送）\n",
		res.ID, res.Group, res.CollisionVersion, res.RebasedVersion)
	return nil
}
