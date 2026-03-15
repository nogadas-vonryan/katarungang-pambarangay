package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type BackupManager struct {
	dataDir   string
	backupDir string
	index     *IndexManager
	archiver  *Archiver
	locker    *StoreLocker
	logger    *slog.Logger
}

func NewBackupManager(dataDir string, logger *slog.Logger) (*BackupManager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	backupDir := filepath.Join(dataDir, BackupDirName)

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}

	// Clean up any stale staging dirs from previous crashes
	os.RemoveAll(filepath.Join(backupDir, ".staging"))

	index, err := NewIndexManager(backupDir)
	if err != nil {
		return nil, fmt.Errorf("init index: %w", err)
	}

	return &BackupManager{
		dataDir:   dataDir,
		backupDir: backupDir,
		index:     index,
		archiver:  NewArchiver(backupDir),
		locker:    NewStoreLocker(),
		logger:    logger,
	}, nil
}

func (m *BackupManager) CreateBackup(ctx context.Context, scope string, storePaths map[string]StoreInfo, createdBy string) (*BackupResult, error) {
	var targetStores map[string]StoreInfo
	var scopeType string

	if scope == "all" {
		targetStores = storePaths
		scopeType = "complete"
	} else {
		info, ok := storePaths[scope]
		if !ok {
			return nil, ErrStoreNotFound
		}
		targetStores = map[string]StoreInfo{scope: info}
		scopeType = "store"
	}

	if m.locker.IsLocked(scope) {
		return nil, ErrBackupInProgress
	}

	if err := m.locker.Lock(scope); err != nil {
		return nil, err
	}
	defer m.locker.Unlock(scope)

	timestamp := time.Now().UTC().Format("2006-01-02T1504")
	var backupName string
	if scope == "all" {
		backupName = fmt.Sprintf("complete-backup-%s.zip", timestamp)
	} else {
		backupName = fmt.Sprintf("%s-%s.zip", scope, timestamp)
	}

	pathsToBackup := make(map[string]string)
	var storeType string
	for name, info := range targetStores {
		pathsToBackup[name] = info.Path
		if storeType == "" {
			storeType = info.Type
		}
	}

	manifest := NewManifest(scope, scopeType, storeType)

	if err := checkDiskSpace(m.backupDir, pathsToBackup); err != nil {
		return nil, err
	}

	zipPath, bytesWritten, recordCount, err := m.archiver.Create(ctx, backupName, pathsToBackup, manifest, FullBackupExclusions)
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(zipPath)
	if err != nil {
		return nil, err
	}

	meta := BackupMeta{
		Name:        backupName,
		Scope:       scope,
		ScopeType:   scopeType,
		Size:        stat.Size(),
		Timestamp:   time.Now().UTC(),
		RecordCount: recordCount,
		CreatedBy:   createdBy,
	}

	if err := m.index.Add(meta); err != nil {
		m.logger.Warn("failed to update index", "err", err)
	}

	m.logger.Info("backup created",
		"name", backupName,
		"scope", scope,
		"records", recordCount,
		"size", stat.Size())

	return &BackupResult{
		FilesProcessed: len(targetStores),
		TotalFiles:     len(targetStores),
		BytesWritten:   bytesWritten,
		RecordCount:    recordCount,
		BackupName:     backupName,
	}, nil
}

func (m *BackupManager) ListBackups() []BackupMeta {
	return m.index.List()
}

func (m *BackupManager) GetBackup(name string) (*BackupMeta, error) {
	meta, ok := m.index.Get(name)
	if !ok {
		return nil, ErrBackupNotFound
	}
	return &meta, nil
}

func (m *BackupManager) Delete(name string) error {
	zipPath := filepath.Join(m.backupDir, name)
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return m.index.Remove(name)
}

func (m *BackupManager) BackupDir() string {
	return m.backupDir
}
