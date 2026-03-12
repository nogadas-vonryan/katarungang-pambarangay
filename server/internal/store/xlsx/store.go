package xlsx

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
	"github.com/xuri/excelize/v2"
)

const (
	StoreMetaFileName = ".store.json"
)

var xlsxIDPatternRe = regexp.MustCompile(`\{id(?::(\d+))?\}`)

type XLSXStore struct {
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

func init() {
	store.RegisterStoreType("xlsx", New)
}

func New(path string, metadata *store.StoreMetadata) (store.Store, error) {
	if metadata == nil {
		metadata = &store.StoreMetadata{
			Name:          filepath.Base(path),
			Type:          "xlsx",
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

	dataFile := metadata.Name + ".xlsx"
	dataPath := filepath.Join(absPath, dataFile)

	xs := &XLSXStore{
		name:     metadata.Name,
		path:     absPath,
		metadata: metadata,
		locker:   store.NewRecordLocker(5 * time.Minute),
	}

	if err := xs.loadStoreMeta(); err != nil {
		return nil, err
	}

	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		if err := xs.initXLSX(dataPath); err != nil {
			return nil, err
		}
	}

	return xs, nil
}

func (s *XLSXStore) Name() string {
	return s.name
}

func (s *XLSXStore) Type() string {
	return "xlsx"
}

func (s *XLSXStore) Path() string {
	return s.path
}

func (s *XLSXStore) Metadata() (*store.StoreMetadata, error) {
	return s.metadata, nil
}

func (s *XLSXStore) dataFilePath() string {
	return filepath.Join(s.path, s.name+".xlsx")
}

func (s *XLSXStore) loadStoreMeta() error {
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

func (s *XLSXStore) saveStoreMeta() error {
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

func (s *XLSXStore) initXLSX(path string) error {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", "Records")

	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("create xlsx: %w", err)
	}
	return nil
}

func (s *XLSXStore) openXLSX() (*excelize.File, error) {
	return excelize.OpenFile(s.dataFilePath())
}

func (s *XLSXStore) saveXLSX(f *excelize.File) error {
	return f.Save()
}

var (
	xlsxIDRe   = regexp.MustCompile(`\{id(?::(\d+))?\}`)
	xlsxUUIDRe = regexp.MustCompile(`\{uuid(?::(\d+))?\}`)
)

func (s *XLSXStore) generateID() (string, error) {
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

	id = xlsxIDRe.ReplaceAllStringFunc(id, func(match string) string {
		padding := 1
		if len(match) > 4 && match[3:4] == ":" {
			if p, err := strconv.Atoi(match[4 : len(match)-1]); err == nil && p > 0 {
				padding = p
			}
		}
		return fmt.Sprintf("%0*d", padding, s.metadata.Counter)
	})

	id = xlsxUUIDRe.ReplaceAllStringFunc(id, func(match string) string {
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

func (s *XLSXStore) generateUUID() string {
	return uuid.New().String()
}

func (s *XLSXStore) generateETag(id string, version int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, version)))
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:])[:16])
}

func (s *XLSXStore) hasAutoID() bool {
	return s.metadata.NamingPattern != "" && xlsxIDPatternRe.MatchString(s.metadata.NamingPattern)
}

func (s *XLSXStore) readRecords() ([]store.Record, error) {
	f, err := s.openXLSX()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := f.GetRows("Records")
	if err != nil {
		return nil, err
	}

	if len(rows) < 1 {
		return []store.Record{}, nil
	}

	headers := rows[0]
	records := make([]store.Record, 0, len(rows)-1)

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) != len(headers) {
			continue
		}

		recordData := make(map[string]interface{})
		for j, header := range headers {
			recordData[header] = row[j]
		}

		id, _ := recordData["_id"].(string)
		uuid, _ := recordData["_uuid"].(string)

		var createdAt, updatedAt time.Time
		if createdStr, ok := recordData["_createdAt"].(string); ok {
			createdAt, _ = time.Parse(time.RFC3339, createdStr)
		}
		if updatedStr, ok := recordData["_updatedAt"].(string); ok {
			updatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		}

		version := 1
		if versionStr, ok := recordData["_version"].(string); ok {
			fmt.Sscanf(versionStr, "%d", &version)
		}

		records = append(records, store.Record{
			ID:        id,
			UUID:      uuid,
			Store:     s.name,
			Data:      recordData,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			Version:   version,
			ETag:      s.generateETag(id, version),
		})
	}

	return records, nil
}

func (s *XLSXStore) writeRecords(records []store.Record) error {
	f, err := s.openXLSX()
	if err != nil {
		return err
	}
	defer f.Close()

	f.DeleteSheet("Records")
	f.NewSheet("Records")

	headers := []string{"_id", "_uuid", "_createdAt", "_updatedAt", "_version"}
	seenHeaders := make(map[string]bool)
	for _, h := range headers {
		seenHeaders[h] = true
	}

	for _, record := range records {
		for key := range record.Data {
			if !seenHeaders[key] {
				seenHeaders[key] = true
				headers = append(headers, key)
			}
		}
	}

	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue("Records", cell, header)
	}

	for rowIdx, record := range records {
		row := make([]string, len(headers))
		row[0] = record.ID
		row[1] = record.UUID
		row[2] = record.CreatedAt.Format(time.RFC3339)
		row[3] = record.UpdatedAt.Format(time.RFC3339)
		row[4] = fmt.Sprintf("%d", record.Version)

		for i, header := range headers {
			if i > 4 {
				if val, ok := record.Data[header]; ok {
					row[i] = fmt.Sprintf("%v", val)
				}
			}
		}

		for col, value := range row {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx+2)
			f.SetCellValue("Records", cell, value)
		}
	}

	return s.saveXLSX(f)
}

func (s *XLSXStore) List(ctx context.Context, opts store.ListOptions) ([]store.Record, int, error) {
	unlock := s.locker.RLock("list")
	defer unlock()

	records, err := s.readRecords()
	if err != nil {
		return nil, 0, err
	}

	total := len(records)

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

func (s *XLSXStore) Get(ctx context.Context, id string) (*store.Record, error) {
	unlock := s.locker.RLock(id)
	defer unlock()

	records, err := s.readRecords()
	if err != nil {
		return nil, err
	}

	for _, r := range records {
		if r.ID == id {
			return &r, nil
		}
	}

	return nil, store.ErrRecordNotFound
}

func (s *XLSXStore) Create(ctx context.Context, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock("create")
	defer unlock()

	userID, hasUserID := data["_id"].(string)
	if hasUserID {
		delete(data, "_id")

		existing, _ := s.Get(ctx, userID)
		if existing != nil {
			return nil, store.ErrRecordExists
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

		records, err := s.readRecords()
		if err != nil {
			return nil, err
		}

		records = append(records, record)
		if err := s.writeRecords(records); err != nil {
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

	records, err := s.readRecords()
	if err != nil {
		return nil, err
	}

	records = append(records, record)
	if err := s.writeRecords(records); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *XLSXStore) Update(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	records, err := s.readRecords()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range records {
		if r.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return nil, store.ErrRecordNotFound
	}

	oldRecord := records[idx]
	for k, v := range data {
		oldRecord.Data[k] = v
	}
	oldRecord.Version++
	oldRecord.UpdatedAt = time.Now()
	oldRecord.ETag = s.generateETag(id, oldRecord.Version)

	records[idx] = oldRecord

	if err := s.writeRecords(records); err != nil {
		return nil, err
	}

	return &oldRecord, nil
}

func (s *XLSXStore) Replace(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	records, err := s.readRecords()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range records {
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
		UUID:      records[idx].UUID,
		Store:     s.name,
		Data:      data,
		CreatedAt: records[idx].CreatedAt,
		UpdatedAt: now,
		Version:   records[idx].Version + 1,
		ETag:      s.generateETag(id, records[idx].Version+1),
	}

	records[idx] = record

	if err := s.writeRecords(records); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *XLSXStore) Delete(ctx context.Context, id string) error {
	unlock := s.locker.Lock(id)
	defer unlock()

	records, err := s.readRecords()
	if err != nil {
		return err
	}

	idx := -1
	for i, r := range records {
		if r.ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		return store.ErrRecordNotFound
	}

	records = append(records[:idx], records[idx+1:]...)
	return s.writeRecords(records)
}

func (s *XLSXStore) ListRecordFiles(ctx context.Context, recordID string) ([]store.FileInfo, error) {
	return []store.FileInfo{}, nil
}

func (s *XLSXStore) UploadFile(ctx context.Context, recordID string, name string, data []byte) error {
	return fmt.Errorf("files not supported in XLSX store")
}

func (s *XLSXStore) DownloadFile(ctx context.Context, recordID string, name string) ([]byte, error) {
	return nil, fmt.Errorf("files not supported in XLSX store")
}

func (s *XLSXStore) DeleteFile(ctx context.Context, recordID string, name string) error {
	return fmt.Errorf("files not supported in XLSX store")
}

func (s *XLSXStore) Watch(ctx context.Context, ch chan<- store.StoreEvent) error {
	_ = ctx
	_ = ch
	return errors.New("watch not implemented")
}

func (s *XLSXStore) Close() error {
	return s.locker.Close()
}

var _ io.Closer = (*XLSXStore)(nil)
