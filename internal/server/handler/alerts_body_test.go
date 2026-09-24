// 告警请求体模板与投递结果判定测试：飞书这类网关要求自家 body 结构，
// 且 HTTP 200 也可能带业务错误码——两者都必须可验证（2026-09-24 公网加固）。
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// rawRecorder 捕获原始请求体，并可预设响应状态码与响应体
type rawRecorder struct {
	status   int
	respBody string

	mu     sync.Mutex
	bodies [][]byte
}

func (r *rawRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.bodies = append(r.bodies, b)
	r.mu.Unlock()
	st := r.status
	if st == 0 {
		st = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(st)
	_, _ = io.WriteString(w, r.respBody)
}

func (r *rawRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func (r *rawRecorder) waitCount(t *testing.T, want int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.bodies)
		bodies := append([][]byte(nil), r.bodies...)
		r.mu.Unlock()
		if n >= want {
			return bodies
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 webhook 收到 %d 次请求超时，实收 %d 次", want, r.count())
	return nil
}

// fireAuthFails 打 n 次坏 token，触发 auth_fail_storm（阈值同为 n）
func fireAuthFails(s *Server, n int) {
	for i := 0; i < n; i++ {
		req := httptest.NewRequest("GET", "/v1/vaults/main/entries", nil)
		req.Header.Set("Authorization", "Bearer bogus")
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func TestAlertBodyTemplateRendersProviderShape(t *testing.T) {
	rec := &rawRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	const tmpl = `{"msg_type":"text","content":{"text":"通知｜senv-server 告警 {{.alert}}｜ip={{.ip}} user={{.user}} client={{.client}} reason={{.reason}} count={{.count}}"}}`
	s := New(newFakeStore(), Options{
		AlertWebhook:           srv.URL,
		AlertAuthFailThreshold: 2,
		AlertDebounce:          time.Hour,
		AlertBodyTemplate:      tmpl,
	})
	fireAuthFails(s, 2)

	bodies := rec.waitCount(t, 1)
	var msg struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(bodies[0], &msg); err != nil {
		t.Fatalf("渲染结果不是合法 JSON: %v body=%s", err, bodies[0])
	}
	if msg.MsgType != "text" {
		t.Errorf("msg_type: got %q, want text", msg.MsgType)
	}
	for _, want := range []string{"通知", "auth_fail_storm", "count=2"} {
		if !strings.Contains(msg.Content.Text, want) {
			t.Errorf("正文缺 %q: %s", want, msg.Content.Text)
		}
	}
	// 认证失败没有 user/client/reason，补齐键必须渲染成空，不能出现 <no value>
	if strings.Contains(msg.Content.Text, "<no value>") {
		t.Errorf("正文残留 <no value>: %s", msg.Content.Text)
	}
}

func TestAlertBodyTemplateEmptyKeepsJSONPayload(t *testing.T) {
	rec := &rawRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	s := New(newFakeStore(), Options{
		AlertWebhook:           srv.URL,
		AlertAuthFailThreshold: 2,
		AlertDebounce:          time.Hour,
	})
	fireAuthFails(s, 2)

	bodies := rec.waitCount(t, 1)
	var p alertPayload
	if err := json.Unmarshal(bodies[0], &p); err != nil {
		t.Fatalf("空模板应保持内置 JSON 载荷: %v body=%s", err, bodies[0])
	}
	if p.Alert != "auth_fail_storm" {
		t.Errorf("alert: got %q", p.Alert)
	}
}

// 模板写错键名 = 操作者配置错误，必须不投递（宁可无告警，不出脏消息）
func TestAlertBodyTemplateUnknownKeyDropsDelivery(t *testing.T) {
	rec := &rawRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	s := New(newFakeStore(), Options{
		AlertWebhook:           srv.URL,
		AlertAuthFailThreshold: 2,
		AlertDebounce:          time.Hour,
		AlertBodyTemplate:      `{"text":"{{.nosuchkey}}"}`,
	})
	fireAuthFails(s, 2)

	time.Sleep(300 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Errorf("模板引用未知键应渲染失败，实投递 %d 次", n)
	}
}

// 语法错模板：回落默认 JSON 载荷（服务不因配置错误失去告警能力）
func TestAlertBodyTemplateSyntaxErrorFallsBack(t *testing.T) {
	rec := &rawRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	s := New(newFakeStore(), Options{
		AlertWebhook:           srv.URL,
		AlertAuthFailThreshold: 2,
		AlertDebounce:          time.Hour,
		AlertBodyTemplate:      `{"text":"{{.alert}`,
	})
	fireAuthFails(s, 2)

	bodies := rec.waitCount(t, 1)
	var p alertPayload
	if err := json.Unmarshal(bodies[0], &p); err != nil {
		t.Fatalf("模板解析失败应回落 JSON 载荷: %v body=%s", err, bodies[0])
	}
}

// 网关 200 + 业务错误码 = 投递失败：飞书关键词不命中实测回
// {"code":19024,"msg":"Key Words Not Found"}，只判状态码会静默丢告警
func TestWebhookResultErrorEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"200 空 body（自建网关）", 200, "", false},
		{"200 非 JSON body", 200, "ok", false},
		{"200 code=0 飞书成功", 200, `{"code":0,"msg":"success","data":{}}`, false},
		{"200 code=19024 关键词不命中", 200, `{"code":19024,"msg":"Key Words Not Found","data":{}}`, true},
		{"200 errcode=0 企业微信成功", 200, `{"errcode":0,"errmsg":"ok"}`, false},
		{"200 errcode 字符串非 0", 200, `{"errcode":"40004","errmsg":"param invalid"}`, true},
		{"200 error_code=1 钉钉", 200, `{"error_code":1}`, true},
		{"200 无错误码字段的对象", 200, `{"ok":true}`, false},
		{"500", 500, `{"code":0}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := webhookResultError(c.status, []byte(c.body))
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", c.wantErr, err)
			}
		})
	}
}

// 端到端：网关 200 + code!=0 必须重试后记失败，而不是当成功
func TestAlertDeliveryFailsOnRejectedEnvelope(t *testing.T) {
	rec := &rawRecorder{status: http.StatusOK, respBody: `{"code":19024,"msg":"Key Words Not Found"}`}
	srv := httptest.NewServer(rec)
	defer srv.Close()

	s := New(newFakeStore(), Options{
		AlertWebhook:           srv.URL,
		AlertAuthFailThreshold: 2,
		AlertDebounce:          time.Hour,
		AlertBodyTemplate:      `{"msg_type":"text","content":{"text":"通知 {{.alert}}"}}`,
	})
	fireAuthFails(s, 2)

	// 3 次尝试（1 + 2 重试，退避 1s + 2s）后放弃
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && rec.count() < 3 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := rec.count(); n != 3 {
		t.Fatalf("业务错误码应触发重试: got %d 次请求, want 3", n)
	}
}
