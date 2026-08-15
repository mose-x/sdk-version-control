package main

import (
	"encoding/base64"
	"strings"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetSettings() config.AppSettings {
	// Never ship the raw GitHub token to the frontend -- it is a secret.
	// Replace it with the masked form (first6***last6) for display; the real
	// value is only ever written back via SaveGithubToken.
	s := a.settings.Get()
	s.GitHubToken = sdk.MaskGithubToken(a.settings)
	return s
}

func (a *App) SaveSettings(settings config.AppSettings) error {
	logger.Info("Saving settings: theme=%s, language=%s, downloadThreads=%d",
		settings.Theme, settings.Language, settings.DownloadThreads)
	// The general SaveSettings flow spreads the existing settings object
	// (which carries the MASKED token from GetSettings) and writes it back
	// for any single-field change (theme, proxy, threads, ...). Without this
	// guard, the masked "ghp_abc***def" string would overwrite the real
	// base64 token on every unrelated settings save. The token is written
	// exclusively through SaveGithubToken, so here we always preserve the
	// stored value and ignore whatever the frontend echoed back.
	existing := a.settings.Get()
	settings.GitHubToken = existing.GitHubToken
	return a.settings.Update(settings)
}

// SaveGithubToken stores a GitHub PAT after base64-encoding it (a light
// obfuscation so the token is not plaintext in settings.json). An empty token
// clears the stored value. The plaintext is never persisted and never returned
// to the frontend (GetSettings returns the masked form).
func (a *App) SaveGithubToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		s := a.settings.Get()
		s.GitHubToken = ""
		logger.Info("Cleared GitHub token")
		return a.settings.Update(s)
	}
	s := a.settings.Get()
	s.GitHubToken = base64.StdEncoding.EncodeToString([]byte(token))
	logger.Warn("GitHub token stored as base64 (reversible, not encrypted). Consider using a token with minimal scopes.")
	logger.Info("Saved GitHub token (masked=%s)", sdk.MaskGithubToken(a.settings))
	return a.settings.Update(s)
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
