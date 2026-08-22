// Package settings owns the user-facing settings policy on top of
// config.SettingsManager: token masking, the stale-snapshot guards in Save,
// and the endpoint accessors.
package settings

import (
	"encoding/base64"
	"strings"

	"svc/internal/config"
	"svc/internal/logger"
	"svc/internal/sdk"
)

// Service wraps the settings manager with the binding-layer policy.
type Service struct {
	sm *config.SettingsManager
}

// New builds a Service on the given settings manager.
func New(sm *config.SettingsManager) *Service {
	return &Service{sm: sm}
}

// Get returns the settings with the GitHub token masked. Never ships the raw
// token to the frontend -- it is a secret. The masked form (first6***last6)
// is for display; the real value is only ever written back via
// SaveGithubToken.
func (s *Service) Get() config.AppSettings {
	st := s.sm.Get()
	st.GitHubToken = sdk.MaskGithubToken(s.sm)
	return st
}

// Save persists a settings snapshot from the frontend.
func (s *Service) Save(settings config.AppSettings) error {
	logger.Info("Saving settings: theme=%s, language=%s, downloadThreads=%d",
		settings.Theme, settings.Language, settings.DownloadThreads)
	// The general SaveSettings flow spreads the existing settings object
	// (which carries the MASKED token from GetSettings) and writes it back
	// for any single-field change (theme, proxy, threads, ...). Without these
	// guards, the stale snapshot echoed back by the frontend would clobber
	// fields owned by other write paths:
	//   - GitHubToken: the masked "ghp_abc***def" string would overwrite the
	//     real base64 token on every unrelated settings save. The token is
	//     written exclusively through SaveGithubToken.
	//   - Endpoints: written exclusively through SaveEndpoints; a snapshot
	//     taken before the user saved custom endpoints would resurrect the
	//     old map (or nil) and wipe them.
	//   - InstallPath: written exclusively through MigrateInstallPath (which
	//     updates the SettingsManager directly, bypassing SaveSettings); a
	//     stale snapshot would silently revert the migration.
	// So here we always preserve the stored values and ignore whatever the
	// frontend echoed back for these three fields.
	existing := s.sm.Get()
	settings.GitHubToken = existing.GitHubToken
	settings.Endpoints = existing.Endpoints
	settings.InstallPath = existing.InstallPath
	return s.sm.Update(settings)
}

// SaveGithubToken stores a GitHub PAT after base64-encoding it (a light
// obfuscation so the token is not plaintext in settings.json). An empty token
// clears the stored value. The plaintext is never persisted and never returned
// to the frontend (Get returns the masked form).
func (s *Service) SaveGithubToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		st := s.sm.Get()
		st.GitHubToken = ""
		logger.Info("Cleared GitHub token")
		return s.sm.Update(st)
	}
	st := s.sm.Get()
	st.GitHubToken = base64.StdEncoding.EncodeToString([]byte(token))
	logger.Warn("GitHub token stored as base64 (reversible, not encrypted). Consider using a token with minimal scopes.")
	logger.Info("Saved GitHub token (masked=%s)", sdk.MaskGithubToken(s.sm))
	return s.sm.Update(st)
}

// GetDefaultEndpoints lists the built-in endpoint presets.
func (s *Service) GetDefaultEndpoints() []sdk.EndpointInfo {
	return sdk.DefaultEndpoints()
}

// GetEndpoints returns the custom endpoint overrides (never nil).
func (s *Service) GetEndpoints() map[string]string {
	st := s.sm.Get()
	if st.Endpoints == nil {
		return map[string]string{}
	}
	return st.Endpoints
}

// SaveEndpoints replaces the custom endpoint overrides.
func (s *Service) SaveEndpoints(endpoints map[string]string) error {
	logger.Info("Saving %d custom endpoints", len(endpoints))
	st := s.sm.Get()
	st.Endpoints = endpoints
	return s.sm.Update(st)
}
