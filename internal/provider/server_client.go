package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wii/senv/internal/perflog"
)

// Entry 是一条同步条目（与 senv-server v1 API 的 wire 格式一致）。
// ciphertext 始终是不透明密文字节；CLI 侧不依赖 server 的 pgx 类型。
type Entry struct {
	Kind             string    `json:"kind"`
	Grp              string    `json:"grp"`
	Key              string    `json:"key"`
	Ciphertext       []byte    `json:"ciphertext,omitempty"`
	CiphertextSHA256 string    `json:"ciphertext_sha256,omitempty"`
	Size             int64     `json:"size,omitempty"`
	BaseRevision     int64     `json:"base_revision,omitempty"`
	Revision         int64     `json:"revision"`
	Deleted          bool      `json:"deleted"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

// EntryKey 指定按需查询的条目三元组。
type EntryKey struct {
	Kind string `json:"kind"`
	Grp  string `json:"grp"`
	Key  string `json:"key"`
}

// Conflict 描述 server 返回的单条乐观锁冲突
type Conflict struct {
	Kind             string    `json:"kind"`
	Grp              string    `json:"grp"`
	Key              string    `json:"key"`
	CurrentRevision  int64     `json:"current_revision"`
	Deleted          bool      `json:"deleted"`
	Size             int64     `json:"size"`
	UpdatedAt        time.Time `json:"updated_at"`
	CiphertextSHA256 string    `json:"ciphertext_sha256,omitempty"`
}

// ConflictError 表示推送被 server 乐观锁拒绝（409）；包含冲突清单
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("revision conflict on %d entries", len(e.Conflicts))
}

// ErrVaultNotFound 表示 server 端 vault 不存在（404）
var ErrVaultNotFound = errors.New("vault not found on server")

// ErrLookupUnsupported 表示老版本 server 不支持 /entries/lookup 端点（404/405）
var ErrLookupUnsupported = errors.New("server does not support entry lookup")

// ErrServerTimeout 表示服务端请求或流式读取超时（区别于连接不可达）
var ErrServerTimeout = errors.New("server request timeout")

// ErrServerUnreachable 表示无法建立连接或网络中断
var ErrServerUnreachable = errors.New("server unreachable")

// ServerRequestError 描述分层超时或传输失败的准确阶段与原因。
type ServerRequestError struct {
	Kind       string // "timeout" | "unreachable"
	Op         string
	Stage      string // "budget" | "connect_or_header" | "body_read" | "transport"
	StatusCode int
	Duration   time.Duration
	Err        error
}

func (e *ServerRequestError) Error() string {
	if e.Kind == "timeout" {
		return fmt.Sprintf("server 请求超时（阶段=%s, op=%s）: %v", e.Stage, e.Op, e.Err)
	}
	return fmt.Sprintf("无法连接 server（阶段=%s, op=%s）: %v", e.Stage, e.Op, e.Err)
}

func (e *ServerRequestError) Unwrap() error { return e.Err }

func (e *ServerRequestError) Is(target error) bool {
	if target == ErrServerTimeout {
		return e.Kind == "timeout"
	}
	if target == ErrServerUnreachable {
		return e.Kind == "unreachable"
	}
	return false
}

// IsTimeout 判断错误是否由网络/请求超时引起（含 context.DeadlineExceeded）。
func IsTimeout(err error) bool {
	if errors.Is(err, ErrServerTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// IsUnreachable 判断错误是否为连接不可达（非超时、非 HTTP 4xx/5xx、非冲突）。
func IsUnreachable(err error) bool {
	return errors.Is(err, ErrServerUnreachable)
}

// ErrClientBlocked 表示 server 判定本 client 已被屏蔽（403 + client_blocked）。
// 收到该错误后 provider 会清空本地解锁缓存（加密数据保留），调用方应以
// 非零码退出并给出重新注册指引。
var ErrClientBlocked = errors.New("此 client 已被服务器屏蔽")

// BlockedGuidance 是屏蔽后给用户的操作指引（附在错误消息之后输出）
const BlockedGuidance = "已清除本机解锁缓存，本地加密数据保留。\n  解封后可直接继续使用；或重新注册: senv server register --address <server> --code <注册码> --name <设备名>"

// serverAPI 是同步引擎依赖的 server 接口（单测可用内存 fake）
type serverAPI interface {
	GetMetadata(ctx context.Context, vault string) ([]byte, error)
	PutMetadata(ctx context.Context, vault string, blob []byte) error
	// PutMetadataIfMatch 带乐观锁写入 metadata：baseHash 非空时服务端校验当前值，
	// 不一致返回 409（metadata 期间被其它设备更新）。旧 server 忽略该字段。
	PutMetadataIfMatch(ctx context.Context, vault string, blob []byte, baseHash string) error
	Push(ctx context.Context, vault string, entries []Entry) ([]Entry, int64, error)
	Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error)
}

// lookupAPI 是新服务端提供的按 key 批量查询窄接口（旧 server/fake 未实现时自动回退 Pull(0)）
type lookupAPI interface {
	Lookup(ctx context.Context, vault string, keys []EntryKey, metaOnly bool) ([]Entry, int64, error)
}

// pullWithMetaAPI 支持跳过全量 tombstone 并返回服务端 metadata_hash（旧 server/fake 无感）
type pullWithMetaAPI interface {
	PullWithMeta(ctx context.Context, vault string, since int64, includeDeleted bool) ([]Entry, int64, string, error)
}

// serverClient 是 senv-server v1 API 的 HTTP client
type serverClient struct {
	baseURL     string
	token       string
	http        *http.Client
	readTimeout time.Duration
	// onBlocked 在首次收到 403 client_blocked 时触发（每实例一次），
	// 由 provider 注入用于清空本地解锁缓存；fake 实现不含此路径。
	onBlocked   func()
	blockedOnce sync.Once
}

const (
	// maxServerResponseBytes 限制单次成功响应体的最大解压字节数（覆盖服务端
	// 默认 64 MiB 批量同步上限，防止被攻陷服务端或 MITM 用 gzip 炸弹打爆内存）。
	maxServerResponseBytes int64 = 64 << 20
	// maxServerErrorBodyBytes 限制错误响应体解码上限。
	maxServerErrorBodyBytes int64 = 64 << 10
	// defaultServerReadTimeout 是等待服务端响应头及响应体活跃读无进展的默认超时。
	defaultServerReadTimeout = 30 * time.Second
	defaultPullPageLimit     = 500
	defaultPullPageMaxBytes  = 2 << 20
)

func parseServerReadTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SENV_SERVER_TIMEOUT"))
	if raw == "" {
		return defaultServerReadTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultServerReadTimeout
}

func newServerClient(baseURL, token string) *serverClient {
	return newServerClientWithTimeout(baseURL, token, parseServerReadTimeout())
}

func newServerClientWithTimeout(baseURL, token string, readTimeout time.Duration) *serverClient {
	if readTimeout <= 0 {
		readTimeout = defaultServerReadTimeout
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: readTimeout,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}
	return &serverClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		token:       token,
		readTimeout: readTimeout,
		http: &http.Client{
			Timeout:   0, // 使用分层超时（建连/Header/活跃读 stall + 调用方 ctx），避免大包总时长误杀
			Transport: tr,
			// 同步 API 不应跟随任何 HTTP 重定向，防止 HTTPS->HTTP 降级或子域重定向
			// 导致 net/http 自动携带 Authorization: Bearer 凭据泄露。
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type stallTimeoutReader struct {
	r        io.ReadCloser
	timeout  time.Duration
	timedOut atomic.Bool
}

func (s *stallTimeoutReader) Read(p []byte) (int, error) {
	if s.timeout <= 0 {
		return s.r.Read(p)
	}
	timer := time.AfterFunc(s.timeout, func() {
		s.timedOut.Store(true)
		_ = s.r.Close()
	})
	n, err := s.r.Read(p)
	timer.Stop()
	if s.timedOut.Load() && err != nil {
		return n, fmt.Errorf("%w: no body progress for %s", context.DeadlineExceeded, s.timeout)
	}
	return n, err
}

func (s *stallTimeoutReader) Close() error {
	return s.r.Close()
}

// decodeBoundedJSON 从受限 reader 读取完整响应体并反序列化；若超出 limit 明确报错，
// 防止超大响应或压缩炸弹耗尽客户端内存。
func decodeBoundedJSON(r io.Reader, limit int64, out any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("server 响应体超过上限（%d 字节）", limit)
	}
	return json.Unmarshal(data, out)
}

type requestMetrics struct {
	dnsMs    int64
	connMs   int64
	tlsMs    int64
	ttfbMs   int64
	bodyMs   int64
	bytesIn  int64
	status   int
	encoding string
}

// do 发起认证请求并统一错误解析（401/404/409/其他），附耗时日志与分阶段网络维度
// （dns_ms/conn_ms/tls_ms/ttfb_ms/body_ms/bytes_in/status/encoding/err_kind）。
func (c *serverClient) do(ctx context.Context, method, path string, body any, out any) error {
	st := perflog.Start("net.request").With("op", method+" "+path)
	var connsNew int
	var m requestMetrics
	var traceMu sync.Mutex
	var dnsStart, connStart, tlsStart, wroteReq time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			traceMu.Lock()
			dnsStart = time.Now()
			traceMu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			traceMu.Lock()
			if !dnsStart.IsZero() {
				m.dnsMs = time.Since(dnsStart).Milliseconds()
			}
			traceMu.Unlock()
		},
		ConnectStart: func(string, string) {
			traceMu.Lock()
			connStart = time.Now()
			traceMu.Unlock()
		},
		ConnectDone: func(string, string, error) {
			traceMu.Lock()
			if !connStart.IsZero() {
				m.connMs = time.Since(connStart).Milliseconds()
			}
			traceMu.Unlock()
		},
		TLSHandshakeStart: func() {
			traceMu.Lock()
			tlsStart = time.Now()
			traceMu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			traceMu.Lock()
			if !tlsStart.IsZero() {
				m.tlsMs = time.Since(tlsStart).Milliseconds()
			}
			traceMu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			if !info.Reused {
				traceMu.Lock()
				connsNew = 1
				traceMu.Unlock()
			}
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			traceMu.Lock()
			wroteReq = time.Now()
			traceMu.Unlock()
		},
		GotFirstResponseByte: func() {
			traceMu.Lock()
			if !wroteReq.IsZero() {
				m.ttfbMs = time.Since(wroteReq).Milliseconds()
			}
			traceMu.Unlock()
		},
	}
	err := c.doOnce(httptrace.WithClientTrace(ctx, trace), method, path, body, out, &m)
	errKind := ""
	if err != nil {
		var reqErr *ServerRequestError
		if errors.As(err, &reqErr) {
			errKind = string(reqErr.Kind)
		} else {
			errKind = "other"
		}
	}
	st.With(
		"conns_new", connsNew,
		"dns_ms", m.dnsMs,
		"conn_ms", m.connMs,
		"tls_ms", m.tlsMs,
		"ttfb_ms", m.ttfbMs,
		"body_ms", m.bodyMs,
		"bytes_in", m.bytesIn,
		"status", m.status,
		"encoding", m.encoding,
		"err_kind", errKind,
	)
	st.EndErr(err)
	return err
}

func (c *serverClient) doOnce(ctx context.Context, method, path string, body any, out any, metrics *requestMetrics) error {
	start := time.Now()
	op := method + " " + path
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return classifyTransportErr(ctx, op, "connect_or_header", time.Since(start), err)
	}
	bodyStart := time.Now()
	bodyReader := &stallTimeoutReader{r: resp.Body, timeout: c.readTimeout}
	defer bodyReader.Close()
	if metrics != nil {
		metrics.status = resp.StatusCode
		metrics.encoding = resp.Header.Get("Content-Encoding")
		if resp.ContentLength > 0 {
			metrics.bytesIn = resp.ContentLength
		}
		defer func() {
			metrics.bodyMs = time.Since(bodyStart).Milliseconds()
		}()
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out == nil {
			return nil
		}
		if err := decodeBoundedJSON(bodyReader, maxServerResponseBytes, out); err != nil {
			if bodyReader.timedOut.Load() || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return classifyTransportErr(ctx, op, "body_read", time.Since(start), err)
			}
			return err
		}
		return nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("server 返回重定向（%d），已拒绝跟随以保护认证凭据", resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("server 认证失败（401）：token 无效或已吊销")
	case resp.StatusCode == http.StatusForbidden:
		var body struct {
			Error string `json:"error"`
		}
		_ = decodeBoundedJSON(bodyReader, maxServerErrorBodyBytes, &body)
		if body.Error == "client_blocked" {
			// 仅机器可读的屏蔽语义触发本地清理；其余 403 按普通错误处理，
			// 防御旧 server 或未知语义导致误清解锁缓存
			c.blockedOnce.Do(func() {
				if c.onBlocked != nil {
					c.onBlocked()
				}
			})
			return fmt.Errorf("%w\n%s", ErrClientBlocked, BlockedGuidance)
		}
		return fmt.Errorf("server 错误: %s", body.Error)
	case resp.StatusCode == http.StatusNotFound:
		raw, _ := io.ReadAll(io.LimitReader(bodyReader, maxServerErrorBodyBytes))
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errBody) == nil && errBody.Error != "" {
			return ErrVaultNotFound
		}
		// 路由级 404（如旧版 server 无 /entries/lookup）返回纯文本 404 page not found
		if strings.HasSuffix(path, "/entries/lookup") {
			return ErrLookupUnsupported
		}
		return ErrVaultNotFound
	case resp.StatusCode == http.StatusMethodNotAllowed:
		if strings.HasSuffix(path, "/entries/lookup") {
			return ErrLookupUnsupported
		}
		return fmt.Errorf("server 错误: %s", resp.Status)
	case resp.StatusCode == http.StatusConflict:
		var body struct {
			Error     string     `json:"error"`
			Conflicts []Conflict `json:"conflicts"`
		}
		if err := decodeBoundedJSON(bodyReader, maxServerResponseBytes, &body); err != nil {
			return fmt.Errorf("推送冲突，但无法解析冲突清单: %w", err)
		}
		if len(body.Conflicts) == 0 {
			// 非条目推送端点的 409（如注册设备名冲突）：透出 server 消息
			if body.Error == "" {
				body.Error = resp.Status
			}
			return fmt.Errorf("server 错误: %s", body.Error)
		}
		return &ConflictError{Conflicts: body.Conflicts}
	default:
		var body struct {
			Error string `json:"error"`
		}
		_ = decodeBoundedJSON(bodyReader, maxServerErrorBodyBytes, &body)
		if body.Error == "" {
			body.Error = resp.Status
		}
		return fmt.Errorf("server 错误: %s", body.Error)
	}
}

func classifyTransportErr(ctx context.Context, op, stage string, dur time.Duration, err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && stage == "connect_or_header" {
			stage = "budget"
		}
		return &ServerRequestError{Kind: "timeout", Op: op, Stage: stage, Duration: dur, Err: err}
	}
	return &ServerRequestError{Kind: "unreachable", Op: op, Stage: stage, Duration: dur, Err: err}
}

func (c *serverClient) GetMetadata(ctx context.Context, vault string) ([]byte, error) {
	var resp struct {
		Blob []byte `json:"blob"`
	}
	if err := c.do(ctx, "GET", "/v1/vaults/"+vault+"/metadata", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Blob, nil
}

func (c *serverClient) PutMetadata(ctx context.Context, vault string, blob []byte) error {
	return c.do(ctx, "PUT", "/v1/vaults/"+vault+"/metadata", map[string][]byte{"blob": blob}, nil)
}

// PutMetadataIfMatch 带 base_hash 写入 metadata；服务端不一致时返回 409。
func (c *serverClient) PutMetadataIfMatch(ctx context.Context, vault string, blob []byte, baseHash string) error {
	body := map[string]any{"blob": blob}
	if baseHash != "" {
		body["base_hash"] = baseHash
	}
	return c.do(ctx, "PUT", "/v1/vaults/"+vault+"/metadata", body, nil)
}

// MetadataHistory 拉取 metadata 历史版本（新到旧）
func (c *serverClient) MetadataHistory(ctx context.Context, vault string, limit int) ([]MetadataVersion, error) {
	var resp struct {
		History []MetadataVersion `json:"history"`
	}
	path := fmt.Sprintf("/v1/vaults/%s/metadata/history?limit=%d", vault, limit)
	if err := c.do(ctx, "GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.History, nil
}

func (c *serverClient) Push(ctx context.Context, vault string, entries []Entry) ([]Entry, int64, error) {
	var resp struct {
		Revisions []struct {
			Kind     string `json:"kind"`
			Grp      string `json:"grp"`
			Key      string `json:"key"`
			Revision int64  `json:"revision"`
		} `json:"revisions"`
		LatestRevision int64 `json:"latest_revision"`
	}
	if err := c.do(ctx, "POST", "/v1/vaults/"+vault+"/entries", map[string]any{"entries": entries}, &resp); err != nil {
		return nil, 0, err
	}
	// 把 ack 的 revision 回填到对应条目
	ack := make(map[string]int64, len(resp.Revisions))
	for _, r := range resp.Revisions {
		ack[entryID(r.Kind, r.Grp, r.Key)] = r.Revision
	}
	for i := range entries {
		entries[i].Revision = ack[entryID(entries[i].Kind, entries[i].Grp, entries[i].Key)]
	}
	return entries, resp.LatestRevision, nil
}

func (c *serverClient) Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error) {
	entries, latest, _, err := c.PullWithMeta(ctx, vault, since, true)
	return entries, latest, err
}

// PullWithMeta 分页拉取增量条目并顺带获取远端 metadata_hash；老 server 不返 has_more 时单次返回。
func (c *serverClient) PullWithMeta(ctx context.Context, vault string, since int64, includeDeleted bool) ([]Entry, int64, string, error) {
	cur := since
	var all []Entry
	var latest int64
	var metaHash string
	for {
		var resp struct {
			Entries        []Entry `json:"entries"`
			LatestRevision int64   `json:"latest_revision"`
			HasMore        bool    `json:"has_more"`
			NextSince      int64   `json:"next_since"`
			MetadataHash   string  `json:"metadata_hash"`
		}
		path := fmt.Sprintf("/v1/vaults/%s/entries?since=%d&limit=%d&max_bytes=%d",
			vault, cur, defaultPullPageLimit, defaultPullPageMaxBytes)
		if since == 0 && !includeDeleted {
			path += "&include_deleted=0"
		}
		if err := c.do(ctx, "GET", path, nil, &resp); err != nil {
			return nil, 0, "", err
		}
		all = append(all, resp.Entries...)
		latest = resp.LatestRevision
		if resp.MetadataHash != "" {
			metaHash = resp.MetadataHash
		}
		if !resp.HasMore {
			break
		}
		if resp.NextSince <= cur {
			return nil, 0, "", fmt.Errorf("server 分页游标未前进（since=%d, next_since=%d）", cur, resp.NextSince)
		}
		cur = resp.NextSince
	}
	if all == nil {
		all = []Entry{}
	}
	return all, latest, metaHash, nil
}

// Lookup 批量按 (kind, grp, key) 查询远端条目当前状态或密文。
func (c *serverClient) Lookup(ctx context.Context, vault string, keys []EntryKey, metaOnly bool) ([]Entry, int64, error) {
	if len(keys) == 0 {
		return []Entry{}, 0, nil
	}
	var resp struct {
		Entries        []Entry `json:"entries"`
		LatestRevision int64   `json:"latest_revision"`
	}
	body := map[string]any{
		"keys":      keys,
		"meta_only": metaOnly,
	}
	if err := c.do(ctx, "POST", "/v1/vaults/"+vault+"/entries/lookup", body, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Entries, resp.LatestRevision, nil
}

// entryID 是条目的唯一标识（kind/grp/key 三元组）
func entryID(kind, grp, key string) string {
	return kind + "\x00" + grp + "\x00" + key
}

// RegisterClientResult 是注册成功的结果
type RegisterClientResult struct {
	Token      string `json:"token"`
	ClientID   int64  `json:"client_id"`
	ClientName string `json:"client_name"`
}

// HistoryVersion 是 server 端条目的一个历史版本（前像密文，本地解密展示）
type HistoryVersion struct {
	Kind       string    `json:"kind"`
	Grp        string    `json:"grp"`
	Key        string    `json:"key"`
	Ciphertext []byte    `json:"ciphertext,omitempty"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	Deleted    bool      `json:"deleted"`
	// ReplacedAt 是该前像被替换的时刻；Actor* 是服务端认定的操作者。
	ReplacedAt      time.Time `json:"replaced_at"`
	ActorClientID   int64     `json:"actor_client_id,omitempty"`
	ActorUserID     int64     `json:"actor_user_id,omitempty"`
	ActorClientName string    `json:"actor_client_name,omitempty"`
}

// HistoryFilter 过滤历史查询；零值字段不过滤
type HistoryFilter struct {
	Kind  string
	Grp   string
	Key   string
	Limit int
}

// History 拉取条目历史（指定 key 时按 revision 新到旧，否则 vault 级最近变更）
func (c *serverClient) History(ctx context.Context, vault string, f HistoryFilter) ([]HistoryVersion, error) {
	path := fmt.Sprintf("/v1/vaults/%s/history?", vault)
	add := func(k string, v any) { path += fmt.Sprintf("%s=%v&", k, v) }
	if f.Kind != "" {
		add("kind", f.Kind)
	}
	if f.Grp != "" {
		add("grp", f.Grp)
	}
	if f.Key != "" {
		add("key", f.Key)
	}
	if f.Limit > 0 {
		add("limit", f.Limit)
	}
	var resp struct {
		History []HistoryVersion `json:"history"`
	}
	if err := c.do(ctx, "GET", strings.TrimSuffix(path, "&"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.History, nil
}

// RegisterClient 凭一次性注册码把本机注册为 server client，返回一次性明文
// token。注册端点免认证，调用方先用 ValidateServerAddress 校验地址。
func RegisterClient(ctx context.Context, address, code, name string) (*RegisterClientResult, error) {
	client := newServerClient(address, "")
	var resp struct {
		Token  string `json:"token"`
		Client struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"client"`
	}
	err := client.do(ctx, "POST", "/v1/register", map[string]string{"code": code, "name": name}, &resp)
	if err != nil {
		return nil, err
	}
	return &RegisterClientResult{Token: resp.Token, ClientID: resp.Client.ID, ClientName: resp.Client.Name}, nil
}
