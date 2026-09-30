// Package testlog provides an isolated diagnostic recorder for contract tests.
package testlog

import (
	"fmt"
	"maps"
	"sync"
)

type Entry struct {
	Level   string
	Message string
	Fields  map[string]any
}

type Logger struct {
	mu      sync.Mutex
	entries []Entry
}

func (l *Logger) record(level, message string, fields []any) {
	entry := Entry{Level: level, Message: message, Fields: make(map[string]any)}
	for index := 0; index+1 < len(fields); index += 2 {
		entry.Fields[fmt.Sprint(fields[index])] = fields[index+1]
	}
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	l.mu.Unlock()
}

func (l *Logger) Warnf(format string, args ...any) {
	l.record("warn", fmt.Sprintf(format, args...), nil)
}
func (l *Logger) Warnw(message string, fields ...any)  { l.record("warn", message, fields) }
func (l *Logger) Debugw(message string, fields ...any) { l.record("debug", message, fields) }

func (l *Logger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := make([]Entry, len(l.entries))
	for index, entry := range l.entries {
		entries[index] = entry
		entries[index].Fields = maps.Clone(entry.Fields)
	}
	return entries
}
