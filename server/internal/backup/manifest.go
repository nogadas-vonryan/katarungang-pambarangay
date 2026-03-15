package backup

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"time"
)

func WriteManifestFile(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ReadManifest(rc io.ReadCloser) (*Manifest, error) {
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, ErrInvalidManifest
	}
	return &m, nil
}

func NewManifest(scope, scopeType, storeType string) *Manifest {
	return &Manifest{
		Scope:     scope,
		ScopeType: scopeType,
		Timestamp: time.Now().UTC(),
		StoreType: storeType,
		Version:   ManifestVersion,
	}
}

func ReadManifestFromZIP(zipPath string) (*Manifest, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, ErrBackupCorrupted
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == ManifestFileName {
			rc, err := f.Open()
			if err != nil {
				return nil, ErrBackupCorrupted
			}
			return ReadManifest(rc)
		}
	}
	return nil, ErrInvalidManifest
}
