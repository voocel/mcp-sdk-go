package server

import "context"

type ctxKeyTaskID struct{}

func contextWithTaskID(ctx context.Context, taskID string) context.Context {
	if taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyTaskID{}, taskID)
}

func taskIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	taskID, ok := ctx.Value(ctxKeyTaskID{}).(string)
	if !ok || taskID == "" {
		return "", false
	}
	return taskID, true
}
