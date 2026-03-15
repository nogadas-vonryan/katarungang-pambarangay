package backup

import "time"

type BackupMeta struct {
	Name        string    `json:"name"`
	Scope       string    `json:"scope"`
	ScopeType   string    `json:"scopeType"`
	Size        int64     `json:"size"`
	Timestamp   time.Time `json:"timestamp"`
	RecordCount int       `json:"recordCount"`
	CreatedBy   string    `json:"createdBy,omitempty"`
}

type RestoreOptions struct {
	Mode   string
	DryRun bool
	Force  bool
}

type BackupResult struct {
	FilesProcessed int
	TotalFiles     int
	BytesWritten   int64
	RecordCount    int
	BackupName     string
}

type DryRunResult struct {
	WouldDelete []string
	WouldCreate []string
	WouldUpdate []string
	RecordCount int
	StoreType   string
}

type Manifest struct {
	Scope     string    `json:"scope"`
	ScopeType string    `json:"scopeType"`
	Timestamp time.Time `json:"timestamp"`
	StoreType string    `json:"storeType,omitempty"`
	Version   int       `json:"version"`
}

type Index struct {
	Backups []BackupMeta `json:"backups"`
}

type StoreInfo struct {
	Name string
	Path string
	Type string
}
