// 热路径基准：无缓存 pgStore（before）与 cachedStore decorator（after）的
// 可比数字，记入 driver server-storage-cache 验证记录。两个基准各自起独立
// 容器（testcontainers）。运行：
//
//	go test -bench . -benchmem -run '^$' ./internal/server/store/
package store

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

// newBenchEnv 起临时 Postgres，建用户并推送 50 条 512B 密文条目；对 cached
// 预热一次 auth 与 pull（把 token 与 vault seq 装进缓存）。返回最新 revision。
func newBenchEnv(b *testing.B) (uncached, cached Store, token string, userID int64, latest int64) {
	b.Helper()
	pool, _ := testdb.NewWithDSN(b)
	ctx := context.Background()
	uncached = NewSQL(pool)
	cached = WithCache(NewSQL(pool))

	var err error
	token, err = uncached.CreateUser(ctx, "bench")
	if err != nil {
		b.Fatal(err)
	}
	userID, err = uncached.Authenticate(ctx, token)
	if err != nil {
		b.Fatal(err)
	}
	entries := make([]Entry, 0, 50)
	for i := 0; i < 50; i++ {
		entries = append(entries, Entry{
			Kind: "env", Grp: "default", Key: fmt.Sprintf("K%d", i),
			Ciphertext: bytes.Repeat([]byte("x"), 512),
		})
	}
	if _, latest, err = uncached.PushEntries(ctx, userID, "main", entries); err != nil {
		b.Fatal(err)
	}
	if _, err := cached.AuthenticateWithClient(ctx, token); err != nil {
		b.Fatal(err)
	}
	if _, _, err := cached.PullEntries(ctx, userID, "main", latest); err != nil {
		b.Fatal(err)
	}
	return uncached, cached, token, userID, latest
}

// BenchmarkAuthenticateWithClient：每请求固定开销的最大头（tokens LEFT JOIN clients）。
// cached 命中后全程无 SQL。
func BenchmarkAuthenticateWithClient(b *testing.B) {
	ctx := context.Background()
	uncached, cached, token, _, _ := newBenchEnv(b)
	b.Run("uncached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := uncached.AuthenticateWithClient(ctx, token); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := cached.AuthenticateWithClient(ctx, token); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkPullEntriesUpToDate：客户端已最新（client 端有 2s 节流且多数
// 轮询无变化）——uncached 走 lookupVault + seq 读 + 空范围查询 3 次往返，
// cached 走快捷判定全程无 SQL。
func BenchmarkPullEntriesUpToDate(b *testing.B) {
	ctx := context.Background()
	uncached, cached, _, userID, latest := newBenchEnv(b)
	b.Run("uncached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entries, got, err := uncached.PullEntries(ctx, userID, "main", latest)
			if err != nil || len(entries) != 0 || got != latest {
				b.Fatalf("entries=%d latest=%d err=%v", len(entries), got, err)
			}
		}
	})
	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			entries, got, err := cached.PullEntries(ctx, userID, "main", latest)
			if err != nil || len(entries) != 0 || got != latest {
				b.Fatalf("entries=%d latest=%d err=%v", len(entries), got, err)
			}
		}
	})
}
