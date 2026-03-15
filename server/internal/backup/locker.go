package backup

import "sync"

type StoreLocker struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

func NewStoreLocker() *StoreLocker {
	return &StoreLocker{
		locks: make(map[string]struct{}),
	}
}

func (l *StoreLocker) Lock(store string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.locks[store]; ok {
		return ErrStoreLocked
	}
	l.locks[store] = struct{}{}
	return nil
}

func (l *StoreLocker) Unlock(store string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, store)
}

func (l *StoreLocker) IsLocked(store string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.locks[store]
	return ok
}
