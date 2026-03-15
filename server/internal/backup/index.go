package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type IndexManager struct {
	path  string
	mu    sync.RWMutex
	index *Index
}

func NewIndexManager(backupDir string) (*IndexManager, error) {
	path := filepath.Join(backupDir, IndexFileName)
	im := &IndexManager{
		path:  path,
		index: &Index{Backups: []BackupMeta{}},
	}
	if err := im.load(); err != nil {
		return nil, err
	}
	return im, nil
}

func (im *IndexManager) load() error {
	data, err := os.ReadFile(im.path)
	if os.IsNotExist(err) {
		return im.save()
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, im.index)
}

func (im *IndexManager) save() error {
	data, err := json.MarshalIndent(im.index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(im.path, data, 0644)
}

func (im *IndexManager) List() []BackupMeta {
	im.mu.RLock()
	defer im.mu.RUnlock()
	result := make([]BackupMeta, len(im.index.Backups))
	copy(result, im.index.Backups)
	return result
}

func (im *IndexManager) Get(name string) (*BackupMeta, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()
	for i := range im.index.Backups {
		if im.index.Backups[i].Name == name {
			return &im.index.Backups[i], true
		}
	}
	return nil, false
}

func (im *IndexManager) Add(meta BackupMeta) error {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.index.Backups = append(im.index.Backups, meta)
	return im.save()
}

func (im *IndexManager) Remove(name string) error {
	im.mu.Lock()
	defer im.mu.Unlock()
	for i := range im.index.Backups {
		if im.index.Backups[i].Name == name {
			im.index.Backups = append(im.index.Backups[:i], im.index.Backups[i+1:]...)
			return im.save()
		}
	}
	return ErrBackupNotFound
}
