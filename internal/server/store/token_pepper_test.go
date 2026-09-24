// token HMAC pepper 与旧 SHA-256 回退比对测试。
package store

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/wii/senv/internal/server/testdb"
)

func hmacHash(t *testing.T, pepper []byte, token string) []byte {
	t.Helper()
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}

func TestTokenPepper(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	s := NewSQL(pool)

	pepper := []byte("test-pepper")
	s.SetTokenPepper(pepper)

	token, err := s.CreateUser(ctx, "pepper-user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 认证通过（主路径）
	uid, err := s.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("authenticate with pepper: %v", err)
	}
	if uid == 0 {
		t.Fatal("uid = 0")
	}

	// 库中哈希是 HMAC，不是裸 SHA-256
	legacy := sha256.Sum256([]byte(token))
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM tokens`).Scan(&stored); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if bytes.Equal(stored, legacy[:]) {
		t.Fatal("库中哈希仍是裸 SHA-256，pepper 未生效")
	}
	if !bytes.Equal(stored, hmacHash(t, pepper, token)) {
		t.Fatal("库中哈希与 HMAC(pepper, token) 不符")
	}

	// AuthenticateWithClient / UserIDByToken / RevokeToken 全走 pepper 路径
	if _, err := s.AuthenticateWithClient(ctx, token); err != nil {
		t.Fatalf("AuthenticateWithClient: %v", err)
	}
	if _, err := s.UserIDByToken(ctx, token); err != nil {
		t.Fatalf("UserIDByToken: %v", err)
	}
	if err := s.RevokeToken(ctx, token); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := s.Authenticate(ctx, token); err == nil {
		t.Fatal("吊销后仍认证通过")
	}
}

func TestTokenPepperLegacyFallback(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	s := NewSQL(pool)

	// 旧时代：无 pepper 创建（库中存裸 SHA-256）
	token, err := s.CreateUser(ctx, "legacy-user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 启用 pepper：主路径未命中，回退比对旧哈希通过
	s.SetTokenPepper([]byte("new-pepper"))
	uid, err := s.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("legacy fallback authenticate: %v", err)
	}
	if uid == 0 {
		t.Fatal("uid = 0")
	}
	// 正缓存窗口内重复认证仍成功（语义不破坏）
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("legacy fallback repeat: %v", err)
	}
	if _, err := s.AuthenticateWithClient(ctx, token); err != nil {
		t.Fatalf("legacy fallback AuthenticateWithClient: %v", err)
	}
	// 吊销审计也可解析旧 token
	if _, err := s.UserIDByToken(ctx, token); err != nil {
		t.Fatalf("legacy UserIDByToken: %v", err)
	}

	// 无效 token 仍统一 ErrNotFound，不泄露存在性
	if _, err := s.Authenticate(ctx, "bogus-token"); err != ErrNotFound {
		t.Fatalf("无效 token err = %v, want ErrNotFound", err)
	}
}

func TestTokenNoPepperUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	s := NewSQL(pool) // 不设 pepper

	token, err := s.CreateUser(ctx, "plain-user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	legacy := sha256.Sum256([]byte(token))
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM tokens`).Scan(&stored); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if !bytes.Equal(stored, legacy[:]) {
		t.Fatal("无 pepper 时哈希应与原 SHA-256 逐字节一致")
	}
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
}
