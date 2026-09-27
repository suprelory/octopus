package sitesync

import "context"

type siteBatchTaskIDContextKey struct{}

func withSiteBatchTaskID(ctx context.Context, taskID int64) context.Context {
	if taskID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, siteBatchTaskIDContextKey{}, taskID)
}

func siteBatchTaskID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	taskID, _ := ctx.Value(siteBatchTaskIDContextKey{}).(int64)
	return taskID
}
