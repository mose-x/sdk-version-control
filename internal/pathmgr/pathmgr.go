package pathmgr

// PathEntry describes a PATH entry
type PathEntry struct {
	Path      string `json:"path"`
	IsManaged bool   `json:"isManaged"` // whether under .svc
	SdkType   string `json:"sdkType"`   // identified SDK type (empty = unknown)
	Source    string `json:"source"`    // "user", "system", or "external"
}

// PathManager manages system PATH environment variables
type PathManager interface {
	// ConfigureSdk adds the specified SDK version's bin directory to PATH (persistent)
	ConfigureSdk(sdkType string, versionDir string, binDir string, extraEnvVars map[string]string) error
	// RemoveSdk removes the specified SDK from PATH
	RemoveSdk(sdkType string, extraEnvVars map[string]string) error
	// GetCurrentConfig returns the PATH entries currently managed by SVC
	GetCurrentConfig() (map[string]string, error)
	// GetAllPathEntries returns all PATH entries
	GetAllPathEntries() ([]PathEntry, error)
	// CleanExternalPaths cleans non-SVC-managed external PATH entries matching the same SDK type and version
	// sourcePath is the specific source path of the import; matched entries are removed first
	CleanExternalPaths(sdkType string, version string, sourcePath string)
	// DetectSystemConflicts checks whether the system level contains env var configs matching the SDK
	// Returns the list of conflicting entries; empty list means no conflicts
	DetectSystemConflicts(sdkType string, envKeys []string) []string
}
