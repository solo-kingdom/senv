package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wii/senv/internal/backup"
	"github.com/wii/senv/internal/config"
	"github.com/wii/senv/internal/env"
	"github.com/wii/senv/internal/llm"
	"github.com/wii/senv/internal/mcp"
	"github.com/wii/senv/internal/search"
	"github.com/wii/senv/internal/ssh"
	"github.com/wii/senv/internal/text"
)

var (
	searchKinds []string
	searchLimit int
	searchJSON  bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Fuzzy-search vault metadata (no values)",
	Long: `Search across env, text, backup, config, SSH hosts, LLM providers and MCP
server profiles. Matching uses identifiers and descriptions only — values,
private keys, credentials and MCP env values are never matched or returned.

Query is required. Results are capped by --limit (default 20).`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoPull(cmd, refreshRequested(cmd))
		query := strings.Join(args, " ")
		kinds, err := search.ParseKinds(searchKinds)
		if err != nil {
			return err
		}
		src, err := searchSources()
		if err != nil {
			return err
		}
		return runSearch(cmd.OutOrStdout(), src, query, kinds, searchLimit, searchJSON)
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().StringSliceVar(&searchKinds, "kind", nil, "restrict kinds (env,text,backup,config,ssh,llm,mcp; repeatable or comma-separated)")
	searchCmd.Flags().IntVar(&searchLimit, "limit", search.DefaultLimit, "max hits to return")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "emit JSON (metadata only)")
	addRefreshFlag(searchCmd)
}

func searchSources() (search.Sources, error) {
	auth, err := resolveAuth(getConfigPath(), getDataPath(), authPrompt)
	if err != nil {
		return search.Sources{}, err
	}
	store := auth.storage
	if auth.hasKey() {
		return search.Sources{
			Env:    env.NewManagerWithKey(store, auth.key),
			Text:   text.NewManagerWithKey(store, auth.key),
			Backup: backup.NewManagerWithKey(store, auth.key),
			Config: config.NewManagerWithKey(store, auth.key),
			SSH:    ssh.NewManagerWithKey(store, auth.key),
			LLM:    llm.NewProviderManagerWithKey(store, auth.key),
			MCP:    mcp.NewManagerWithKey(store, auth.key),
		}, nil
	}
	return search.Sources{
		Env:    env.NewManager(store, auth.password),
		Text:   text.NewManager(store, auth.password),
		Backup: backup.NewManager(store, auth.password),
		Config: config.NewManager(store, auth.password),
		SSH:    ssh.NewManager(store, auth.password),
		LLM:    llm.NewProviderManager(store, auth.password),
		MCP:    mcp.NewManager(store, auth.password),
	}, nil
}

func runSearch(out io.Writer, src search.Sources, query string, kinds []search.Kind, limit int, asJSON bool) error {
	res, err := search.Run(search.Gather(src), query, kinds, limit)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if len(res.Hits) == 0 {
		fmt.Fprintln(out, "no matches")
		return nil
	}
	for _, h := range res.Hits {
		line := formatSearchHit(h)
		fmt.Fprintln(out, line)
	}
	if res.Truncated {
		fmt.Fprintf(out, "(showing %d of %d; raise --limit)\n", len(res.Hits), res.Total)
	}
	return nil
}

func formatSearchHit(h search.Item) string {
	id := h.Key
	if h.Group != "" {
		id = h.Group + "/" + h.Key
	}
	parts := []string{string(h.Kind), id}
	if h.Description != "" {
		parts = append(parts, h.Description)
	} else if h.Preview != "" {
		parts = append(parts, h.Preview)
	}
	return strings.Join(parts, "  ")
}
