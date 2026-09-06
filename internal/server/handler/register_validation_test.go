// 注册端点输入校验：设备名控制字符拒绝（防终端转义注入管理控制台）。
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/server/store"
	"github.com/wii/senv/internal/server/testdb"
)

func TestRegisterNameControlCharsRejected(t *testing.T) {
	pool := testdb.New(t)
	st := store.New(pool)
	ctx := context.Background()
	userID := createUser(t, st, pool, "alice")
	code, err := st.CreateRegistrationCode(ctx, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	srv := New(st)

	cases := []struct {
		label, name string
	}{
		{"ESC 转义序列", "lap\x1b[31mtop"},
		{"换行", "laptop\nrm -rf"},
		{"NULL 字节", "lap\x00top"},
		{"C1 控制区", "lap\u009b31mtop"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			rec := doRequest(t, srv, "POST", "/v1/register", "",
				registerRequest{Code: code, Name: tc.name})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("register = %d: %s, want 400", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "控制字符") {
				t.Errorf("body = %q, want 控制字符提示", rec.Body.String())
			}
		})
	}

	// 被拒请求不消费注册码：合法名称仍可注册成功
	rec := doRequest(t, srv, "POST", "/v1/register", "",
		registerRequest{Code: code, Name: "clean-laptop"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register after rejections = %d: %s, want 201", rec.Code, rec.Body.String())
	}
	var resp registerResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Client.Name != "clean-laptop" || resp.Token == "" {
		t.Errorf("unexpected register response: %+v", resp)
	}
}
