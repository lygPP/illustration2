package usage

import (
	"context"
	"strings"
)

type Recorder interface {
	AddUsage(userID, modelName string, promptTokens, completionTokens int) error
}

type contextKey string

const (
	userIDKey contextKey = "usageUserID"
	storeKey  contextKey = "usageStore"
)

func WithContext(ctx context.Context, userID string, recorder Recorder) context.Context {
	ctx = context.WithValue(ctx, userIDKey, userID)
	ctx = context.WithValue(ctx, storeKey, recorder)
	return ctx
}

func Record(ctx context.Context, modelName string, promptTokens, completionTokens int) error {
	userID, _ := ctx.Value(userIDKey).(string)
	recorder, _ := ctx.Value(storeKey).(Recorder)
	if recorder == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	return recorder.AddUsage(userID, modelName, promptTokens, completionTokens)
}
