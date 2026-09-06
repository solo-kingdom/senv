package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Entry 是一条同步条目（与 senv-server v1 API 的 wire 格式一致）。
// ciphertext 始终是不透明密文字节；CLI 侧不依赖 server 的 pgx 类型。
type Entry struct {
	Kind         string    `json:"kind"`
	Grp          string    `json:"grp"`
	Key          string    `json:"key"`
	Ciphertext   []byte    `json:"ciphertext,omitempty"`
	BaseRevision int64     `json:"base_revision,omitempty"`
	Revision     int64     `json:"revision"`
	Deleted      bool      `json:"deleted"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// Conflict 描述 server 返回的单条乐观锁冲突
type Conflict struct {
	Kind            string    `json:"kind"`
	Grp             string    `json:"grp"`
	Key             string    `json:"key"`
	CurrentRevision int64     `json:"current_revision"`
	Deleted         bool      `json:"deleted"`
	Size            int64     `json:"size"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	Push(ctx context.Context, vault string, entries []Entry) ([]Entry, int64, error)
	Pull(ctx context.Context, vault string, since int64) ([]Entry, int64, error)
}

// serverClient 是 senv-server v1 API 的 HTTP client
type serverClient struct {
	baseURL string
	token   string
	http    *http.Client
	// onBlocked 在首次收到 403 client_blocked 时触发（每实例一次），
	// 由 provider 注入用于清空本地解锁缓存；fake 实现不含此路径。
	onBlocked   func()
	blockedOnce sync.Once
}

func newServerClient(baseURL, token string) *serverClient {
	return &serverClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// do 发起认证请求并统一错误解析（401/404/409/其他）
func (c *serverClient) do(ctx context.Context, method, path string, body any, out any) error {
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
		return fmt.Errorf("无法连接 server: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("server 认证失败（401）：token 无效或已吊销")
	case resp.StatusCode == http.StatusForbidden:
		var body struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
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
		return ErrVaultNotFound
	case resp.StatusCode == http.StatusConflict:
		var body struct {
			Error     string     `json:"error"`
			Conflicts []Conflict `json:"conflicts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
		json.NewDecoder(resp.Body).Decode(&body)
		if body.Error == "" {
			body.Error = resp.Status
		}
		return fmt.Errorf("server 错误: %s", body.Error)
	}
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
	var resp struct {
		Entries        []Entry `json:"entries"`
		LatestRevision int64   `json:"latest_revision"`
	}
	path := fmt.Sprintf("/v1/vaults/%s/entries?since=%d", vault, since)
	if err := c.do(ctx, "GET", path, nil, &resp); err != nil {
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
