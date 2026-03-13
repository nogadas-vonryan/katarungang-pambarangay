package folder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kp-cms/server/internal/store"
)

var defaultLogger = slog.Default()

func SetDefaultLogger(logger *slog.Logger) {
	if logger != nil {
		defaultLogger = logger
	}
}

const (
	StoreMetaFileName  = ".store.json"
	RecordMetaFileName = ".record.json"
	FilesDirName       = "files"
)

type FolderStore struct {
	name     string
	path     string
	metadata *store.StoreMetadata
	locker   store.Locker
	logger   *slog.Logger
}

type recordMetaFile struct {
	Version   int                    `json:"version"`
	ID        string                 `json:"id"`
	UUID      string                 `json:"uuid"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

func init() {
	store.RegisterStoreType("folder", New)
}

func New(path string, metadata *store.StoreMetadata) (store.Store, error) {
	if metadata == nil {
		metadata = &store.StoreMetadata{
			Name:          filepath.Base(path),
			Type:          "folder",
			Path:          path,
			IndexReady:    false,
			NamingPattern: "",
			Counter:       0,
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	fs := &FolderStore{
		name:     metadata.Name,
		path:     absPath,
		metadata: metadata,
		locker:   store.NewRecordLocker(5 * time.Minute),
		logger:   defaultLogger,
	}

	if err := fs.loadStoreMeta(); err != nil {
		return nil, err
	}

	return fs, nil
}

func (s *FolderStore) Name() string {
	return s.name
}

func (s *FolderStore) Type() string {
	return "folder"
}

func (s *FolderStore) Path() string {
	return s.path
}

func (s *FolderStore) Metadata() (*store.StoreMetadata, error) {
	return s.metadata, nil
}

func (s *FolderStore) loadStoreMeta() error {
	metaPath := filepath.Join(s.path, StoreMetaFileName)
	data, err := os.ReadFile(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveStoreMeta()
	}
	if err != nil {
		return fmt.Errorf("read store meta: %w", err)
	}

	meta, err := store.ParseStoreMeta(data)
	if err != nil {
		return fmt.Errorf("parse store meta: %w", err)
	}

	storeMeta := meta.ToMetadata()
	s.metadata.Name = storeMeta.Name
	s.metadata.Schema = storeMeta.Schema
	s.metadata.NamingPattern = storeMeta.NamingPattern
	s.metadata.Counter = storeMeta.Counter
	s.metadata.CreatedAt = storeMeta.CreatedAt
	s.metadata.UpdatedAt = storeMeta.UpdatedAt

	return nil
}

func (s *FolderStore) saveStoreMeta() error {
	meta := store.MetadataToMetaFile(s.metadata, 1)

	data, err := store.WriteStoreMeta(meta)
	if err != nil {
		return fmt.Errorf("marshal store meta: %w", err)
	}

	metaPath := filepath.Join(s.path, StoreMetaFileName)
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write store meta: %w", err)
	}

	s.metadata.UpdatedAt = meta.UpdatedAt
	return nil
}

func (s *FolderStore) recordPath(id string) string {
	return filepath.Join(s.path, id)
}

func (s *FolderStore) loadRecord(id string) (*recordMetaFile, error) {
	recordPath := s.recordPath(id)
	metaPath := filepath.Join(recordPath, RecordMetaFileName)

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read record meta: %w", err)
	}

	var meta recordMetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse record meta: %w", err)
	}

	return &meta, nil
}

func (s *FolderStore) saveRecord(id string, meta *recordMetaFile) error {
	recordPath := s.recordPath(id)

	if err := os.MkdirAll(recordPath, 0755); err != nil {
		return fmt.Errorf("create record directory: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal record meta: %w", err)
	}

	metaPath := filepath.Join(recordPath, RecordMetaFileName)
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write record meta: %w", err)
	}

	return nil
}

var folderIDGen = store.NewIDGenerator()

func (s *FolderStore) generateID() (string, error) {
	s.metadata.Counter++

	pattern := s.metadata.NamingPattern
	if pattern == "" {
		hash := sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano)))
		return hex.EncodeToString(hash[:])[:16], nil
	}

	id, err := folderIDGen.Generate(pattern, s.metadata.Counter)
	if err != nil {
		return "", err
	}

	if err := s.saveStoreMeta(); err != nil {
		return "", err
	}

	return id, nil
}

func (s *FolderStore) generateUUID() string {
	return store.GenerateUUID()
}

func (s *FolderStore) generateETag(id string, version int) string {
	return store.GenerateETag(id, version)
}

func (s *FolderStore) hasAutoID() bool {
	return folderIDGen.HasAutoID(s.metadata.NamingPattern)
}

func (s *FolderStore) scanRecords() ([]string, error) {
	entries, err := os.ReadDir(s.path)
	if err != nil {
		return nil, fmt.Errorf("read store directory: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "." || e.Name() == ".." {
			continue
		}

		recordPath := filepath.Join(s.path, e.Name(), RecordMetaFileName)
		if _, err := os.Stat(recordPath); err != nil {
			continue
		}

		ids = append(ids, e.Name())
	}

	return ids, nil
}

func (s *FolderStore) List(ctx context.Context, opts store.ListOptions) ([]store.Record, int, error) {
	unlock := s.locker.RLock("list")
	defer unlock()

	ids, err := s.scanRecords()
	if err != nil {
		return nil, 0, err
	}

	total := len(ids)

	records := make([]store.Record, 0, len(ids))
	for _, id := range ids {
		meta, err := s.loadRecord(id)
		if err != nil {
			continue
		}

		records = append(records, store.Record{
			ID:        meta.ID,
			UUID:      meta.UUID,
			Store:     s.name,
			Data:      meta.Data,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Version:   meta.Version,
			ETag:      s.generateETag(id, meta.Version),
		})
	}

	store.SortRecords(records, opts.SortBy, opts.SortDesc)

	if opts.Limit > 0 && opts.Offset >= 0 {
		end := opts.Offset + opts.Limit
		if end > len(records) {
			end = len(records)
		}
		if opts.Offset < len(records) {
			records = records[opts.Offset:end]
		} else {
			records = []store.Record{}
		}
	}

	return records, total, nil
}

func (s *FolderStore) Get(ctx context.Context, id string) (*store.Record, error) {
	unlock := s.locker.RLock(id)
	defer unlock()

	meta, err := s.loadRecord(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrRecordNotFound
		}
		return nil, err
	}

	return &store.Record{
		ID:        meta.ID,
		UUID:      meta.UUID,
		Store:     s.name,
		Data:      meta.Data,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Version:   meta.Version,
		ETag:      s.generateETag(id, meta.Version),
	}, nil
}

func (s *FolderStore) Create(ctx context.Context, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock("create")
	defer unlock()

	userID, hasUserID := data["_id"].(string)
	if hasUserID {
		delete(data, "_id")

		recordPath := s.recordPath(userID)
		if _, err := os.Stat(recordPath); err == nil {
			return nil, store.ErrRecordExists
		}

		meta := recordMetaFile{
			Version:   1,
			ID:        userID,
			UUID:      s.generateUUID(),
			Data:      data,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := s.saveRecord(userID, &meta); err != nil {
			return nil, err
		}

		return &store.Record{
			ID:        meta.ID,
			UUID:      meta.UUID,
			Store:     s.name,
			Data:      meta.Data,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Version:   meta.Version,
			ETag:      s.generateETag(userID, 1),
		}, nil
	}

	if !s.hasAutoID() {
		return nil, store.ErrIDNotAllowed
	}

	var id string
	for {
		var err error
		id, err = s.generateID()
		if err != nil {
			return nil, err
		}

		recordPath := s.recordPath(id)
		if _, err := os.Stat(recordPath); os.IsNotExist(err) {
			break
		}
	}

	meta := recordMetaFile{
		Version:   1,
		ID:        id,
		UUID:      s.generateUUID(),
		Data:      data,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.saveRecord(id, &meta); err != nil {
		return nil, err
	}

	return &store.Record{
		ID:        meta.ID,
		UUID:      meta.UUID,
		Store:     s.name,
		Data:      meta.Data,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Version:   meta.Version,
		ETag:      s.generateETag(id, 1),
	}, nil
}

func (s *FolderStore) Update(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	meta, err := s.loadRecord(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrRecordNotFound
		}
		return nil, err
	}

	for k, v := range data {
		meta.Data[k] = v
	}
	meta.Version++
	meta.UpdatedAt = time.Now()

	if err := s.saveRecord(id, meta); err != nil {
		return nil, err
	}

	return &store.Record{
		ID:        meta.ID,
		UUID:      meta.UUID,
		Store:     s.name,
		Data:      meta.Data,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Version:   meta.Version,
		ETag:      s.generateETag(id, meta.Version),
	}, nil
}

func (s *FolderStore) Replace(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	meta, err := s.loadRecord(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrRecordNotFound
		}
		return nil, err
	}

	meta.Data = data
	meta.Version++
	meta.UpdatedAt = time.Now()

	if err := s.saveRecord(id, meta); err != nil {
		return nil, err
	}

	return &store.Record{
		ID:        meta.ID,
		UUID:      meta.UUID,
		Store:     s.name,
		Data:      meta.Data,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Version:   meta.Version,
		ETag:      s.generateETag(id, meta.Version),
	}, nil
}

func (s *FolderStore) Delete(ctx context.Context, id string) error {
	unlock := s.locker.Lock(id)
	defer unlock()

	recordPath := s.recordPath(id)
	if _, err := os.Stat(recordPath); os.IsNotExist(err) {
		return store.ErrRecordNotFound
	}

	if err := os.RemoveAll(recordPath); err != nil {
		return fmt.Errorf("delete record: %w", err)
	}

	return nil
}

func (s *FolderStore) ListFiles(ctx context.Context, recordID string) ([]store.FileInfo, error) {
	var recordPath string
	if recordID != "" {
		recordPath = s.recordPath(recordID)
	} else {
		recordPath = s.path
	}

	filesDir := filepath.Join(recordPath, FilesDirName)
	entries, err := os.ReadDir(filesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []store.FileInfo{}, nil
		}
		return nil, fmt.Errorf("read files directory: %w", err)
	}

	files := make([]store.FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		files = append(files, store.FileInfo{
			Name:        e.Name(),
			Path:        filepath.Join(filesDir, e.Name()),
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			IsDir:       e.IsDir(),
			ContentType: "application/octet-stream",
		})
	}

	return files, nil
}

func (s *FolderStore) isValidPath(name string) bool {
	clean := filepath.Clean(name)
	return !strings.Contains(clean, "..") && !strings.HasPrefix(clean, "/")
}

func (s *FolderStore) UploadFile(ctx context.Context, recordID, name string, data []byte) error {
	if !s.isValidPath(name) {
		return fmt.Errorf("invalid file path")
	}

	unlock := s.locker.Lock(recordID)
	defer unlock()

	recordPath := s.recordPath(recordID)
	filesDir := filepath.Join(recordPath, FilesDirName)

	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return fmt.Errorf("create files directory: %w", err)
	}

	filePath := filepath.Join(filesDir, name)
	if _, err := os.Stat(filePath); err == nil {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		dir := filepath.Dir(name)
		counter := 1
		for {
			newName := fmt.Sprintf("%s (Copy %d)%s", base, counter, ext)
			if dir != "." {
				newName = filepath.Join(dir, newName)
			}
			filePath = filepath.Join(filesDir, newName)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				break
			}
			counter++
		}
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (s *FolderStore) DownloadFile(ctx context.Context, recordID, name string) ([]byte, error) {
	if !s.isValidPath(name) {
		return nil, fmt.Errorf("invalid file path")
	}

	filePath := filepath.Join(s.recordPath(recordID), FilesDirName, name)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}

func (s *FolderStore) DeleteFile(ctx context.Context, recordID, name string) error {
	if !s.isValidPath(name) {
		return fmt.Errorf("invalid file path")
	}

	unlock := s.locker.Lock(recordID)
	defer unlock()

	filePath := filepath.Join(s.recordPath(recordID), FilesDirName, name)
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func (s *FolderStore) RenameFile(ctx context.Context, recordID, oldName, newName string) error {
	if oldName == newName {
		return nil
	}

	if !s.isValidPath(oldName) {
		return fmt.Errorf("invalid old file path")
	}
	if !s.isValidPath(newName) {
		return fmt.Errorf("invalid new file path")
	}

	unlock := s.locker.Lock(recordID)
	defer unlock()

	recordPath := s.recordPath(recordID)
	filesDir := filepath.Join(recordPath, FilesDirName)

	oldPath := filepath.Join(filesDir, oldName)
	newPath := filepath.Join(filesDir, newName)

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return store.ErrFileNotFound
	}

	if _, err := os.Stat(newPath); err == nil {
		return store.ErrFileExists
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}

func (s *FolderStore) ListRecordFiles(ctx context.Context, recordID string) ([]store.FileInfo, error) {
	return s.ListFiles(ctx, recordID)
}

func (s *FolderStore) ListStoreFiles(ctx context.Context) ([]store.FileInfo, error) {
	return s.ListFiles(ctx, "")
}

func (s *FolderStore) Watch(ctx context.Context, ch chan<- store.StoreEvent) error {
	_ = ctx
	_ = ch
	return errors.New("watch not implemented")
}

func (s *FolderStore) Close() error {
	return s.locker.Close()
}

var _ io.Closer = (*FolderStore)(nil)
