package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrUnknownStoreType = errors.New("unknown store type")
	ErrRecordNotFound   = errors.New("record not found")
	ErrRecordExists     = errors.New("record already exists")
	ErrInvalidID        = errors.New("invalid record ID")
	ErrIDNotAllowed     = errors.New("auto ID generation not allowed for this store")
)

type Record struct {
	ID        string                 `json:"id"`
	UUID      string                 `json:"uuid"`
	Store     string                 `json:"store"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
	Version   int                    `json:"version"`
	ETag      string                 `json:"etag"`
}

type FileInfo struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	Size        int64       `json:"size"`
	ContentType string      `json:"contentType"`
	ModTime     time.Time   `json:"modTime"`
	IsDir       bool        `json:"isDir"`
	Metadata    interface{} `json:"metadata,omitempty"`
}

type StoreMetadata struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Path          string                 `json:"path"`
	Description   string                 `json:"description,omitempty"`
	Schema        map[string]interface{} `json:"schema,omitempty"`
	NamingPattern string                 `json:"namingPattern,omitempty"`
	Counter       int                    `json:"counter"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	IndexReady    bool                   `json:"indexReady"`
}

type Store interface {
	Name() string
	Type() string
	Path() string
	Metadata() (*StoreMetadata, error)

	List(ctx context.Context, opts ListOptions) ([]Record, int, error)
	Get(ctx context.Context, id string) (*Record, error)
	Create(ctx context.Context, data map[string]interface{}) (*Record, error)
	Update(ctx context.Context, id string, data map[string]interface{}) (*Record, error)
	Replace(ctx context.Context, id string, data map[string]interface{}) (*Record, error)
	Delete(ctx context.Context, id string) error

	ListRecordFiles(ctx context.Context, recordID string) ([]FileInfo, error)
	UploadFile(ctx context.Context, recordID string, name string, data []byte) error
	DownloadFile(ctx context.Context, recordID string, name string) ([]byte, error)
	DeleteFile(ctx context.Context, recordID string, name string) error

	Watch(ctx context.Context, ch chan<- StoreEvent) error
	Close() error
}

type ListOptions struct {
	Limit    int
	Offset   int
	SortBy   string
	SortDesc bool
	Filter   map[string]interface{}
}

type StoreEvent struct {
	Type      string
	Store     string
	RecordID  string
	Timestamp time.Time
}

type StoreFactory func(path string, metadata *StoreMetadata) (Store, error)

var (
	storeFactories = make(map[string]StoreFactory)
	factoryMu      sync.RWMutex
)

func RegisterStoreType(t string, f StoreFactory) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
	storeFactories[t] = f
}

func CreateStore(t, path string, metadata *StoreMetadata) (Store, error) {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	f, ok := storeFactories[t]
	if !ok {
		return nil, ErrUnknownStoreType
	}
	return f(path, metadata)
}

func IsValidType(t string) bool {
	factoryMu.RLock()
	defer factoryMu.RUnlock()
	_, ok := storeFactories[t]
	return ok
}
