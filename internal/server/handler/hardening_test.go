package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wii/senv/internal/server/store"
)

// --- 认证失败限速(5.3) ---

func TestAuthRateLimit(t *testing.T) {
	srv, token, _ := setup(t, Options{AuthRateLimit: 2})

	// 两次失败(阈值=2)后,同一来源被限速
	for i := 0; i < 2; i++ {
		rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: code = %d, want 401", i+1, rec.Code)
		}
	}
	rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("over-limit request = %d, want 429", rec.Code)
	}

	// 超限窗口内,有效 token 也被 429(按来源拦截)
	rec = doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("valid token from blocked source = %d, want 429", rec.Code)
	}

	// 429 响应不泄露账户/vault 信息
	if strings.Contains(rec.Body.String(), "vault") || strings.Contains(rec.Body.String(), "user") {
		t.Errorf("429 body must not leak existence info: %s", rec.Body.String())
	}
}

func TestAuthRateLimitDisabled(t *testing.T) {
	srv, _, _ := setup(t, Options{AuthRateLimit: -1})

	for i := 0; i < 40; i++ {
		rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("disabled limiter: request %d = %d, want 401", i+1, rec.Code)
		}
	}
}

func TestAuthRateLimitSuccessNotCounted(t *testing.T) {
	srv, token, _ := setup(t, Options{AuthRateLimit: 2})

	// 一次失败 + 多次成功:成功不计数,不应触发限速
	doRequest(t, srv, "GET", "/v1/vaults/main/metadata", "bogus", nil)
	for i := 0; i < 5; i++ {
		rec := doRequest(t, srv, "GET", "/v1/vaults/main/metadata", token, nil)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("successful auths must not be rate limited (request %d)", i+1)
		}
	}
}

// --- 请求体上限与 413(5.2) ---

func TestRequestBodyTooLarge413(t *testing.T) {
	srv, token, _ := setup(t, Options{MaxBodyBytes: 16})

	big := strings.Repeat("x", 64)
	body := map[string]any{
		"entries": []store.Entry{{Kind: "env", Grp: "default", Key: "K", Ciphertext: []byte(big)}},
	}
	rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", rec.Code)
	}
}

// --- 错误分级:校验类 400 带原因,内部错误 500 通用消息(5.4) ---

func TestPushValidationError400(t *testing.T) {
	srv, token, _ := setup(t)

	// 空批次
	rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{"entries": []store.Entry{}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "推送批次不能为空") {
		t.Errorf("empty batch = %d %q", rec.Code, rec.Body.String())
	}

	// 缺 kind
	rec = doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []store.Entry{{Grp: "default", Key: "K", Ciphertext: []byte("x")}},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "kind") {
		t.Errorf("missing kind = %d %q", rec.Code, rec.Body.String())
	}

	// 超长 key
	rec = doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []store.Entry{{Kind: "env", Grp: "default", Key: strings.Repeat("k", store.MaxKeyLen+1), Ciphertext: []byte("x")}},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "key") {
		t.Errorf("overlong key = %d %q", rec.Code, rec.Body.String())
	}

	// 边界值:恰好上限的 key 应通过(409/200 而非 400)
	rec = doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []store.Entry{{Kind: "env", Grp: "default", Key: strings.Repeat("k", store.MaxKeyLen), Ciphertext: []byte("x")}},
	})
	if rec.Code == http.StatusBadRequest {
		t.Errorf("key at limit must not be rejected as invalid: %d %q", rec.Code, rec.Body.String())
	}
}

func TestPushInternalErrorSanitized(t *testing.T) {
	srv, token, _ := setup(t)

	// 关闭连接池制造存储层内部错误,响应必须是通用消息,
	// 不含驱动/SQL 细节。
	srv.store.Pool().Close()

	rec := doRequest(t, srv, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []store.Entry{{Kind: "env", Grp: "default", Key: "K", Ciphertext: []byte("x")}},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store failure = %d, want 500", rec.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp["error"] != "internal error" {
		t.Errorf("error message = %q, want generic %q", resp["error"], "internal error")
	}
}

// --- metadata 校验错误(5.4/5.5 配套) ---

func TestPutMetadataValidationError(t *testing.T) {
	srv, token, _ := setup(t)

	// 空 blob → 400 带原因
	rec := doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", token, map[string]any{"blob": []byte{}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不能为空") {
		t.Errorf("empty blob = %d %q", rec.Code, rec.Body.String())
	}

	// 超大 blob → 400 带原因(而非内部错误)
	big := make([]byte, store.MaxMetadataBlobSize+1)
	rec = doRequest(t, srv, "PUT", "/v1/vaults/main/metadata", token, map[string]any{"blob": big})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "上限") {
		t.Errorf("oversized blob = %d %q", rec.Code, rec.Body.String())
	}
}
