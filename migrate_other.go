//go:build !windows && !darwin

package main

import "context"

// maybeShowLegacyMigrationPrompt is a no-op on platforms other than
// Windows/macOS: the rename migration only concerns old self-updated
// installs on those two platforms.
func maybeShowLegacyMigrationPrompt(ctx context.Context) {}
