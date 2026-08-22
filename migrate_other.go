//go:build !windows

package main

import (
	"context"

	"svc/internal/config"
)

// maybeShowLegacyMigrationPrompt is a no-op off Windows: the rename
// migration only concerns old Windows install folders; macOS/Linux builds
// never used the "SDK Version Control" install layout.
func maybeShowLegacyMigrationPrompt(ctx context.Context, sm *config.SettingsManager) {}
