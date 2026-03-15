package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Archiver struct {
	backupDir string
}

func NewArchiver(backupDir string) *Archiver {
	return &Archiver{backupDir: backupDir}
}

func (a *Archiver) Create(ctx context.Context, name string, storePaths map[string]string, manifest *Manifest, exclusions []string) (string, int64, int, error) {
	stagingDir := filepath.Join(a.backupDir, ".staging", time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", 0, 0, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	manifestPath := filepath.Join(stagingDir, ManifestFileName)
	if err := WriteManifestFile(manifestPath, manifest); err != nil {
		return "", 0, 0, fmt.Errorf("write manifest: %w", err)
	}

	zipPath := filepath.Join(a.backupDir, name)

	f, err := os.Create(zipPath)
	if err != nil {
		return "", 0, 0, fmt.Errorf("create zip file: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	if err := a.addFileToZip(zw, ManifestFileName, manifestPath); err != nil {
		os.Remove(zipPath)
		return "", 0, 0, err
	}

	var totalSize int64
	var recordCount int

	for storeName, storePath := range storePaths {
		select {
		case <-ctx.Done():
			os.Remove(zipPath)
			return "", 0, 0, ctx.Err()
		default:
		}

		count, size, err := a.addStoreToZip(ctx, zw, storeName, storePath, exclusions)
		if err != nil {
			os.Remove(zipPath)
			return "", 0, 0, err
		}
		recordCount += count
		totalSize += size
	}

	return zipPath, totalSize, recordCount, nil
}

func (a *Archiver) addFileToZip(zw *zip.Writer, zipPath, srcPath string) error {
	w, err := zw.Create(zipPath)
	if err != nil {
		return err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}

func (a *Archiver) addStoreToZip(ctx context.Context, zw *zip.Writer, storeName, storePath string, exclusions []string) (int, int64, error) {
	var recordCount int
	var totalSize int64

	err := filepath.Walk(storePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		base := filepath.Base(path)
		if info.IsDir() && isExcludedDir(base, exclusions) {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		if isExcludedFile(base, exclusions) {
			return nil
		}

		relPath, err := filepath.Rel(storePath, path)
		if err != nil {
			return err
		}

		zipPath := filepath.Join(storeName, relPath)

		if base == ".meta.json" || base == ".record.json" {
			recordCount++
		}

		w, err := zw.Create(zipPath)
		if err != nil {
			return fmt.Errorf("create zip entry: %w", err)
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		n, err := io.Copy(w, file)
		if err != nil {
			return err
		}

		totalSize += n
		return nil
	})

	return recordCount, totalSize, err
}

func isExcludedDir(name string, exclusions []string) bool {
	for _, ex := range exclusions {
		if name == ex {
			return true
		}
	}
	return false
}

func isExcludedFile(name string, exclusions []string) bool {
	for _, ex := range exclusions {
		if name == ex {
			return true
		}
	}
	return false
}

func (a *Archiver) Extract(ctx context.Context, zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ErrBackupCorrupted
	}
	defer r.Close()

	for _, f := range r.File {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		destPath := filepath.Join(destDir, f.Name)

		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid path in archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			destFile.Close()
			return err
		}

		_, err = io.Copy(destFile, rc)
		rc.Close()
		destFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
