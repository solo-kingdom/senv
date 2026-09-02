package handler

import "context"

func contextWithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func userIDFrom(ctx context.Context) int64 {
	v, _ := ctx.Value(userIDKey).(int64)
	return v
}
