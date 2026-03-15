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

	tmpPath := im.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, im.path)
}

func (im *IndexManager) List() []BackupMeta {
	im.mu.RLock()
	defer im.mu.RUnlock()
	result := make([]BackupMeta, len(im.index.Backups))
	copy(result, im.index.Backups)
	return result
}

func (im *IndexManager) Get(name string) (BackupMeta, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()
	for i := range im.index.Backups {
		if im.index.Backups[i].Name == name {
			return im.index.Backups[i], true
		}
	}
	return BackupMeta{}, false
}

func (im *IndexManager) Add(meta BackupMeta) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	newBackups := make([]BackupMeta, len(im.index.Backups)+1)
	copy(newBackups, im.index.Backups)
	newBackups[len(newBackups)-1] = meta

	oldIndex := im.index
	im.index = &Index{Backups: newBackups}

	if err := im.save(); err != nil {
		im.index = oldIndex
		return err
	}
	return nil
}

func (im *IndexManager) Remove(name string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	var foundIndex int = -1
	for i := range im.index.Backups {
		if im.index.Backups[i].Name == name {
			foundIndex = i
			break
		}
	}

	if foundIndex == -1 {
		return ErrBackupNotFound
	}

	newBackups := make([]BackupMeta, len(im.index.Backups)-1)
	copy(newBackups[:foundIndex], im.index.Backups[:foundIndex])
	copy(newBackups[foundIndex:], im.index.Backups[foundIndex+1:])

	oldIndex := im.index
	im.index = &Index{Backups: newBackups}

	if err := im.save(); err != nil {
		im.index = oldIndex
		return err
	}
	return nil
}
