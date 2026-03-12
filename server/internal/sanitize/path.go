package sanitize

import (
	"path/filepath"
	"strings"
)

func Path(name string) bool {
	if strings.Contains(name, "..") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	return true
}

func RecordID(id string) bool {
	if strings.Contains(id, "..") {
		return false
	}
	clean := filepath.Clean(id)
	return !strings.Contains(clean, "..") && !strings.HasPrefix(clean, "/")
}

func FileName(name string) bool {
	badNames := []string{".store.json", ".meta.json"}
	for _, bad := range badNames {
		if name == bad {
			return false
		}
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	return true
}
