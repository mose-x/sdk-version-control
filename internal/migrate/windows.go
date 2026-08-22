//go:build windows

package migrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"svc/internal/logger"
	"svc/internal/wailsrt"
)

// legacyExeName is the pre-rename executable name ("SDK Version Control"
// wails project name + .exe). Self-updated pre-2.0.0 installs still run
// under this name from the old folder; installer-based installs are handled
// by the NSIS migration instead.
const legacyExeName = "SDK Version Control.exe"

// createNoWindow hides the console window of the migration helper so the
// rename happens silently.
const createNoWindow = 0x08000000

// isLegacyWindowsInstall reports whether the running binary is the old-named
// executable, i.e. a self-updated install that never went through the rename
// migration.
func isLegacyWindowsInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(exe), legacyExeName)
}

// buildLegacyRenamePs renders the migration as a PowerShell script: wait for
// the app to exit, rename the install folder to "svc" when the parent
// directory allows it (falls back to exe-only rename otherwise), rename the
// executable to svc.exe (leaving svc.exe.bak so RollbackUpdate keeps
// working), recreate shortcuts where legacy ones exist, and relaunch.
// PowerShell (not a .bat) avoids the nested-quoting problems that broke the
// earlier batch + inline-powershell version. Extracted for testing.
func buildLegacyRenamePs(pid int, currentExe string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$targetPid = %d
$oldExe = '%s'
$log = Join-Path $env:USERPROFILE 'svc_migration.log'
function Log($m) { Add-Content -LiteralPath $log -Value ("[{0}] {1}" -f (Get-Date -Format 'HH:mm:ss'), $m) }
Log "=== migration start (pid=$targetPid, exe=$oldExe) ==="

# Wait for the application to exit (it quits right after launching this
# script). Poll; if it already exited, proceed immediately.
$timeout = 60
while ((Get-Process -Id $targetPid -ErrorAction SilentlyContinue) -and $timeout -gt 0) {
    Start-Sleep -Seconds 1
    $timeout--
}
if ($timeout -le 0) { Log "ABORT: app did not exit in 60s"; exit 1 }

$oldDir  = Split-Path -Parent $oldExe
$parent  = Split-Path -Parent $oldDir
$newDir  = Join-Path $parent 'svc'
$oldName = 'SDK Version Control.exe'
Log "oldDir=$oldDir newDir=$newDir"

# Rename the install folder when the parent directory is writable (needs the
# same privileges the app already has; a non-elevated app in Program Files
# cannot rename the folder, so this silently falls back to exe-only rename).
if ($oldDir -ne $newDir) {
    Rename-Item -LiteralPath $oldDir -NewName 'svc' -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $newDir) { Log "folder renamed -> $newDir" }
    else { Log "folder rename FAILED (keeping $oldDir)"; $newDir = $oldDir }
}

# Rename the executable and set up the rollback backup RollbackUpdate expects
# (svc.exe.bak). Prefer the self-update backup (<old name>.bak), which holds
# the REAL previous version, over a copy of the current exe — so rollback
# actually restores the prior version instead of the same one, and we don't
# end up with two .bak files. Only copy the current exe when no self-update
# backup exists.
$legacyExe = Join-Path $newDir $oldName
$newExe    = Join-Path $newDir 'svc.exe'
$legacyBak = Join-Path $newDir ($oldName + '.bak')
$newBak    = Join-Path $newDir 'svc.exe.bak'
if (Test-Path -LiteralPath $legacyExe) {
    if (Test-Path -LiteralPath $legacyBak) {
        Move-Item -LiteralPath $legacyBak -Destination $newBak -Force
        Log "rollback backup moved -> svc.exe.bak"
    } else {
        Copy-Item -LiteralPath $legacyExe -Destination $newBak -Force
        Log "rollback backup copied -> svc.exe.bak"
    }
    Rename-Item -LiteralPath $legacyExe -NewName 'svc.exe' -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $newExe) { Log "exe renamed -> svc.exe" }
    else { Log "exe rename FAILED" }
} else { Log "legacy exe not found at $legacyExe" }
if (-not (Test-Path -LiteralPath $newExe)) { $newExe = $legacyExe }

# Icon for repaired shortcuts: the migrated install's white-plate icon, or the
# exe itself when the icon file is absent. Prevents shortcuts from keeping an
# IconLocation that points into the old (renamed) folder.
$icon = Join-Path $newDir 'icon-white.ico'
if (-not (Test-Path -LiteralPath $icon)) { $icon = $newExe }

# Update shortcuts only where legacy ones exist (never add shortcuts where the
# user had none). Each legacy .lnk is retargeted to the new exe and renamed to
# svc.lnk IN PLACE, so the desktop icon keeps its position and we never end up
# with a duplicate shortcut. A legacy .lnk is only deleted when a svc.lnk
# already exists in the same folder.
try {
    $ws = New-Object -ComObject WScript.Shell
    $bases = @(
        [Environment]::GetFolderPath('Desktop'),
        [Environment]::GetFolderPath('Programs'),
        'C:\Users\Public\Desktop',
        [Environment]::GetFolderPath('CommonPrograms')
    )
    foreach ($b in $bases) {
        if (-not $b) { continue }
        $legacy = Get-ChildItem -Path $b -Filter *.lnk -ErrorAction SilentlyContinue | Where-Object {
            $t = ''
            try { $t = ($ws.CreateShortcut($_.FullName)).TargetPath } catch {}
            ($_.Name -like 'SDK Version Control*') -or ($t -like '*SDK Version Control*')
        }
        foreach ($lnk in $legacy) {
            $svcLnk = Join-Path $lnk.DirectoryName 'svc.lnk'
            if (Test-Path -LiteralPath $svcLnk) {
                Remove-Item -LiteralPath $lnk.FullName -Force
                continue
            }
            $s = $ws.CreateShortcut($lnk.FullName)
            $s.TargetPath = $newExe
            $s.IconLocation = "$icon, 0"
            $s.Save()
            Rename-Item -LiteralPath $lnk.FullName -NewName 'svc.lnk' -Force -ErrorAction SilentlyContinue
        }
    }
} catch {}

# Notify the shell so renamed/removed shortcuts update on the desktop
# immediately, without the user having to manually refresh (SHCNE_ASSOCCHANGED).
try {
    Add-Type @"
using System;
using System.Runtime.InteropServices;
public class ShellRefresh { [DllImport("shell32.dll")] public static extern void SHChangeNotify(int wEventId, int uFlags, IntPtr dwItem1, IntPtr dwItem2); }
"@
    [ShellRefresh]::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)
} catch {}

# Relaunch only if the migration actually produced svc.exe. If the rename
# failed, relaunching the old exe would re-trigger this migration on the next
# launch and flash on every startup; leave the app closed for the user to
# relaunch manually instead.
if (Test-Path -LiteralPath (Join-Path $newDir 'svc.exe')) {
    Start-Process -FilePath (Join-Path $newDir 'svc.exe')
}
exit 0
`, pid, strings.ReplaceAll(currentExe, "'", "''"))
}

// launchLegacyRenameMigration writes the migration script to a temp .ps1 and
// starts it hidden (CREATE_NO_WINDOW), inheriting the app's current token —
// NO UAC prompt. If the app is already elevated the folder rename succeeds;
// otherwise the script falls back to renaming only the executable and
// shortcuts. The child survives the app's exit and waits for it before
// touching anything.
func launchLegacyRenameMigration(currentExe string) error {
	script := buildLegacyRenamePs(os.Getpid(), currentExe)
	f, err := os.CreateTemp(os.TempDir(), "svc_migrate_*.ps1")
	if err != nil {
		return fmt.Errorf("failed to create migration script: %w", err)
	}
	scriptPath := f.Name()
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write migration script: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write migration script: %w", err)
	}

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to launch migration: %w", err)
	}
	// Deliberately do NOT Wait: the script waits for THIS process to exit
	// before renaming. The child keeps running after the app quits.
	return nil
}

// maybeShowLegacyMigrationPrompt silently completes the rename migration on
// startup when the app detects it is still the old-named executable. No
// dialog and no UAC: the hidden migration script runs, the app quits, the
// script renames what it can and relaunches svc.exe. Idempotent — once the
// executable is svc.exe, isLegacyWindowsInstall is false and this is a no-op.
func MaybeShowLegacyMigrationPrompt(ctx context.Context, rt wailsrt.Runtime) {
	if !isLegacyWindowsInstall() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	logger.Info("Legacy install detected (%s); starting silent rename migration", exe)
	if err := launchLegacyRenameMigration(exe); err != nil {
		logger.Warn("Failed to start rename migration: %v", err)
		return
	}
	// The script waits for this process to exit before touching anything.
	rt.Quit()
}

// buildShortcutIconRepairPs renders a hidden PowerShell that repairs existing
// svc / legacy shortcuts whose TargetPath or IconLocation still reference the
// old (renamed) install folder — the cause of "cannot find icon-white.ico"
// and a blank desktop logo after an upgrade. It only edits shortcuts that
// already point at svc or the legacy product; it never creates new ones.
func buildShortcutIconRepairPs(currentExe string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$exe = '%s'
$dir = Split-Path -Parent $exe
$icon = Join-Path $dir 'icon-white.ico'
if (-not (Test-Path -LiteralPath $icon)) { $icon = $exe }
$ws = New-Object -ComObject WScript.Shell
$changed = $false
$bases = @(
    [Environment]::GetFolderPath('Desktop'),
    [Environment]::GetFolderPath('Programs'),
    'C:\Users\Public\Desktop',
    [Environment]::GetFolderPath('CommonPrograms')
)
foreach ($b in $bases) {
    if (-not $b) { continue }
    Get-ChildItem -Path $b -Filter *.lnk -ErrorAction SilentlyContinue | Where-Object {
        $t = ''
        try { $t = ($ws.CreateShortcut($_.FullName)).TargetPath } catch {}
        ($_.Name -eq 'svc.lnk') -or ($t -like '*svc.exe') -or ($t -like '*SDK Version Control*')
    } | ForEach-Object {
        $s = $ws.CreateShortcut($_.FullName)
        $need = $false
        if ($s.TargetPath -ne $exe) { $s.TargetPath = $exe; $need = $true }
        $curIcon = ($s.IconLocation -split ',')[0]
        if ($curIcon -ne $icon) { $s.IconLocation = "$icon, 0"; $need = $true }
        if ($need) { $s.Save(); $changed = $true }
    }
}
# Only refresh the shell if we actually repaired something, so a normal launch
# with already-correct shortcuts never triggers a desktop-wide refresh.
if ($changed) {
    try {
        Add-Type @"
using System;
using System.Runtime.InteropServices;
public class ShellRefresh2 { [DllImport("shell32.dll")] public static extern void SHChangeNotify(int wEventId, int uFlags, IntPtr dwItem1, IntPtr dwItem2); }
"@
        [ShellRefresh2]::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)
    } catch {}
}
`, strings.ReplaceAll(currentExe, "'", "''"))
}

// RepairShortcutIcons launches the hidden shortcut-icon repair on startup so
// shortcuts left pointing at the pre-rename folder get their target and logo
// re-pointed at the current install. Fire-and-forget; never blocks startup.
func RepairShortcutIcons() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	f, err := os.CreateTemp(os.TempDir(), "svc_lnk_repair_*.ps1")
	if err != nil {
		return
	}
	scriptPath := f.Name()
	if _, err := f.WriteString(buildShortcutIconRepairPs(exe)); err != nil {
		f.Close()
		os.Remove(scriptPath)
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(scriptPath)
		return
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
	}
	// Fire-and-forget: the repair runs hidden in the background.
}
