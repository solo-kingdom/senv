package cmd

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wii/senv/internal/llm"
)

// catalogCachePath 返回模型目录缓存路径。缓存是机器本地的公开数据，
// 放 config 目录而非 vault data 目录，不随同步分发。
func catalogCachePath() string {
	return filepath.Join(getConfigPath(), "cache", "models-dev.json")
}

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "Manage the LLM provider catalog and agent switching",
	Long: `Manage LLM providers and switch coding agents between them.

The ai command group works on public catalog data and does not require
unlocking the vault.`,
	Args: cobra.NoArgs,
}

var aiRefreshSource string

var aiRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh the LLM provider/model catalog cache",
	Long: `Fetch the provider/model catalog from models.dev, validate it, and
replace the local cache atomically. On network or validation failure the old
cache is kept untouched and the command exits non-zero.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		source := aiRefreshSource
		if source == "" {
			source = llm.DefaultCatalogURL
		}
		cat, err := llm.Fetch(source, nil)
		if err != nil {
			return err
		}
		providers, models, err := cat.Counts()
		if err != nil {
			return err
		}
		// 先拉取并校验、后落盘，保证失败时旧缓存原样保留。
		if err := llm.Save(catalogCachePath(), cat); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "已刷新模型目录：%d 个 provider，%d 个 model（来源 %s）\n",
			providers, models, cat.Source)
		return nil
	},
}

var aiCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Inspect the cached model catalog",
	Args:  cobra.NoArgs,
}

var aiCatalogStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cached model catalog metadata (offline)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cat, err := llm.Load(catalogCachePath())
		if errors.Is(err, llm.ErrCacheNotFound) {
			return fmt.Errorf("本地暂无模型目录缓存，请先执行 senv ai refresh")
		}
		if err != nil {
			return fmt.Errorf("模型目录缓存损坏（%v）；请重新执行 senv ai refresh", err)
		}
		providers, models, err := cat.Counts()
		if err != nil {
			return fmt.Errorf("模型目录缓存损坏（%v）；请重新执行 senv ai refresh", err)
		}
		fetchedAt, err := cat.FetchedAtTime()
		if err != nil {
			return fmt.Errorf("模型目录缓存损坏（%v）；请重新执行 senv ai refresh", err)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "拉取时间：%s\n", fetchedAt.Local().Format("2006-01-02 15:04:05 MST"))
		fmt.Fprintf(out, "目录源：%s\n", cat.Source)
		fmt.Fprintf(out, "provider 数：%d\n", providers)
		fmt.Fprintf(out, "model 数：%d\n", models)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(aiCmd)
	aiRefreshCmd.Flags().StringVar(&aiRefreshSource, "source", "",
		"catalog source URL (default "+llm.DefaultCatalogURL+")")
	aiCmd.AddCommand(aiRefreshCmd)
	aiCmd.AddCommand(aiCatalogCmd)
	aiCatalogCmd.AddCommand(aiCatalogStatusCmd)
}
