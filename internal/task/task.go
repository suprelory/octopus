package task

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

type taskEntry struct {
	name       string
	interval   time.Duration
	fn         func()
	runOnStart bool
	stopCh     chan struct{}
	updateCh   chan struct{}
	running    atomic.Bool
}

var (
	tasks        = make(map[string]*taskEntry)
	tasksMu      sync.RWMutex
	tasksStarted bool
)

// Register 注册一个定时任务
// runOnStart: 是否在启动时立即执行一次
func Register(name string, interval time.Duration, runOnStart bool, fn func()) {
	if interval <= 0 {
		log.Debugf("task %s not registered: interval is 0", name)
		return
	}

	tasksMu.Lock()
	defer tasksMu.Unlock()

	if _, exists := tasks[name]; exists {
		log.Warnf("task %s already registered, skipping", name)
		return
	}

	tasks[name] = &taskEntry{
		name:       name,
		interval:   interval,
		fn:         fn,
		runOnStart: runOnStart,
		stopCh:     make(chan struct{}),
		updateCh:   make(chan struct{}, 1),
	}
	if tasksStarted {
		entry := tasks[name]
		safe.Go("task-loop:"+name, func() { runTask(entry) })
	}
	log.Debugf("task %s registered with interval %v, runOnStart: %v", name, interval, runOnStart)
}

// Update 更新任务的执行间隔
// 当 interval 为 0 时，删除任务
func Update(name string, interval time.Duration) {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	entry, exists := tasks[name]
	if !exists {
		log.Warnf("task %s not found", name)
		return
	}

	if interval <= 0 {
		delete(tasks, name)
		close(entry.stopCh)
		log.Infof("task %s removed: interval is 0", name)
		return
	}
	entry.interval = interval
	select {
	case entry.updateCh <- struct{}{}:
		log.Infof("task %s interval updated to %v", name, interval)
	default:
		// A wakeup is already queued; the loop reads the latest interval.
	}
}

// RUN 启动所有注册的任务
func RUN() {
	startTasks()
	// 阻塞主协程
	select {}
}

func startTasks() {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	if tasksStarted {
		return
	}
	tasksStarted = true
	for _, entry := range tasks {
		safe.Go("task-loop:"+entry.name, func() {
			runTask(entry)
		})
	}
}

func runTask(entry *taskEntry) {
	tasksMu.RLock()
	interval := entry.interval
	tasksMu.RUnlock()
	// 根据配置决定是否在启动时立即执行
	if entry.runOnStart {
		triggerTask(entry, "startup")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			triggerTask(entry, "ticker")
		case <-entry.updateCh:
			tasksMu.RLock()
			interval = entry.interval
			tasksMu.RUnlock()
			ticker.Reset(interval)
		case <-entry.stopCh:
			return
		}
	}
}

func triggerTask(entry *taskEntry, trigger string) {
	if entry == nil {
		return
	}
	select {
	case <-entry.stopCh:
		return
	default:
	}
	if !entry.running.CompareAndSwap(false, true) {
		log.Warnf("task %s skipped: previous run still in progress (trigger=%s)", entry.name, trigger)
		return
	}
	safe.Go("task-exec:"+entry.name+":"+trigger, func() {
		defer entry.running.Store(false)
		entry.fn()
	})
}
