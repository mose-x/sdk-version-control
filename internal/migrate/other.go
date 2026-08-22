//go:build !windows && !darwin

package migrate

import (
	"context"

	"svc/internal/wailsrt"
)

// MaybeShowLegacyMigrationPrompt is a no-op on platforms other than
// Windows/macOS: the rename migration only concerns old self-updated
// installs on those two platforms.
func MaybeShowLegacyMigrationPrompt(ctx context.Context, rt wailsrt.Runtime) {}

// RepairShortcutIcons is a no-op off Windows: only Windows has .lnk shortcuts
// that can retain a stale icon/target path after the rename migration.
func RepairShortcutIcons() {}
