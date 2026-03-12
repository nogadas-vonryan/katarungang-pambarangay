package store

import (
	"fmt"
	"sync"
	"time"
)

type Locker interface {
	Lock(key string) func()
	RLock(key string) func()
	Remove(key string)
	Close() error
}

type RecordLocker struct {
	mu     sync.RWMutex
	locks  map[string]*sync.RWMutex
	refCnt map[string]int
	ttl    time.Duration
	stopCh chan struct{}
}

func NewRecordLocker(ttl time.Duration) *RecordLocker {
	l := &RecordLocker{
		locks:  make(map[string]*sync.RWMutex),
		refCnt: make(map[string]int),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *RecordLocker) cleanupLoop() {
	ticker := time.NewTicker(l.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.removeStale()
		case <-l.stopCh:
			return
		}
	}
}

func (l *RecordLocker) removeStale() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.locks {
		if l.refCnt[key] <= 0 {
			delete(l.locks, key)
			delete(l.refCnt, key)
		}
	}
}

func (l *RecordLocker) getOrCreate(key string) *sync.RWMutex {
	if m, ok := l.locks[key]; ok {
		return m
	}
	m := &sync.RWMutex{}
	l.locks[key] = m
	l.refCnt[key] = 0
	return m
}

func (l *RecordLocker) Lock(key string) func() {
	l.mu.Lock()
	m := l.getOrCreate(key)
	l.refCnt[key]++
	l.mu.Unlock()

	m.Lock()
	return func() {
		m.Unlock()
		l.mu.Lock()
		l.refCnt[key]--
		l.mu.Unlock()
	}
}

func (l *RecordLocker) RLock(key string) func() {
	l.mu.Lock()
	m := l.getOrCreate(key)
	l.refCnt[key]++
	l.mu.Unlock()

	m.RLock()
	return func() {
		m.RUnlock()
		l.mu.Lock()
		l.refCnt[key]--
		l.mu.Unlock()
	}
}

func (l *RecordLocker) Remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, key)
	delete(l.refCnt, key)
}

func (l *RecordLocker) Close() error {
	close(l.stopCh)
	return nil
}

type FileLocker struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

func NewFileLocker() *FileLocker {
	return &FileLocker{
		locks: make(map[string]struct{}),
	}
}

func (l *FileLocker) Lock(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.locks[path]; ok {
		return fmt.Errorf("file %s is locked", path)
	}
	l.locks[path] = struct{}{}
	return nil
}

func (l *FileLocker) Unlock(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, path)
}

func (l *FileLocker) IsLocked(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.locks[path]
	return ok
}
