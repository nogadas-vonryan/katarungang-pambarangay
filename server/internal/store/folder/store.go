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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kp-cms/server/internal/store"
)

const (
	StoreMetaFileName  = ".store.json"
	RecordMetaFileName = ".record.json"
	FilesDirName       = "files"
)

type FolderStore struct {
	name     string
	path     string
	metadata *store.StoreMetadata
	locker   *store.RecordLocker
	logger   *slog.Logger
}

type storeMetaFile struct {
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
		logger:   slog.Default(),
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

	var meta storeMetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("parse store meta: %w", err)
	}

	s.metadata.Name = meta.Name
	s.metadata.Schema = meta.Schema
	s.metadata.NamingPattern = meta.NamingPattern
	s.metadata.Counter = meta.Counter
	s.metadata.CreatedAt = meta.CreatedAt
	s.metadata.UpdatedAt = meta.UpdatedAt

	return nil
}

func (s *FolderStore) saveStoreMeta() error {
	meta := storeMetaFile{
		Version:       1,
		Name:          s.metadata.Name,
		Type:          s.metadata.Type,
		Path:          s.metadata.Path,
		Schema:        s.metadata.Schema,
		NamingPattern: s.metadata.NamingPattern,
		Counter:       s.metadata.Counter,
		CreatedAt:     s.metadata.CreatedAt,
		UpdatedAt:     time.Now(),
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}

	data, err := json.MarshalIndent(meta, "", "  ")
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

var (
	idRe   = regexp.MustCompile(`\{id(?::(\d+))?\}`)
	uuidRe = regexp.MustCompile(`\{uuid(?::(\d+))?\}`)
)

func (s *FolderStore) generateID() (string, error) {
	s.metadata.Counter++

	pattern := s.metadata.NamingPattern
	if pattern == "" {
		hash := sha256.Sum256([]byte(time.Now().Format(time.RFC3339Nano)))
		return hex.EncodeToString(hash[:])[:16], nil
	}

	now := time.Now()

	id := pattern
	id = strings.ReplaceAll(id, "{YYYY}", now.Format("2006"))
	id = strings.ReplaceAll(id, "{YY}", now.Format("06"))
	id = strings.ReplaceAll(id, "{MM}", now.Format("01"))
	id = strings.ReplaceAll(id, "{DD}", now.Format("02"))
	id = strings.ReplaceAll(id, "{date}", now.Format("2006-01-02"))
	id = strings.ReplaceAll(id, "{day}", fmt.Sprintf("%03d", now.YearDay()))

	id = idRe.ReplaceAllStringFunc(id, func(match string) string {
		padding := 1
		if len(match) > 4 && match[3:4] == ":" {
			if p, err := strconv.Atoi(match[4 : len(match)-1]); err == nil && p > 0 {
				padding = p
			}
		}
		return fmt.Sprintf("%0*d", padding, s.metadata.Counter)
	})

	id = uuidRe.ReplaceAllStringFunc(id, func(match string) string {
		length := 8
		if len(match) > 7 && match[6:7] == ":" {
			if l, err := strconv.Atoi(match[7 : len(match)-1]); err == nil && l > 0 {
				length = l
			}
		}
		u := uuid.New().String()
		if length < len(u) {
			u = u[:length]
		}
		return u
	})

	if err := s.saveStoreMeta(); err != nil {
		return "", err
	}

	return id, nil
}

func (s *FolderStore) generateUUID() string {
	return uuid.New().String()
}

func (s *FolderStore) generateETag(id string, version int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, version)))
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:])[:16])
}

var hasIDPatternRe = regexp.MustCompile(`\{id(?::\d+)?\}`)

func (s *FolderStore) hasAutoID() bool {
	return s.metadata.NamingPattern != "" && hasIDPatternRe.MatchString(s.metadata.NamingPattern)
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

func (s *FolderStore) List(ctx context.Context, opts store.ListOptions) ([]store.Record, error) {
	unlock := s.locker.RLock("list")
	defer unlock()

	ids, err := s.scanRecords()
	if err != nil {
		return nil, err
	}

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

	if opts.SortBy != "" {
		slices.SortFunc(records, func(a, b store.Record) int {
			var aVal, bVal interface{}
			switch opts.SortBy {
			case "id":
				aVal, bVal = a.ID, b.ID
			case "uuid":
				aVal, bVal = a.UUID, b.UUID
			case "createdAt":
				aVal, bVal = a.CreatedAt, b.CreatedAt
			case "updatedAt":
				aVal, bVal = a.UpdatedAt, b.UpdatedAt
			default:
				aVal, bVal = a.Data[opts.SortBy], b.Data[opts.SortBy]
			}
			if opts.SortDesc {
				aVal, bVal = bVal, aVal
			}
			if aVal == bVal {
				return 0
			}
			if aVal == nil {
				return -1
			}
			if bVal == nil {
				return 1
			}
			return strings.Compare(fmt.Sprint(aVal), fmt.Sprint(bVal))
		})
	}

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

	return records, nil
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
