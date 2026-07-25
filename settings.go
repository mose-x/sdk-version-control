package main

import (
	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetSettings() config.AppSettings {
	return a.settings.Get()
}

func (a *App) SaveSettings(settings config.AppSettings) error {
	logger.Info("Saving settings: theme=%s, language=%s, downloadThreads=%d",
		settings.Theme, settings.Language, settings.DownloadThreads)
	return a.settings.Update(settings)
}

func (a *App) GetDefaultEndpoints() []sdk.EndpointInfo {
	return sdk.DefaultEndpoints()
}

func (a *App) GetEndpoints() map[string]string {
	s := a.settings.Get()
	if s.Endpoints == nil {
		return map[string]string{}
	}
	return s.Endpoints
}

func (a *App) SaveEndpoints(endpoints map[string]string) error {
	logger.Info("Saving %d custom endpoints", len(endpoints))
	s := a.settings.Get()
	s.Endpoints = endpoints
	return a.settings.Update(s)
}
