package sdk

import (
	"sdk_version_control/internal/config"
)

// Registry SDK 注册表，管理所有 SDK 的 fetcher
type Registry struct {
	fetchers map[SdkType]VersionFetcher
}

// NewRegistry 创建并注册所有 SDK fetcher
func NewRegistry(cfg *config.Config, sm *config.SettingsManager) *Registry {
	r := &Registry{
		fetchers: make(map[SdkType]VersionFetcher),
	}
	// 运行时 & 语言
	r.Register(NewNodejsFetcher(cfg, sm))
	r.Register(NewJdkFetcher(cfg, sm))
	r.Register(NewGolangFetcher(cfg, sm))
	r.Register(NewPythonFetcher(cfg, sm))
	r.Register(NewRustFetcher(cfg, sm))
	r.Register(NewRubyFetcher(cfg, sm))
	r.Register(NewDotNetFetcher(cfg, sm))
	r.Register(NewPHPFetcher(cfg, sm))
	r.Register(NewPerlFetcher(cfg, sm))
	// 构建工具
	r.Register(NewMavenFetcher(cfg, sm))
	r.Register(NewGradleFetcher(cfg, sm))
	// 移动开发
	r.Register(NewFlutterFetcher(cfg, sm))
	r.Register(NewAndroidFetcher(cfg, sm))
	r.Register(NewDartFetcher(cfg, sm))
	return r
}

// Register 注册一个 SDK fetcher
func (r *Registry) Register(f VersionFetcher) {
	r.fetchers[f.Type()] = f
}

// Get 获取指定 SDK 的 fetcher
func (r *Registry) Get(t SdkType) VersionFetcher {
	return r.fetchers[t]
}

// All 获取所有 SDK fetcher（按顺序）
func (r *Registry) All() []VersionFetcher {
	types := AllSdkTypes()
	result := make([]VersionFetcher, 0, len(types))
	for _, t := range types {
		if f, ok := r.fetchers[t]; ok {
			result = append(result, f)
		}
	}
	return result
}
