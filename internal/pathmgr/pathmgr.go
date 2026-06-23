package pathmgr

// PathEntry 描述一个 PATH 条目
type PathEntry struct {
	Path      string `json:"path"`
	IsManaged bool   `json:"isManaged"` // 是否在 .svc 下
	SdkType   string `json:"sdkType"`   // 识别出的 SDK 类型（空=未知）
}

// PathManager 管理系统 PATH 环境变量
type PathManager interface {
	// ConfigureSdk 将指定 SDK 版本的 bin 目录添加到 PATH（持久化）
	ConfigureSdk(sdkType string, versionDir string, binDir string, extraEnvVars map[string]string) error

	// RemoveSdk 将指定 SDK 从 PATH 中移除
	RemoveSdk(sdkType string, extraEnvVars map[string]string) error

	// GetCurrentConfig 获取当前 SVC 管理的 PATH 条目
	GetCurrentConfig() (map[string]string, error) // envVar -> value

	// GetAllPathEntries 获取所有 PATH 条目
	GetAllPathEntries() ([]PathEntry, error)

	// CleanExternalPaths 清理非 SVC 管理的、匹配同 SDK 类型和版本的外部 PATH 条目
	// sourcePath 是导入来源的具体路径，会优先匹配移除
	CleanExternalPaths(sdkType string, version string, sourcePath string) error

	// DetectSystemConflicts 检测系统级是否有匹配该 SDK 的环境变量配置
	// 返回冲突的条目列表，空列表表示无冲突
	DetectSystemConflicts(sdkType string, extraEnvVarKeys []string) []string
}
