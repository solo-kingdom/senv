// 通用 webhook 告警：检测器订阅安全事件流（recordAccess 是唯一出口），
// 命中规则后经 channel 进 goroutine 投递，绝不阻塞请求路径。
// 规则：连续 AUTH-FAILED 超阈值、client 被屏蔽、新 client 注册成功、
// client 换 IP 首次访问。未配置 webhook 时 detector 为 nil，零开销。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wii/senv/internal/server/store"
)

const (
	defaultAlertAuthFailThreshold = 10
	defaultAlertDebounce          = 5 * time.Minute
	// alertChanBuffer 检测队列上限：超限丢弃 + slog。告警是 best-effort，
	// 宁可丢告警不可拖慢请求路径
	alertChanBuffer = 256
)

// alertDetector 从安全事件流检测异常模式并投递 webhook
type alertDetector struct {
	store     store.Store // 仅用于名称解析与启动回放，不在请求路径调用
	webhook   string
	threshold int           // 同一来源连续 AUTH-FAILED 告警阈值
	debounce  time.Duration // 同一 (类型, 对象) 最小通知间隔

	ch chan store.AccessEvent

	mu         sync.Mutex
	failCounts map[string]int       // ip -> 连续 AUTH-FAILED 计数
	lastIP     map[int64]string     // client_id -> 最近一次来源 IP
	lastSent   map[string]time.Time // 去抖键 -> 上次通知时间
}

// newAlertDetector 创建检测器（调用方保证 webhook 非空）
func newAlertDetector(st store.Store, webhook string, threshold int, debounce time.Duration) *alertDetector {
	if threshold <= 0 {
		threshold = defaultAlertAuthFailThreshold
	}
	if debounce <= 0 {
		debounce = defaultAlertDebounce
	}
	return &alertDetector{
		store:      st,
		webhook:    webhook,
		threshold:  threshold,
		debounce:   debounce,
		ch:         make(chan store.AccessEvent, alertChanBuffer),
		failCounts: map[string]int{},
		lastIP:     map[int64]string{},
		lastSent:   map[string]time.Time{},
	}
}

// push 非阻塞投递事件；队列满丢弃并记日志（best-effort，见 alertChanBuffer）
func (d *alertDetector) push(e store.AccessEvent) {
	select {
	case d.ch <- e:
	default:
		slog.Warn("alert queue full, event dropped", "outcome", e.Outcome, "ip", e.IP)
	}
}

// run 消费事件流并检测；进程退出即终止，在途告警丢弃（best-effort 语义）
func (d *alertDetector) run(ctx context.Context) {
	d.loadLastIPs(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-d.ch:
			d.handle(ctx, e)
		}
	}
}

// loadLastIPs 启动时从 access_log 回放每个 client 的最近来源 IP，
// 避免重启后把「重启前已见过的 IP」误报为新 IP
func (d *alertDetector) loadLastIPs(ctx context.Context) {
	rows, err := d.store.ListAccessLogs(ctx, store.AccessLogFilter{Limit: 1000})
	if err != nil {
		slog.Warn("alert init: load last IPs failed", "err", err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range rows {
		if r.ClientID > 0 && d.lastIP[r.ClientID] == "" {
			d.lastIP[r.ClientID] = r.IP
		}
	}
}

// alertPayload 是 webhook POST 的 JSON 体；只含 access_log 已有元数据，
// 绝不包含 token、口令或密文
type alertPayload struct {
	Alert    string    `json:"alert"` // auth_fail_storm / client_blocked / client_registered / client_new_ip
	Time     time.Time `json:"time"`
	IP       string    `json:"ip"`
	UserID   int64     `json:"user_id,omitempty"`
	ClientID int64     `json:"client_id,omitempty"`
	UserName string    `json:"user,omitempty"`
	Client   string    `json:"client,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Count    int       `json:"count,omitempty"`
}

// handle 单事件检测：命中规则且过去抖则异步投递
func (d *alertDetector) handle(ctx context.Context, e store.AccessEvent) {
	var alertType, key string
	var count int

	switch {
	case e.Outcome == store.AccessOutcomeAuthFailed:
		d.mu.Lock()
		d.failCounts[e.IP]++
		n := d.failCounts[e.IP]
		d.mu.Unlock()
		if n < d.threshold {
			return
		}
		alertType, key, count = "auth_fail_storm", e.IP, n
	case e.Outcome == store.AccessOutcomeBlocked:
		alertType = "client_blocked"
		key = e.IP
	case e.Outcome == store.AccessOutcomeOK && strings.HasSuffix(e.Path, "/register") && e.ClientID > 0:
		alertType = "client_registered"
		key = clientKey(e.ClientID)
	default:
		// 任意非失败事件清零该来源的连续失败计数
		d.mu.Lock()
		delete(d.failCounts, e.IP)
		d.mu.Unlock()
	}

	// client 换 IP 检测（对带 client 身份的 OK/事件均适用）
	if e.ClientID > 0 {
		d.mu.Lock()
		last := d.lastIP[e.ClientID]
		if last != "" && last != e.IP {
			alertType, key = "client_new_ip", clientKey(e.ClientID)
		}
		d.lastIP[e.ClientID] = e.IP
		d.mu.Unlock()
	}

	if alertType == "" {
		return
	}
	if !d.debounced(alertType, key) {
		return
	}
	go d.deliver(ctx, alertPayload{
		Alert: alertType, Time: time.Now(), IP: e.IP,
		UserID: e.UserID, ClientID: e.ClientID,
		Reason: e.Reason, Count: count,
	})
}

func clientKey(id int64) string { return fmt.Sprintf("client:%d", id) }

// debounced 检查并更新去抖窗口；窗口内重复返回 false
func (d *alertDetector) debounced(alertType, key string) bool {
	k := alertType + "|" + key
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.lastSent[k]; ok && now.Sub(t) < d.debounce {
		return false
	}
	d.lastSent[k] = now
	return true
}

// deliver 投递 webhook：指数退避重试 ≤3 次后丢弃 + slog。独立 goroutine
// 运行，阻塞不传染检测循环。
func (d *alertDetector) deliver(ctx context.Context, p alertPayload) {
	p.UserName, p.Client = d.resolveNames(ctx, p.UserID, p.ClientID)
	body, err := json.Marshal(p)
	if err != nil {
		slog.Error("alert marshal failed", "err", err)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	backoff := time.Second
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhook, bytes.NewReader(body))
		if err != nil {
			slog.Error("alert request build failed", "err", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return
			}
			err = fmt.Errorf("webhook status %d", resp.StatusCode)
		}
		if attempt < 2 {
			slog.Warn("alert delivery failed, retrying", "alert", p.Alert, "attempt", attempt+1, "err", err)
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	slog.Error("alert delivery dropped after retries", "alert", p.Alert, "ip", p.IP)
}

// resolveNames 尽力解析用户/设备名（best-effort；解析失败留空）
func (d *alertDetector) resolveNames(ctx context.Context, userID, clientID int64) (string, string) {
	var userName, clientName string
	if userID > 0 {
		if rows, err := d.store.ListAccessLogs(ctx, store.AccessLogFilter{User: &userID, Limit: 1}); err == nil && len(rows) > 0 {
			userName = rows[0].UserName
		}
	}
	if clientID > 0 {
		if rows, err := d.store.ListAccessLogs(ctx, store.AccessLogFilter{Client: &clientID, Limit: 1}); err == nil && len(rows) > 0 {
			clientName = rows[0].ClientName
		}
	}
	return userName, clientName
}
