package index

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kp-cms/server/internal/store"
)

type RecordMeta struct {
	ID        string
	Store     string
	Version   int
	ETag      string
	UpdatedAt time.Time
}

type Index struct {
	mu       sync.RWMutex
	stores   map[string]map[string]RecordMeta
	watchers map[string]struct{}
	logger   *slog.Logger
	closed   bool
}

func New(logger *slog.Logger) *Index {
	if logger == nil {
		logger = slog.Default()
	}
	return &Index{
		stores:   make(map[string]map[string]RecordMeta),
		watchers: make(map[string]struct{}),
		logger:   logger,
	}
}

func (idx *Index) Get(storeName, recordID string) (RecordMeta, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	storeIdx, ok := idx.stores[storeName]
	if !ok {
		return RecordMeta{}, false
	}
	meta, ok := storeIdx[recordID]
	return meta, ok
}

func (idx *Index) List(storeName string) []RecordMeta {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	storeIdx, ok := idx.stores[storeName]
	if !ok {
		return nil
	}
	metas := make([]RecordMeta, 0, len(storeIdx))
	for _, meta := range storeIdx {
		metas = append(metas, meta)
	}
	return metas
}

func (idx *Index) Set(storeName string, records []store.Record) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, ok := idx.stores[storeName]; !ok {
		idx.stores[storeName] = make(map[string]RecordMeta)
	}
	for _, r := range records {
		idx.stores[storeName][r.ID] = RecordMeta{
			ID:        r.ID,
			Store:     r.Store,
			Version:   r.Version,
			ETag:      r.ETag,
			UpdatedAt: r.UpdatedAt,
		}
	}
}

func (idx *Index) Add(storeName string, record store.Record) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if _, ok := idx.stores[storeName]; !ok {
		idx.stores[storeName] = make(map[string]RecordMeta)
	}
	idx.stores[storeName][record.ID] = RecordMeta{
		ID:        record.ID,
		Store:     record.Store,
		Version:   record.Version,
		ETag:      record.ETag,
		UpdatedAt: record.UpdatedAt,
	}
}

func (idx *Index) Update(storeName string, record store.Record) {
	idx.Add(storeName, record)
}

func (idx *Index) Remove(storeName, recordID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if storeIdx, ok := idx.stores[storeName]; ok {
		delete(storeIdx, recordID)
	}
}

func (idx *Index) Clear(storeName string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.stores, storeName)
}

func (idx *Index) ClearAll() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.stores = make(map[string]map[string]RecordMeta)
}

func (idx *Index) WatchStore(storeName string, st store.Store) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, ok := idx.watchers[storeName]; ok {
		return nil
	}

	eventCh := make(chan store.StoreEvent, 100)
	if err := st.Watch(context.Background(), eventCh); err != nil {
		return err
	}

	idx.watchers[storeName] = struct{}{}

	go idx.watchLoop(storeName, eventCh)

	return nil
}

func (idx *Index) watchLoop(storeName string, eventCh chan store.StoreEvent) {
	for event := range eventCh {
		switch event.Type {
		case "create":
			idx.logger.Debug("index: record created", "store", storeName, "id", event.RecordID)
		case "update":
			idx.logger.Debug("index: record updated", "store", storeName, "id", event.RecordID)
		case "delete":
			idx.Remove(storeName, event.RecordID)
			idx.logger.Debug("index: record deleted", "store", storeName, "id", event.RecordID)
		}
	}
}

func (idx *Index) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.closed = true
	return nil
}

type IndexService struct {
	index  *Index
	stores map[string]store.Store
	mu     sync.RWMutex
	logger *slog.Logger
}

func NewService(logger *slog.Logger) *IndexService {
	if logger == nil {
		logger = slog.Default()
	}
	return &IndexService{
		index:  New(logger),
		stores: make(map[string]store.Store),
		logger: logger,
	}
}

func (svc *IndexService) RegisterStore(name string, st store.Store) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	svc.stores[name] = st

	records, err := st.List(context.Background(), store.ListOptions{})
	if err != nil {
		return err
	}

	svc.index.Set(name, records)
	svc.logger.Info("index: store registered", "name", name, "records", len(records))

	return nil
}

func (svc *IndexService) UnregisterStore(name string) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	delete(svc.stores, name)
	svc.index.Clear(name)

	svc.logger.Info("index: store unregistered", "name", name)
	return nil
}

func (svc *IndexService) GetIndex() *Index {
	return svc.index
}

func (svc *IndexService) RebuildIndex(ctx context.Context, storeName string) error {
	svc.mu.RLock()
	st, ok := svc.stores[storeName]
	svc.mu.RUnlock()

	if !ok {
		return nil
	}

	records, err := st.List(ctx, store.ListOptions{})
	if err != nil {
		return err
	}

	svc.index.Set(storeName, records)
	svc.logger.Info("index: rebuilt", "store", storeName, "records", len(records))
	return nil
}

func (svc *IndexService) RebuildAll(ctx context.Context) error {
	svc.mu.RLock()
	stores := make(map[string]store.Store)
	for k, v := range svc.stores {
		stores[k] = v
	}
	svc.mu.RUnlock()

	for name := range stores {
		if err := svc.RebuildIndex(ctx, name); err != nil {
			return err
		}
	}
	return nil
}
