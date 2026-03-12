package store

import (
	"encoding/json"
	"os"
	"time"
)

type StoreMetaFile struct {
	Version       int                    `json:"version"`
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Path          string                 `json:"path"`
	Schema        map[string]interface{} `json:"schema,omitempty"`
	NamingPattern string                 `json:"namingPattern,omitempty"`
	Counter       int                    `json:"counter"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

func ParseStoreMeta(data []byte) (*StoreMetaFile, error) {
	var meta StoreMetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func WriteStoreMeta(meta *StoreMetaFile) ([]byte, error) {
	return json.MarshalIndent(meta, "", "  ")
}

func (m *StoreMetaFile) ToMetadata() *StoreMetadata {
	return &StoreMetadata{
		Name:          m.Name,
		Type:          m.Type,
		Path:          m.Path,
		Schema:        m.Schema,
		NamingPattern: m.NamingPattern,
		Counter:       m.Counter,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		IndexReady:    false,
	}
}

func MetadataToMetaFile(meta *StoreMetadata, version int) *StoreMetaFile {
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	return &StoreMetaFile{
		Version:       version,
		Name:          meta.Name,
		Type:          meta.Type,
		Path:          meta.Path,
		Schema:        meta.Schema,
		NamingPattern: meta.NamingPattern,
		Counter:       meta.Counter,
		CreatedAt:     meta.CreatedAt,
		UpdatedAt:     now,
	}
}

func ReadStoreMetaFile(path string) (*StoreMetaFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseStoreMeta(data)
}

func WriteStoreMetaFile(path string, meta *StoreMetaFile) error {
	data, err := WriteStoreMeta(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
