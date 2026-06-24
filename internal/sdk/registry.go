package sdk

import (
	"sdk_version_control/internal/config"
)

// Registry holds all SDK fetchers
type Registry struct {
	fetchers map[SdkType]VersionFetcher
}

// NewRegistry creates and registers all SDK fetchers
func NewRegistry(cfg *config.Config, sm *config.SettingsManager) *Registry {
	r := &Registry{
		fetchers: make(map[SdkType]VersionFetcher),
	}
	// Runtimes & languages
	r.Register(NewNodejsFetcher(cfg, sm))
	r.Register(NewJdkFetcher(cfg, sm))
	r.Register(NewGolangFetcher(cfg, sm))
	r.Register(NewPythonFetcher(cfg, sm))
	r.Register(NewRustFetcher(cfg, sm))
	r.Register(NewRubyFetcher(cfg, sm))
	r.Register(NewDotNetFetcher(cfg, sm))
	r.Register(NewPHPFetcher(cfg, sm))
	r.Register(NewPerlFetcher(cfg, sm))
	// Build tools
	r.Register(NewMavenFetcher(cfg, sm))
	r.Register(NewGradleFetcher(cfg, sm))
	// Mobile development
	r.Register(NewFlutterFetcher(cfg, sm))
	r.Register(NewAndroidFetcher(cfg, sm))
	r.Register(NewDartFetcher(cfg, sm))
	return r
}

// Register registers an SDK fetcher
func (r *Registry) Register(f VersionFetcher) {
	r.fetchers[f.Type()] = f
}

// Get returns the fetcher for the specified SDK
func (r *Registry) Get(t SdkType) VersionFetcher {
	return r.fetchers[t]
}

// All returns all SDK fetchers (in order)
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
