package csv

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
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

var csvIDPatternRe = regexp.MustCompile(`\{id(?::(\d+))?\}`)

type CSVStore struct {
	name     string
	path     string
	metadata *store.StoreMetadata
	locker   *store.RecordLocker
}

func init() {
	store.RegisterStoreType("csv", New)
}

func New(path string, metadata *store.StoreMetadata) (store.Store, error) {
	if metadata == nil {
		metadata = &store.StoreMetadata{
			Name:          filepath.Base(path),
			Type:          "csv",
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

	dataFile := metadata.Name + ".csv"
	dataPath := filepath.Join(absPath, dataFile)

	cs := &CSVStore{
		name:     metadata.Name,
		path:     absPath,
		metadata: metadata,
		locker:   store.NewRecordLocker(5 * time.Minute),
	}

	if err := cs.loadStoreMeta(); err != nil {
		return nil, err
	}

	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		if err := cs.initCSV(dataPath); err != nil {
			return nil, err
		}
	}

	return cs, nil
}

func (s *CSVStore) Name() string {
	return s.name
}

func (s *CSVStore) Type() string {
	return "csv"
}

func (s *CSVStore) Path() string {
	return s.path
}

func (s *CSVStore) Metadata() (*store.StoreMetadata, error) {
	return s.metadata, nil
}

func (s *CSVStore) dataFilePath() string {
	return filepath.Join(s.path, s.name+".csv")
}

func (s *CSVStore) loadStoreMeta() error {
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

func (s *CSVStore) saveStoreMeta() error {
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

func (s *CSVStore) initCSV(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	writer.Comma = ','
	writer.Write([]string{})
	writer.Flush()

	return nil
}

func (s *CSVStore) readCSV() ([][]string, error) {
	file, err := os.Open(s.dataFilePath())
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}

	return records, nil
}

func (s *CSVStore) writeCSV(records [][]string) error {
	file, err := os.Create(s.dataFilePath())
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	writer.Comma = ','

	if err := writer.WriteAll(records); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}

	return nil
}

var (
	csvIDRe   = regexp.MustCompile(`\{id(?::(\d+))?\}`)
	csvUUIDRe = regexp.MustCompile(`\{uuid(?::(\d+))?\}`)
)

func (s *CSVStore) generateID() (string, error) {
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

	id = csvIDRe.ReplaceAllStringFunc(id, func(match string) string {
		padding := 1
		if len(match) > 4 && match[3:4] == ":" {
			if p, err := strconv.Atoi(match[4 : len(match)-1]); err == nil && p > 0 {
				padding = p
			}
		}
		return fmt.Sprintf("%0*d", padding, s.metadata.Counter)
	})

	id = csvUUIDRe.ReplaceAllStringFunc(id, func(match string) string {
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

func (s *CSVStore) generateUUID() string {
	return uuid.New().String()
}

func (s *CSVStore) generateETag(id string, version int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, version)))
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:])[:16])
}

func (s *CSVStore) hasAutoID() bool {
	return s.metadata.NamingPattern != "" && csvIDPatternRe.MatchString(s.metadata.NamingPattern)
}

func (s *CSVStore) csvToRecords(data [][]string) ([]store.Record, error) {
	if len(data) < 2 {
		return []store.Record{}, nil
	}

	headers := data[0]
	records := make([]store.Record, 0, len(data)-1)

	for i := 1; i < len(data); i++ {
		row := data[i]
		if len(row) != len(headers) {
			continue
		}

		recordData := make(map[string]interface{})
		for j, header := range headers {
			recordData[header] = row[j]
		}

		id, _ := recordData["_id"].(string)
		uuid, _ := recordData["_uuid"].(string)

		records = append(records, store.Record{
			ID:        id,
			UUID:      uuid,
			Store:     s.name,
			Data:      recordData,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Version:   1,
			ETag:      s.generateETag(id, 1),
		})
	}

	return records, nil
}

func (s *CSVStore) recordsToCSV(records []store.Record) ([][]string, error) {
	if len(records) == 0 {
		return [][]string{{}}, nil
	}

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

	result := [][]string{headers}

	for _, record := range records {
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

		result = append(result, row)
	}

	return result, nil
}

func (s *CSVStore) List(ctx context.Context, opts store.ListOptions) ([]store.Record, int, error) {
	unlock := s.locker.RLock("list")
	defer unlock()

	data, err := s.readCSV()
	if err != nil {
		return nil, 0, err
	}

	records, err := s.csvToRecords(data)
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

func (s *CSVStore) Get(ctx context.Context, id string) (*store.Record, error) {
	unlock := s.locker.RLock(id)
	defer unlock()

	data, err := s.readCSV()
	if err != nil {
		return nil, err
	}

	records, err := s.csvToRecords(data)
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

func (s *CSVStore) Create(ctx context.Context, data map[string]interface{}) (*store.Record, error) {
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

		if err := s.appendRecord(record); err != nil {
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

	if err := s.appendRecord(record); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *CSVStore) appendRecord(record store.Record) error {
	data, err := s.readCSV()
	if err != nil {
		return err
	}

	records, err := s.csvToRecords(data)
	if err != nil {
		return err
	}

	records = append(records, record)
	newData, err := s.recordsToCSV(records)
	if err != nil {
		return err
	}

	return s.writeCSV(newData)
}

func (s *CSVStore) Update(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	csvData, err := s.readCSV()
	if err != nil {
		return nil, err
	}

	records, err := s.csvToRecords(csvData)
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

	newData, err := s.recordsToCSV(records)
	if err != nil {
		return nil, err
	}

	if err := s.writeCSV(newData); err != nil {
		return nil, err
	}

	return &oldRecord, nil
}

func (s *CSVStore) Replace(ctx context.Context, id string, data map[string]interface{}) (*store.Record, error) {
	unlock := s.locker.Lock(id)
	defer unlock()

	csvData, err := s.readCSV()
	if err != nil {
		return nil, err
	}

	records, err := s.csvToRecords(csvData)
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

	newData, err := s.recordsToCSV(records)
	if err != nil {
		return nil, err
	}

	if err := s.writeCSV(newData); err != nil {
		return nil, err
	}

	return &record, nil
}

func (s *CSVStore) Delete(ctx context.Context, id string) error {
	unlock := s.locker.Lock(id)
	defer unlock()

	csvData, err := s.readCSV()
	if err != nil {
		return err
	}

	records, err := s.csvToRecords(csvData)
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

	newData, err := s.recordsToCSV(records)
	if err != nil {
		return err
	}

	return s.writeCSV(newData)
}

func (s *CSVStore) ListRecordFiles(ctx context.Context, recordID string) ([]store.FileInfo, error) {
	return []store.FileInfo{}, nil
}

func (s *CSVStore) UploadFile(ctx context.Context, recordID string, name string, data []byte) error {
	return fmt.Errorf("files not supported in CSV store")
}

func (s *CSVStore) DownloadFile(ctx context.Context, recordID string, name string) ([]byte, error) {
	return nil, fmt.Errorf("files not supported in CSV store")
}

func (s *CSVStore) DeleteFile(ctx context.Context, recordID string, name string) error {
	return fmt.Errorf("files not supported in CSV store")
}

func (s *CSVStore) RenameFile(ctx context.Context, recordID, oldName, newName string) error {
	return fmt.Errorf("files not supported in CSV store")
}

func (s *CSVStore) Watch(ctx context.Context, ch chan<- store.StoreEvent) error {
	_ = ctx
	_ = ch
	return errors.New("watch not implemented")
}

func (s *CSVStore) Close() error {
	return s.locker.Close()
}

var _ io.Closer = (*CSVStore)(nil)
