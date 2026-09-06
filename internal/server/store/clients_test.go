package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/testdb"
)

// newTestUser 创建测试用户并返回 (userID, userToken)
func newTestUser(t *testing.T, s *Store, name string) (int64, string) {
	t.Helper()
	token, err := s.CreateUser(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", name, err)
	}
	id, err := s.UserIDByName(context.Background(), name)
	if err != nil {
		t.Fatalf("UserIDByName %s: %v", name, err)
	}
	return id, token
}

func TestRegisterClientSuccess(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	code, err := s.CreateRegistrationCode(ctx, userID, 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	token, client, err := s.RegisterClient(ctx, code, "my-laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if client.Name != "my-laptop" || client.Status != ClientStatusActive {
		t.Errorf("client = %+v, want name my-laptop status active", client)
	}

	// 注册签发的 token 可认证且归属 client
	res, err := s.AuthenticateWithClient(ctx, token)
	if err != nil {
		t.Fatalf("AuthenticateWithClient: %v", err)
	}
	if res.UserID != userID || res.ClientID != client.ID || res.ClientBlocked {
		t.Errorf("auth result = %+v, want user %d client %d not blocked", res, userID, client.ID)
	}

	// 注册码一次性：重放被拒
	if _, _, err := s.RegisterClient(ctx, code, "another"); !errors.Is(err, ErrNotFound) {
		t.Errorf("replayed code err = %v, want ErrNotFound", err)
	}
}

func TestRegisterClientInvalidCodes(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	// 未知注册码
	if _, _, err := s.RegisterClient(ctx, "no-such-code", "dev"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown code err = %v, want ErrNotFound", err)
	}

	// 过期注册码
	code, err := s.CreateRegistrationCode(ctx, userID, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, _, err := s.RegisterClient(ctx, code, "dev"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired code err = %v, want ErrNotFound", err)
	}
}

func TestRegisterClientNameConflictKeepsCode(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	code, err := s.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	if _, _, err := s.RegisterClient(ctx, code, "laptop"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// 新码注册同名 → 冲突
	code2, err := s.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	if _, _, err := s.RegisterClient(ctx, code2, "laptop"); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("name conflict err = %v, want ErrNameConflict", err)
	}
	// 冲突未消费注册码：换名可成功
	if _, _, err := s.RegisterClient(ctx, code2, "desktop"); err != nil {
		t.Fatalf("register with new name after conflict: %v", err)
	}
}

func TestSetClientStatusAndAuth(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")
	_, legacyToken := newTestUser(t, s, "bob")

	code, err := s.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	token, client, err := s.RegisterClient(ctx, code, "laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	// 屏蔽后：该 client 凭证解析结果标记 blocked，但 token 仍可解析（403 由 HTTP 层负责）
	if err := s.SetClientStatus(ctx, userID, "laptop", ClientStatusBlocked); err != nil {
		t.Fatalf("SetClientStatus block: %v", err)
	}
	res, err := s.AuthenticateWithClient(ctx, token)
	if err != nil {
		t.Fatalf("AuthenticateWithClient after block: %v", err)
	}
	if !res.ClientBlocked || res.ClientID != client.ID {
		t.Errorf("auth result = %+v, want blocked on client %d", res, client.ID)
	}

	// 解封恢复
	if err := s.SetClientStatus(ctx, userID, "laptop", ClientStatusActive); err != nil {
		t.Fatalf("SetClientStatus unblock: %v", err)
	}
	res, err = s.AuthenticateWithClient(ctx, token)
	if err != nil || res.ClientBlocked {
		t.Errorf("after unblock: res=%+v err=%v, want not blocked", res, err)
	}

	// 不存在的 client
	if err := s.SetClientStatus(ctx, userID, "ghost", ClientStatusBlocked); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing client err = %v, want ErrNotFound", err)
	}

	// 存量 user 级 token：client_id 为空，不受屏蔽影响
	res, err = s.AuthenticateWithClient(ctx, legacyToken)
	if err != nil || res.ClientID != 0 || res.ClientBlocked {
		t.Errorf("legacy token res = %+v err = %v, want client 0 not blocked", res, err)
	}
}

func TestListClients(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")
	otherID, _ := newTestUser(t, s, "bob")

	for _, name := range []string{"a", "b"} {
		code, err := s.CreateRegistrationCode(ctx, userID, time.Hour)
		if err != nil {
			t.Fatalf("CreateRegistrationCode: %v", err)
		}
		if _, _, err := s.RegisterClient(ctx, code, name); err != nil {
			t.Fatalf("RegisterClient %s: %v", name, err)
		}
	}
	code, err := s.CreateRegistrationCode(ctx, otherID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	if _, _, err := s.RegisterClient(ctx, code, "bob-dev"); err != nil {
		t.Fatalf("RegisterClient bob-dev: %v", err)
	}

	mine, err := s.ListClients(ctx, userID)
	if err != nil || len(mine) != 2 {
		t.Fatalf("ListClients(alice) = %v, %v; want 2 clients", mine, err)
	}
	all, err := s.ListClients(ctx, -1)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListClients(all) = %d clients, %v; want 3", len(all), err)
	}
}

func TestTouchClientThrottled(t *testing.T) {
	s := New(testdb.New(t))
	ctx := context.Background()
	userID, _ := newTestUser(t, s, "alice")

	code, _ := s.CreateRegistrationCode(ctx, userID, time.Hour)
	_, client, err := s.RegisterClient(ctx, code, "laptop")
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if client.LastSeenAt != nil {
		t.Fatalf("fresh client should have nil last_seen_at, got %v", client.LastSeenAt)
	}
	s.TouchClient(ctx, client.ID)
	clients, err := s.ListClients(ctx, userID)
	if err != nil || len(clients) != 1 {
		t.Fatalf("ListClients: %v, %v", clients, err)
	}
	if clients[0].LastSeenAt == nil {
		t.Errorf("last_seen_at should be set after TouchClient")
	}
}
