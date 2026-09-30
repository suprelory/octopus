package task

import (
	"sync"
	"testing"
	"time"
)

func resetTaskScheduler(t *testing.T) {
	t.Helper()
	reset := func() {
		tasksMu.Lock()
		defer tasksMu.Unlock()
		for _, entry := range tasks {
			close(entry.stopCh)
		}
		tasks = make(map[string]*taskEntry)
		tasksStarted = false
	}
	reset()
	t.Cleanup(reset)
}

func awaitTask(t *testing.T, ran <-chan struct{}) {
	t.Helper()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not run")
	}
}

func TestRegisterAfterStartAndReenable(t *testing.T) {
	resetTaskScheduler(t)
	startTasks()
	ran := make(chan struct{}, 100)
	Register("backup", 5*time.Millisecond, false, func() { ran <- struct{}{} })
	awaitTask(t, ran)
	Update("backup", 0)
	reenabled := make(chan struct{}, 100)
	Register("backup", 5*time.Millisecond, false, func() { reenabled <- struct{}{} })
	awaitTask(t, reenabled)
}

func TestIntervalUpdateBeforeStartAndWhileRunning(t *testing.T) {
	resetTaskScheduler(t)
	ran := make(chan struct{}, 100)
	Register("backup", time.Hour, false, func() { ran <- struct{}{} })
	Update("backup", 5*time.Millisecond)
	startTasks()
	awaitTask(t, ran)
	Update("backup", time.Hour)
	var writers sync.WaitGroup
	for i := 0; i < 20; i++ {
		writers.Add(1)
		go func() { defer writers.Done(); Update("backup", time.Hour) }()
	}
	writers.Wait()
	// Drain earlier ticks so only the final interval update can satisfy this.
	for len(ran) > 0 {
		<-ran
	}
	Update("backup", 5*time.Millisecond)
	awaitTask(t, ran)
}

func TestSchedulerStartsEachTaskOnce(t *testing.T) {
	resetTaskScheduler(t)
	ran := make(chan struct{}, 10)
	Register("once", time.Hour, true, func() { ran <- struct{}{} })
	startTasks()
	awaitTask(t, ran)
	startTasks()
	select {
	case <-ran:
		t.Fatal("scheduler started a duplicate loop")
	case <-time.After(20 * time.Millisecond):
	}
}
