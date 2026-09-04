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
	case resp.StatusCode == http.StatusNotFound:
		return ErrVaultNotFound
	case resp.StatusCode == http.StatusConflict:
		var body struct {
			Conflicts []Conflict `json:"conflicts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return fmt.Errorf("推送冲突，但无法解析冲突清单: %w", err)
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
