package backup

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

func (m *BackupManager) RestoreBackup(ctx context.Context, backupName, targetStore string, opts RestoreOptions) (*DryRunResult, error) {
	meta, err := m.GetBackup(backupName)
	if err != nil {
		return nil, err
	}

	manifest, err := ReadManifestFromZIP(filepath.Join(m.backupDir, backupName))
	if err != nil {
		return nil, err
	}

	if !opts.Force && meta.Scope != "all" && meta.Scope != targetStore {
		return nil, ErrScopeMismatch
	}

	targetPath := filepath.Join(m.dataDir, targetStore)
	if meta.Scope == "all" {
		targetPath = m.dataDir
	}

	if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("target path is not a directory: %s", targetPath)
	}

	lockKey := targetStore
	if meta.Scope == "all" {
		lockKey = "all"
	}

	if err := m.locker.Lock(lockKey); err != nil {
		return nil, err
	}
	defer m.locker.Unlock(lockKey)

	if opts.DryRun {
		return m.dryRunRestore(ctx, filepath.Join(m.backupDir, backupName), targetPath, manifest)
	}

	rollbackDir := filepath.Join(m.backupDir, RestoreTempDir, time.Now().UTC().Format("2006-01-02T150405"))
	if err := m.createRollback(ctx, targetPath, rollbackDir); err != nil {
		m.logger.Warn("failed to create rollback backup", "err", err)
	}

	extractDir := filepath.Join(m.backupDir, ExtractTempDir, time.Now().UTC().Format("2006-01-02T150405"))
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, fmt.Errorf("create extract directory: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if err := m.archiver.Extract(ctx, filepath.Join(m.backupDir, backupName), extractDir); err != nil {
		return nil, err
	}

	if err := m.executeRestore(ctx, extractDir, targetPath, manifest); err != nil {
		m.logger.Error("restore failed, rollback data available", "rollbackDir", rollbackDir)
		return nil, ErrRestoreFailed
	}

	m.logger.Info("restore completed",
		"backup", backupName,
		"target", targetStore)

	if err := os.RemoveAll(rollbackDir); err != nil {
		m.logger.Warn("failed to clean up rollback dir", "err", err)
	}

	return &DryRunResult{
		RecordCount: meta.RecordCount,
		StoreType:   manifest.StoreType,
	}, nil
}

func (m *BackupManager) dryRunRestore(ctx context.Context, zipPath, targetPath string, manifest *Manifest) (*DryRunResult, error) {
	extractDir := filepath.Join(m.backupDir, ExtractTempDir, "dryrun")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(extractDir)

	if err := m.archiver.Extract(ctx, zipPath, extractDir); err != nil {
		return nil, err
	}

	result := &DryRunResult{}

	backupBase := extractDir
	if manifest.Scope != "all" {
		backupBase = filepath.Join(extractDir, manifest.Scope)
	}

	currentRecords := make(map[string]bool)
	filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == ".meta.json" || base == ".record.json" {
			dir := filepath.Dir(path)
			if dir != targetPath {
				currentRecords[filepath.Base(dir)] = true
			}
		}
		return nil
	})

	backupRecords := make(map[string]bool)
	filepath.Walk(backupBase, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == ".meta.json" || base == ".record.json" {
			dir := filepath.Dir(path)
			if dir != backupBase {
				backupRecords[filepath.Base(dir)] = true
			}
		}
		return nil
	})

	for id := range backupRecords {
		if _, exists := currentRecords[id]; exists {
			result.WouldUpdate = append(result.WouldUpdate, id)
		} else {
			result.WouldCreate = append(result.WouldCreate, id)
		}
	}
	for id := range currentRecords {
		if _, exists := backupRecords[id]; !exists {
			result.WouldDelete = append(result.WouldDelete, id)
		}
	}

	result.RecordCount = len(backupRecords)
	result.StoreType = manifest.StoreType

	return result, nil
}

func (m *BackupManager) createRollback(ctx context.Context, targetPath, rollbackDir string) error {
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return nil
	}

	if err := os.MkdirAll(rollbackDir, 0755); err != nil {
		return err
	}

	return filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(targetPath, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(rollbackDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(ctx, path, destPath)
	})
}

func (m *BackupManager) executeRestore(ctx context.Context, extractDir, targetPath string, manifest *Manifest) error {
	sourceDir := extractDir
	if manifest.Scope != "all" {
		sourceDir = filepath.Join(extractDir, manifest.Scope)
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		name := entry.Name()
		if name == ".store.json" {
			continue
		}
		if IsExcluded(name) {
			continue
		}

		if err := os.RemoveAll(filepath.Join(targetPath, name)); err != nil {
			return err
		}
	}

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(ctx, path, destPath)
	})
}

func copyFile(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	return err
}

func (m *BackupManager) GetStoreLocker() *StoreLocker {
	return m.locker
}

func (m *BackupManager) GetDataDir() string {
	return m.dataDir
}

func (m *BackupManager) GetLogger() *slog.Logger {
	return m.logger
}
