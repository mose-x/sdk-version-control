package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/sdk"
)

type StorageInfo struct {
	SdkType      string `json:"sdkType"`
	DisplayName  string `json:"displayName"`
	SdkDir       string `json:"sdkDir"`
	TotalSize    int64  `json:"totalSize"`
	VersionCount int    `json:"versionCount"`
	ActiveVer    string `json:"activeVer"`
}

func (a *App) UninstallVersion(sdkType string, version string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}

	logger.Info("Uninstalling %s version: %s", sdkType, version)

	active := a.cfg.GetActiveVersion(sdkType)
	wasActive := active == version

	versionDir := a.cfg.SdkVersionDir(sdkType, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		logger.Error("Version directory does not exist: %s", versionDir)
		return fmt.Errorf("version directory does not exist: %s", version)
	}

	if err := os.RemoveAll(versionDir); err != nil {
		logger.Error("Failed to delete version directory %s: %v", versionDir, err)
		return fmt.Errorf("failed to delete version directory: %w", err)
	}

	// If we deleted the active version, clear the active version config
	if wasActive {
		logger.Info("Deleted active version, clearing active version config")
		if err := a.cfg.ClearActiveVersion(sdkType); err != nil {
			logger.Error("Failed to clear active version config: %v", err)
		}
	}

	// Tear down the shim layer for this SDK type when the last version is
	// gone. RemoveSdk deletes shim files (go.exe/gofmt.exe/...), clears the
	// SDK's entries from shims.json, and drops its env-var lines from
	// .svc.rc / the registry. Without this, uninstalled SDKs leave orphan
	// shims that resolve to "no active version" and keep PathConfigured=true
	// (the shim files are still found by IsCommandAvailable).
	//
	// Only remove when NO versions remain: if other versions of the same
	// SDK are still installed, their shims must stay (shims route by SDK
	// type, not by version — the active version is resolved at run time).
	//
	// M7: Propagate the GetInstalledVersions error instead of treating any
	// read failure as "0 remaining". A transient read error (permission,
	// path is a file, ...) must NOT trigger shim teardown, otherwise
	// installed-but-unreadable SDKs lose their shims.
	left, err := a.noVersionsLeft(sdkType)
	if err != nil {
		logger.Warn("Failed to check remaining versions for %s, skipping shim teardown: %v", sdkType, err)
	} else if left {
		extraEnvVars := a.getExtraEnvVars(sdkType)
		if err := a.pathMgr.RemoveSdk(sdkType, extraEnvVars); err != nil {
			logger.Warn("Failed to remove shims for %s: %v", sdkType, err)
		}
	}

	if wasActive {
		// Return special error to signal frontend to refresh and show warning
		return fmt.Errorf("ACTIVE_VERSION_DELETED:%s", sdkType)
	}

	logger.Info("Successfully uninstalled %s version %s", sdkType, version)
	return nil
}

// noVersionsLeft reports whether no installed versions remain for sdkType.
// Used after UninstallVersion to decide whether to tear down the shim layer
// for the whole SDK type (shims route by type, so they are only obsolete
// once the last version is gone).
//
// M7: Returns (bool, error) so callers can distinguish a genuine "no
// versions" result from a read failure. A read error (permission, path is a
// file, ...) is propagated; callers must NOT treat it as "0 remaining" or
// they would wrongly tear down shims for SDKs whose versions are merely
// unreadable.
func (a *App) noVersionsLeft(sdkType string) (bool, error) {
	remaining, err := a.cfg.GetInstalledVersions(sdkType)
	if err != nil {
		return false, err
	}
	return len(remaining) == 0, nil
}

// getExtraEnvVars returns the extra env vars (JAVA_HOME, GOROOT, ...) declared
// by the SDK fetcher, mirroring the InstallSdk/ImportPathSdk call sites so the
// RemoveSdk path stays symmetric with the ConfigureSdk path.
func (a *App) getExtraEnvVars(sdkType string) map[string]string {
	if a.registry == nil {
		return nil
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil
	}
	return f.GetExtraEnvVars()
}

func (a *App) GetStorageInfo() []StorageInfo {
	var infos []StorageInfo
	for _, t := range sdk.AllSdkTypes() {
		sdkDir := a.cfg.SdkDir(string(t))
		entries, err := os.ReadDir(sdkDir)
		if err != nil {
			continue
		}

		var totalSize int64
		var versionCount int
		for _, e := range entries {
			if e.IsDir() {
				versionCount++
				totalSize += dirSize(filepath.Join(sdkDir, e.Name()))
			}
		}

		if versionCount > 0 {
			infos = append(infos, StorageInfo{
				SdkType:      string(t),
				DisplayName:  sdk.SdkDisplayName(t),
				SdkDir:       sdkDir,
				TotalSize:    totalSize,
				VersionCount: versionCount,
				ActiveVer:    a.cfg.GetActiveVersion(string(t)),
			})
		}
	}
	return infos
}

func (a *App) GetTmpCacheSize() int64 {
	return dirSize(a.cfg.TmpDir())
}

func (a *App) CleanTmpCache() error {
	tmpDir := a.cfg.TmpDir()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		logger.Error("Failed to read cache directory %s: %v", tmpDir, err)
		return fmt.Errorf("failed to read cache directory: %w", err)
	}
	logger.Info("Cleaning temporary cache: %d items in %s", len(entries), tmpDir)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(tmpDir, e.Name()))
	}
	logger.Info("Temporary cache cleaned")
	return nil
}

func (a *App) CleanInactiveVersions(sdkType string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}

	active := a.cfg.GetActiveVersion(sdkType)
	sdkDir := a.cfg.SdkDir(sdkType)
	entries, err := os.ReadDir(sdkDir)
	if err != nil {
		logger.Error("Failed to read directory %s: %v", sdkDir, err)
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var cleaned int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == active {
			continue
		}
		logger.Info("Removing inactive version: %s %s", sdkType, e.Name())
		if err := os.RemoveAll(filepath.Join(sdkDir, e.Name())); err != nil {
			logger.Error("Failed to delete %s: %v", e.Name(), err)
			return fmt.Errorf("failed to delete %s: %w", e.Name(), err)
		}
		cleaned++
	}
	// If no versions remain (e.g. active was empty and everything was
	// inactive), tear down the shim layer for this SDK type — same rationale
	// as in UninstallVersion: orphan shims keep PathConfigured=true and
	// resolve to "no active version" at run time.
	// M7: on read error, do NOT tear down shims (cannot confirm zero left).
	left, err := a.noVersionsLeft(sdkType)
	if cleaned > 0 && err == nil && left {
		extraEnvVars := a.getExtraEnvVars(sdkType)
		if err := a.pathMgr.RemoveSdk(sdkType, extraEnvVars); err != nil {
			logger.Warn("Failed to remove shims for %s: %v", sdkType, err)
		}
	}
	if err != nil {
		logger.Warn("Failed to check remaining versions for %s, skipped shim teardown: %v", sdkType, err)
	}
	logger.Info("Cleaned %d inactive versions for %s", cleaned, sdkType)
	return nil
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}
