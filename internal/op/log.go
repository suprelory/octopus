package op

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
)

const (
	relayLogBatchSize        = 200
	relayLogFlushInterval    = time.Second
	relayLogQueueSize        = 5000
	relayLogQueueBytes       = 64 << 20
	relayLogRecentMaxSize    = 100 // 最近日志缓存，用于实时查询/不落库模式
	relayLogCleanupBatchSize = 1000
	relayLogCleanupBatchWait = 30 * time.Millisecond
	relayLogWriterMaxBatches = 25
)

// relayLogBufferState owns the buffers shared by writes, queries and clearing.
// When locks are combined, acquire flushLock before snapshotLock, then the
// pending/recent lock. The state must not be copied after first use.
type relayLogBufferState struct {
	pendingLock  sync.Mutex
	pending      []model.RelayLog
	pendingBytes int64

	recentLock sync.Mutex
	recent     []model.RelayLog

	flushLock   sync.Mutex
	flushSignal chan struct{}

	// RelayLogAdd holds a read lock from ID allocation through its buffer writes.
	// RelayLogClear takes the write lock to mint a cutoff and prune the buffers,
	// making every log unambiguously older or newer than the active purge.
	snapshotLock sync.RWMutex

	// Protected by flushLock; rejects overlapping clears. A purge releases
	// flushLock before batched DB deletes so the writer can keep draining logs
	// admitted after the snapshot without overflowing the pending queue.
	clearing bool

	droppedTotal atomic.Uint64
	lastDropWarn atomic.Int64
}

var relayLogBuffer = relayLogBufferState{
	pending:     make([]model.RelayLog, 0, relayLogBatchSize),
	recent:      make([]model.RelayLog, 0, relayLogRecentMaxSize),
	flushSignal: make(chan struct{}, 1),
}

// RelayLogWriterRun flushes persisted relay logs from the in-memory queue in
// the background. It wakes either when the queue reaches relayLogBatchSize or
// on relayLogFlushInterval; request goroutines never write relay_logs directly.
func RelayLogWriterRun(ctx context.Context) {
	ticker := time.NewTicker(relayLogFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = RelayLogFlushPending(flushCtx)
			cancel()
			return
		case <-relayLogBuffer.flushSignal:
			if err := relayLogDrainPending(ctx, relayLogWriterMaxBatches); err != nil {
				log.Warnw("relay_log.flush_failed", "batch_size", relayLogBatchSize, "queue_length", RelayLogPendingLen(), "error", err.Error())
			}
		case <-ticker.C:
			if err := relayLogDrainPending(ctx, relayLogWriterMaxBatches); err != nil {
				log.Warnw("relay_log.flush_failed", "batch_size", relayLogBatchSize, "queue_length", RelayLogPendingLen(), "error", err.Error())
			}
		}
	}
}

func signalRelayLogFlush() {
	select {
	case relayLogBuffer.flushSignal <- struct{}{}:
	default:
	}
}

func appendRelayLogRecent(relayLog model.RelayLog) {
	relayLogBuffer.recentLock.Lock()
	relayLogBuffer.recent = append(relayLogBuffer.recent, relayLog)
	if len(relayLogBuffer.recent) > relayLogRecentMaxSize {
		keep := relayLogRecentMaxSize / 2
		relayLogBuffer.recent = append([]model.RelayLog(nil), relayLogBuffer.recent[len(relayLogBuffer.recent)-keep:]...)
	}
	relayLogBuffer.recentLock.Unlock()
}

func enqueueRelayLogPending(relayLog model.RelayLog) bool {
	estimatedBytes := relayLogApproxBytes(relayLog)
	relayLogBuffer.pendingLock.Lock()
	defer relayLogBuffer.pendingLock.Unlock()
	if len(relayLogBuffer.pending) >= relayLogQueueSize || relayLogBuffer.pendingBytes+estimatedBytes > relayLogQueueBytes {
		dropped := relayLogBuffer.droppedTotal.Add(1)
		warnRelayLogDropped(dropped)
		return false
	}
	relayLogBuffer.pending = append(relayLogBuffer.pending, relayLog)
	relayLogBuffer.pendingBytes += estimatedBytes
	if len(relayLogBuffer.pending) >= relayLogBatchSize {
		signalRelayLogFlush()
	}
	return true
}

func relayLogApproxBytes(relayLog model.RelayLog) int64 {
	size := 256
	size += len(relayLog.RequestModelName) + len(relayLog.RequestAPIKeyName) + len(relayLog.ClientIP) + len(relayLog.EndpointType) + len(relayLog.ChannelName) + len(relayLog.ActualModelName) + len(relayLog.ReasoningEffort)
	size += len(relayLog.RequestContent) + len(relayLog.ResponseContent) + len(relayLog.Error)
	for _, attempt := range relayLog.Attempts {
		size += 96 + len(attempt.ChannelName) + len(attempt.ModelName) + len(attempt.AdapterType) + len(attempt.Msg)
	}
	return int64(size)
}

func warnRelayLogDropped(dropped uint64) {
	now := time.Now().Unix()
	last := relayLogBuffer.lastDropWarn.Load()
	if now-last < 60 {
		return
	}
	if relayLogBuffer.lastDropWarn.CompareAndSwap(last, now) {
		log.Warnw("relay_log.queue_full", "dropped_total", dropped, "queue_size", relayLogQueueSize, "queue_bytes", relayLogQueueBytes)
	}
}

func RelayLogPendingLen() int {
	relayLogBuffer.pendingLock.Lock()
	defer relayLogBuffer.pendingLock.Unlock()
	return len(relayLogBuffer.pending)
}

func relayLogDrainPending(ctx context.Context, maxBatches int) error {
	if maxBatches <= 0 {
		maxBatches = 1
	}
	for i := 0; i < maxBatches; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if RelayLogPendingLen() == 0 {
			return nil
		}
		if err := relayLogFlushPendingBatch(ctx, relayLogBatchSize); err != nil {
			return err
		}
	}
	if RelayLogPendingLen() > 0 {
		signalRelayLogFlush()
	}
	return nil
}

func relayLogFlushPendingBatch(ctx context.Context, batchSize int) error {
	if batchSize <= 0 {
		batchSize = relayLogBatchSize
	}
	relayLogBuffer.flushLock.Lock()
	defer relayLogBuffer.flushLock.Unlock()

	relayLogBuffer.pendingLock.Lock()
	if len(relayLogBuffer.pending) == 0 {
		relayLogBuffer.pendingLock.Unlock()
		return nil
	}
	if batchSize > len(relayLogBuffer.pending) {
		batchSize = len(relayLogBuffer.pending)
	}
	batch := make([]model.RelayLog, batchSize)
	copy(batch, relayLogBuffer.pending[:batchSize])
	batchBytes := relayLogBatchApproxBytes(batch)
	relayLogBuffer.pendingLock.Unlock()

	start := time.Now()
	result := db.GetDB().WithContext(ctx).CreateInBatches(&batch, relayLogBatchSize)
	if result.Error != nil {
		return result.Error
	}
	duration := time.Since(start)
	log.Debugw("relay_log.flush", "batch_size", len(batch), "duration", duration.String(), "queue_length", RelayLogPendingLen())

	relayLogBuffer.pendingLock.Lock()
	if len(relayLogBuffer.pending) >= batchSize && relayLogBuffer.pending[0].ID == batch[0].ID && relayLogBuffer.pending[batchSize-1].ID == batch[batchSize-1].ID {
		relayLogBuffer.pending = relayLogBuffer.pending[batchSize:]
		relayLogBuffer.pendingBytes -= batchBytes
	} else {
		flushed := make(map[int64]struct{}, len(batch))
		for _, item := range batch {
			flushed[item.ID] = struct{}{}
		}
		kept := relayLogBuffer.pending[:0]
		keptBytes := int64(0)
		for _, item := range relayLogBuffer.pending {
			if _, ok := flushed[item.ID]; !ok {
				kept = append(kept, item)
				keptBytes += relayLogApproxBytes(item)
			}
		}
		relayLogBuffer.pending = kept
		relayLogBuffer.pendingBytes = keptBytes
	}
	if relayLogBuffer.pendingBytes < 0 {
		relayLogBuffer.pendingBytes = 0
	}
	if len(relayLogBuffer.pending) == 0 {
		relayLogBuffer.pending = make([]model.RelayLog, 0, relayLogBatchSize)
		relayLogBuffer.pendingBytes = 0
	}
	relayLogBuffer.pendingLock.Unlock()

	return nil
}

func relayLogBatchApproxBytes(batch []model.RelayLog) int64 {
	var total int64
	for _, item := range batch {
		total += relayLogApproxBytes(item)
	}
	return total
}

func RelayLogFlushPending(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if RelayLogPendingLen() == 0 {
			return nil
		}
		if err := relayLogFlushPendingBatch(ctx, relayLogBatchSize); err != nil {
			return err
		}
	}
}

func RelayLogAdd(relayLog model.RelayLog) error {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}

	relayLogBuffer.snapshotLock.RLock()
	relayLog.ID = snowflake.GenerateID()
	notifySubscribers(relayLog)
	appendRelayLogRecent(relayLog)

	if enabled {
		enqueueRelayLogPending(relayLog)
	}
	relayLogBuffer.snapshotLock.RUnlock()

	return nil
}
