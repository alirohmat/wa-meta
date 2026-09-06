package main

import (
	"sync"
	"time"
)

type logEntry struct {
	T     string `json:"t"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Extra any    `json:"extra,omitempty"`
}
type logStore struct {
	mu sync.Mutex
	a  []logEntry
}

func (l *logStore) add(level, msg string, extra any) logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := logEntry{time.Now().Format(time.RFC3339), level, msg, extra}
	l.a = append(l.a, e)
	if len(l.a) > 800 {
		l.a = l.a[len(l.a)-800:]
	}
	return e
}
func (l *logStore) all() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]logEntry, len(l.a))
	copy(out, l.a)
	return out
}
func (l *logStore) clear() { l.mu.Lock(); l.a = nil; l.mu.Unlock() }
func (l *logStore) clearLevel(level string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keep []logEntry
	for _, e := range l.a {
		if e.Level != level {
			keep = append(keep, e)
		}
	}
	l.a = keep
}
