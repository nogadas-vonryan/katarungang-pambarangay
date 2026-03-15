package backup

const (
	BackupDirName    = ".backups"
	ConfigDirName    = ".config"
	ManifestFileName = "_manifest.json"
	IndexFileName    = "index.json"
	ExtractTempDir   = ".extract-temp"
	RestoreTempDir   = ".restore-temp"
	ManifestVersion  = 1
)

var FullBackupExclusions = []string{BackupDirName, ConfigDirName}

func IsExcluded(name string) bool {
	for _, ex := range FullBackupExclusions {
		if name == ex {
			return true
		}
	}
	return false
}
