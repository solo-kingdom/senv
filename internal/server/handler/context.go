package handler

import "context"

// accessInfo 是一次请求的身份与拦截原因聚合。认证中间件在原 context 上
// 通过指针原地修改（外层访问日志包装器因此能看到内层写入的值，无需重新
// 包装 context）。
type accessInfo struct {
	userID   int64
	clientID int64
	reason   string
}

func contextWithAccessInfo(ctx context.Context, info *accessInfo) context.Context {
	return context.WithValue(ctx, accessInfoKey, info)
}

func accessInfoFrom(ctx context.Context) *accessInfo {
	if info, ok := ctx.Value(accessInfoKey).(*accessInfo); ok {
		return info
	}
	return &accessInfo{}
}

// userIDFrom / clientIDFrom 供业务 handler 读取认证结果
func userIDFrom(ctx context.Context) int64   { return accessInfoFrom(ctx).userID }
func clientIDFrom(ctx context.Context) int64 { return accessInfoFrom(ctx).clientID }

type contextKey string

const accessInfoKey contextKey = "accessInfo"
