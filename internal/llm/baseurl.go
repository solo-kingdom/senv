package llm

import (
	"net/url"
	"strings"
)

// ProtocolFamily 描述一个 coding agent 使用的 API 方言。不同族的接入点形态
// 不同：同一份档案写进不同族的 agent 配置时必须转换。
type ProtocolFamily int

const (
	// ProtocolOpenAICompatible：OpenAI 兼容 API（codex/kimi/pi/opencode）。
	// 调用方在 base 之后拼 /chat/completions 或 /responses，因此 base 必须带
	// 版本段。
	ProtocolOpenAICompatible ProtocolFamily = iota
	// ProtocolAnthropic：Anthropic Messages API（claude-code）。SDK 自行拼
	// /v1/messages，因此 base 必须不带版本段。
	ProtocolAnthropic
)

// baseURLForFamily 把档案里的接入地址（统一按 OpenAI 兼容形态落库）转成某个
// 协议族写进 agent 配置的形态。
//
// 归一幂等：重复调用结果不变；解析失败或缺少 scheme/host 时原样返回，交由既有
// URL 校验报错。只处理路径末段，不猜 /v1beta 等版本变体，也不动中间重复斜杠，
// query 与 fragment 原样保留。
func baseURLForFamily(raw string, family ProtocolFamily) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	// 尾斜杠先收敛，与同步后端 server_client 的既有约定一致。
	path := strings.TrimRight(u.Path, "/")
	if family == ProtocolAnthropic {
		path = trimTrailingV1(path)
	} else {
		path = ensureTrailingV1(path)
	}
	u.Path = path
	return u.String()
}

func hasTrailingV1(path string) bool {
	return path == "/v1" || strings.HasSuffix(path, "/v1")
}

// hasVersionSegment 判断路径末段是否本身就是纯数字版本段（v1、v4 这类）。
// 智谱开放平台等网关的 OpenAI 兼容端点以 /v4 结尾，再补 /v1 会得到 /v4/v1
// 的死地址（实测 404），这类末段视为已归一。/v1beta 等带字母后缀的变体仍不
// 识别，维持 ADR 0004 记载的有损语义。
func hasVersionSegment(path string) bool {
	last := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		last = path[i+1:]
	}
	if len(last) < 2 || last[0] != 'v' {
		return false
	}
	for i := 1; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			return false
		}
	}
	return true
}

func ensureTrailingV1(path string) string {
	if hasTrailingV1(path) || hasVersionSegment(path) {
		return path
	}
	return path + "/v1"
}

// trimTrailingV1 剥离末段 /v1。对 Anthropic 族这是无损变换：Claude Code 总会
// 请求 base + /v1/messages，剥离前后的最终 URL 完全相同。删掉这个转换前先想
// 清楚这一点。
func trimTrailingV1(path string) string {
	if !hasTrailingV1(path) {
		return path
	}
	// 再收敛一次尾斜杠：`//v1` 剥掉版本段后只剩 `/`。
	return strings.TrimRight(strings.TrimSuffix(path, "/v1"), "/")
}
