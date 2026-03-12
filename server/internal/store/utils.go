package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	idPatternRe   = regexp.MustCompile(`\{id(?::(\d+))?\}`)
	uuidPatternRe = regexp.MustCompile(`\{uuid(?::(\d+))?\}`)
)

type IDGenerator struct {
	patterns *regexp.Regexp
}

func NewIDGenerator() *IDGenerator {
	return &IDGenerator{}
}

func (g *IDGenerator) Generate(pattern string, counter int) (string, error) {
	counter++

	if pattern == "" {
		return fmt.Sprintf("%d", counter), nil
	}

	now := time.Now()

	id := pattern
	id = strings.ReplaceAll(id, "{YYYY}", now.Format("2006"))
	id = strings.ReplaceAll(id, "{YY}", now.Format("06"))
	id = strings.ReplaceAll(id, "{MM}", now.Format("01"))
	id = strings.ReplaceAll(id, "{DD}", now.Format("02"))
	id = strings.ReplaceAll(id, "{date}", now.Format("2006-01-02"))
	id = strings.ReplaceAll(id, "{day}", fmt.Sprintf("%03d", now.YearDay()))

	id = idPatternRe.ReplaceAllStringFunc(id, func(match string) string {
		padding := 1
		if len(match) > 4 && match[3:4] == ":" {
			if p, err := strconv.Atoi(match[4 : len(match)-1]); err == nil && p > 0 {
				padding = p
			}
		}
		return fmt.Sprintf("%0*d", padding, counter)
	})

	id = uuidPatternRe.ReplaceAllStringFunc(id, func(match string) string {
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

	return id, nil
}

func (g *IDGenerator) HasAutoID(pattern string) bool {
	return pattern != "" && idPatternRe.MatchString(pattern)
}

func GenerateUUID() string {
	return uuid.New().String()
}

func GenerateETag(id string, version int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", id, version)))
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:])[:16])
}

func SortRecords(records []Record, sortBy string, desc bool) {
	if sortBy == "" {
		return
	}

	slices.SortFunc(records, func(a, b Record) int {
		var aVal, bVal interface{}
		switch sortBy {
		case "id":
			aVal, bVal = a.ID, b.ID
		case "uuid":
			aVal, bVal = a.UUID, b.UUID
		case "createdAt":
			aVal, bVal = a.CreatedAt, b.CreatedAt
		case "updatedAt":
			aVal, bVal = a.UpdatedAt, b.UpdatedAt
		default:
			aVal, bVal = a.Data[sortBy], b.Data[sortBy]
		}
		if desc {
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
