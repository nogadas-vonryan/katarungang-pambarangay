package json

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	StoreMetaFileName = ".store.json"
)

type JSONStore struct {
	name     string
	path     string
	metadata *store.StoreMetadata
	locker   *store.RecordLocker
	logger   interface{}
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

type recordsFile struct {
	Version int            `json:"version"`
	Records []store.Record `json:"records"`
}

func init() {
	store.RegisterStoreType("json", New)
}

func New(path string, metadata *store.StoreMetadata) (store.Store, error) {
	if metadata == nil {
		metadata = &store.StoreMetadata{
			Name:          filepath.Base(path),
			Type:          "json",
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

	dataFile := metadata.Name + ".json"
	dataPath := filepath.Join(absPath, dataFile)

	js := &JSONStore{
		name:     metadata.Name,
		path:     absPath,
		metadata: metadata,
		locker:   store.NewRecordLocker(5 * time.Minute),
	}

	if err := js.loadStoreMeta(); err != nil {
		return nil, err
	}

	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		if err := js.initJSON(dataPath); err != nil {
			return nil, err
		}
	}

	return js, nil
}

func (s *JSONStore) Name() string {
	return s.name
}

func (s *JSONStore) Type() string {
	return "json"
}

func (s *JSONStore) Path() string {
	return s.path
}

func (s *JSONStore) Metadata() (*store.StoreMetadata, error) {
	return s.metadata, nil
}

func (s *JSONStore) dataFilePath() string {
	return filepath.Join(s.path, s.name+".json")
}

func (s *JSONStore) loadStoreMeta() error {
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

func (s *JSONStore) saveStoreMeta() error {
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

func (s *JSONStore) initJSON(path string) error {
	rf := recordsFile{
		Version: 1,
		Records: []store.Record{},
	}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal records: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (s *JSONStore) loadRecords() (*recordsFile, error) {
	data, err := os.ReadFile(s.dataFilePath())
	if errors.Is(err, os.ErrNotExist) {
		return &recordsFile{Version: 1, Records: []store.Record{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read records: %w", err)
	}

	var rf recordsFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse records: %w", err)
	}

	return &rf, nil
}

func (s *JSONStore) saveRecords(rf *recordsFile) error {
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal records: %w", err)
	}

	tmpPath := s.dataFilePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write records temp: %w", err)
	}

	if err := os.Rename(tmpPath, s.dataFilePath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename records: %w", err)
	}

	return nil
}

var (
	jsonIDRe   = regexp.MustCompile(`\{id(?::(\d+))?\}`)
	jsonUUIDRe = regexp.MustCompile(`\{uuid(?::(\d+))?\}`)
)

func (s *JSONStore) generateID() (string, error) {
	s.metadata.Counter++

	pattern := s.metadata.NamingPattern
	if pattern == "" {
		return fmt.Sprintf("%d", s.metadata.Counter), nil
	}

	now := time.Now()

	id := pattern
	id = strings.ReplaceAll(id, "{YYYY}", now.Format("2006"))
	id = strings.ReplaceAll(id, "{YY}", now.Format("06"))
	id = strings.ReplaceAll(id, "{MM}", now.Format("01"))
	id = strings.ReplaceAll(id, "{DD}", now.Format("02"))
	id = strings.ReplaceAll(id, "{date}", now.Format("2006-01-02"))
	id = strings.ReplaceAll(id, "{day}", fmt.Sprintf("%03d", now.YearDay()))

	id = jsonIDRe.ReplaceAllStringFunc(id, func(match string) string {
		padding := 1
		if len(match) > 4 && match[3:4] == ":" {
			if p, err := strconv.Atoi(match[4 : len(match)-1]); err == nil && p > 0 {
				padding = p
			}
		}
		return fmt.Sprintf("%0*d", padding, s.metadata.Counter)
	})

	id = jsonUUIDRe.ReplaceAllStringFunc(id, func(match string) string {
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

func (s *JSONStore) generateUUID() string {
	return uuid.New().String()
}

func (s *JSONStore) generateETag(id string, version int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, version)))
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:])[:16])
}

func (s *JSONStore) hasAutoID() bool {
	return s.metadata.NamingPattern != "" && strings.Contains(s.metadata.NamingPattern, "{id}")
}

func (s *JSONStore) List(ctx context.Context, opts store.ListOptions) ([]store.Record, int, error) {
	unlock := s.locker.RLock("list")
	defer unlock()

	rf, err := s.loadRecords()
	if err != nil {
		return nil, 0, err
	}

	total := len(rf.Records)
	records := make([]store.Record, total)
	copy(records, rf.Records)

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

	return records, total, nil
}

func (s *JSONStore) Get(ctx context.Context, id string) (*store.Record, error) {
	unlock := s.locker.RLock(id)
	defer unlock()

	rf, err := s.loadRecords()
	if err != nil {
		return nil, err
	}

	for _, r := range rf.Records {
		if r.ID == id {
			return &r, nil
		}
	}

	return nil, store.ErrRecordNotFound
}

func (s *JSONStore) Create(ctx context.Context, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock("create")
	defer unlock()

	userID, hasUserID := data["_id"].(string)
	if hasUserID {
		delete(data, "_id")

		rf, _ := s.loadRecords()
		for _, r := range rf.Records {
			if r.ID == userID {
				return nil, store.ErrRecordExists
			}
		}

		now := time.Now()
		record := store.Record{
			ID:        userID,
			UUID:      s.generateUUID(),
			Store:     s.name,
			Data:      data,
			CreatedAt: now,
			UpdatedAt: now,
			Version:   1,
			ETag:      s.generateETag(userID, 1),
		}

		rf.Records = append(rf.Records, record)
		if err := s.saveRecords(rf); err != nil {
			return nil, err
		}

		return &record, nil
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

		existing, _ := s.Get(ctx, id)
		if existing == nil {
			break
		}
	}

	now := time.Now()
	record := store.Record{
		ID:        id,
		UUID:      s.generateUUID(),
		Store:     s.name,
		Data:      data,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
		ETag:      s.generateETag(id, 1),
	}

	rf, err := s.loadRecords()
	if err != nil {
		return nil, err
	}

	rf.Records = append(rf.Records, record)
	if err := s.saveRecords(rf); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *JSONStore) Update(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	rf, err := s.loadRecords()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range rf.Records {
		if r.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return nil, store.ErrRecordNotFound
	}

	oldRecord := rf.Records[idx]
	for k, v := range data {
		oldRecord.Data[k] = v
	}
	oldRecord.Version++
	oldRecord.UpdatedAt = time.Now()
	oldRecord.ETag = s.generateETag(id, oldRecord.Version)

	rf.Records[idx] = oldRecord
	if err := s.saveRecords(rf); err != nil {
		return nil, err
	}

	return &oldRecord, nil
}

func (s *JSONStore) Replace(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	rf, err := s.loadRecords()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range rf.Records {
		if r.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return nil, store.ErrRecordNotFound
	}

	now := time.Now()
	record := store.Record{
		ID:        id,
		UUID:      rf.Records[idx].UUID,
		Store:     s.name,
		Data:      data,
		CreatedAt: rf.Records[idx].CreatedAt,
		UpdatedAt: now,
		Version:   rf.Records[idx].Version + 1,
		ETag:      s.generateETag(id, rf.Records[idx].Version+1),
	}

	rf.Records[idx] = record
	if err := s.saveRecords(rf); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *JSONStore) Delete(ctx context.Context, id string) error {
	unlock := s.locker.Lock(id)
	defer unlock()

	rf, err := s.loadRecords()
	if err != nil {
		return err
	}

	idx := -1
	for i, r := range rf.Records {
		if r.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return store.ErrRecordNotFound
	}

	rf.Records = append(rf.Records[:idx], rf.Records[idx+1:]...)
	return s.saveRecords(rf)
}

func (s *JSONStore) ListRecordFiles(ctx context.Context, recordID string) ([]store.FileInfo, error) {
	return []store.FileInfo{}, nil
}

func (s *JSONStore) UploadFile(ctx context.Context, recordID string, name string, data []byte) error {
	return fmt.Errorf("files not supported in JSON store")
}

func (s *JSONStore) DownloadFile(ctx context.Context, recordID string, name string) ([]byte, error) {
	return nil, fmt.Errorf("files not supported in JSON store")
}

func (s *JSONStore) DeleteFile(ctx context.Context, recordID string, name string) error {
	return fmt.Errorf("files not supported in JSON store")
}

func (s *JSONStore) Watch(ctx context.Context, ch chan<- store.StoreEvent) error {
	_ = ctx
	_ = ch
	return errors.New("watch not implemented")
}

func (s *JSONStore) Close() error {
	return s.locker.Close()
}

var _ io.Closer = (*JSONStore)(nil)
