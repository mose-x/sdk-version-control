package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

// PythonFetcher Python version fetcher using astral-sh/python-build-standalone
// prebuilt binaries. python.org only ships source tarballs for Linux/macOS
// (no prebuilt bin/ dir), so we use python-build-standalone which provides
// prebuilt CPython for all three platforms with pip included.
type PythonFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewPythonFetcher(cfg *config.Config, sm *config.SettingsManager) *PythonFetcher {
	return &PythonFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *PythonFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }

// StripArchiveTopDir returns false because the python-build-standalone
// install_only archive extracts to a top-level `python/` directory whose
// name is part of GetBinDirs() (e.g. "python/bin"). Stripping would break
// the bin path resolution.
func (f *PythonFetcher) StripArchiveTopDir() bool { return false }

func (f *PythonFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Python)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://github.com", custom, -1)
}

func (f *PythonFetcher) Type() SdkType {
	return Python
}

// GetBinDirs returns the relative bin directories inside the extracted SDK.
// python-build-standalone extracts to `python/` containing:
//   - Unix: python/bin/  (python, python3, pip, pip3, etc.)
//   - Windows: python/   (python.exe, pythonw.exe directly in root; Scripts/
//     is empty in install_only archive — pip is usable via `python -m pip`)
func (f *PythonFetcher) GetBinDirs() []string {
	if config.IsWindows() {
		return []string{"python"}
	}
	return []string{"python/bin"}
}

func (f *PythonFetcher) GetExtraEnvVars() map[string]string {
	return nil
}

func (f *PythonFetcher) VerifyCommand() (string, []string) {
	// macOS 12.3+ and modern Linux distros ship only `python3` (Python 2's
	// `python` was removed). python-build-standalone Unix archives include
	// both `python` and `python3` in bin/, so `python3` verifies both the
	// system copy and an app-installed copy. Windows keeps `python`:
	// python-build-standalone Windows archives ship python.exe only, and
	// Windows users expect `python` (not python3.exe).
	if runtime.GOOS == "windows" {
		return "python", []string{"--version"}
	}
	return "python3", []string{"--version"}
}

// IsSystemPythonPath reports whether binPath points at a system-managed Python
// binary that cannot be safely imported. Importing such a copy would CopyDir
// an OS directory (e.g. /usr or C:\Windows) into the app's managed store, so
// the app should refuse and guide the user to install via the app instead.
// Covered cases:
//   - macOS /usr/bin/python3 (SSV-protected, also CLT framework path)
//   - Linux /usr/bin/python3 (distro package, owned by package manager)
//   - Windows Microsoft Store python.exe stub (WindowsApps alias)
func IsSystemPythonPath(binPath string) bool {
	if binPath == "" {
		return false
	}
	p := filepath.ToSlash(binPath)
	lower := strings.ToLower(p)
	switch runtime.GOOS {
	case "darwin":
		for _, prefix := range []string{"/usr/bin/", "/bin/", "/sbin/", "/system/", "/library/"} {
			if strings.HasPrefix(p, prefix) {
				return true
			}
		}
	case "linux":
		for _, prefix := range []string{"/usr/bin/", "/bin/", "/sbin/", "/usr/lib/"} {
			if strings.HasPrefix(p, prefix) {
				return true
			}
		}
	case "windows":
		if strings.HasPrefix(lower, "c:/windows/system32/") ||
			strings.HasPrefix(lower, "c:/windows/syswow64/") {
			return true
		}
		// Microsoft Store alias stub (e.g. %LOCALAPPDATA%\Microsoft\WindowsApps\python.exe)
		if strings.Contains(lower, "windowsapps") {
			return true
		}
	}
	return false
}

// platformTarget returns the python-build-standalone target triple used in
// asset filenames for the current OS/arch.
func (f *PythonFetcher) platformTarget() string {
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "x86_64-unknown-linux-gnu"
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		return "aarch64-unknown-linux-gnu"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		return "x86_64-apple-darwin"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "aarch64-apple-darwin"
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return "x86_64-pc-windows-msvc-shared"
	default:
		return ""
	}
}

// assetNameSuffix is the suffix appended after the target triple to select
// the standard install_only build (excludes debug/freethreaded/stripped).
func (f *PythonFetcher) assetNameSuffix() string {
	if runtime.GOOS == "windows" {
		return "-install_only.tar.gz"
	}
	return "-install_only.tar.gz"
}

// pythonRelease matches the GitHub releases API response for
// astral-sh/python-build-standalone.
type pythonRelease struct {
	TagName     string        `json:"tag_name"`
	PublishedAt string        `json:"published_at"`
	Assets      []pythonAsset `json:"assets"`
}

type pythonAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchRemoteVersions fetches Python versions from python-build-standalone
// GitHub releases. Each release (tagged by date) contains builds for multiple
// Python versions; we aggregate them and deduplicate by Python version,
// keeping the latest release's build for each version.
func (f *PythonFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	target := f.platformTarget()
	if target == "" {
		return nil, fmt.Errorf("current platform is not supported by python-build-standalone")
	}
	suffix := target + f.assetNameSuffix()

	// Regex to extract the Python version from asset names like:
	//   cpython-3.12.3+20240415-x86_64-unknown-linux-gnu-install_only.tar.gz
	verRe := regexp.MustCompile(`^cpython-(\d+\.\d+\.\d+)\+\d+-` + regexp.QuoteMeta(suffix) + `$`)

	seen := make(map[string]VersionInfo) // pythonVersion -> latest VersionInfo
	page := 1
	for page <= 5 { // fetch up to 5 pages (150 releases) to cover all Python versions
		url := f.useEndpoint(fmt.Sprintf("https://api.github.com/repos/astral-sh/python-build-standalone/releases?per_page=30&page=%d", page))
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build Python version list request (page %d): %w", page, err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Python version list (page %d): %w; check proxy/network, or if GitHub API is reachable", page, err)
		}
		// GitHub API returns 403 with a rate-limit body (non-JSON) when the
		// unauthenticated quota (60/hour per IP) is exhausted. Without a
		// status check the JSON decode below fails and the old code silently
		// broke out of the loop, reporting a misleading "no releases found".
		if resp.StatusCode == 403 {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API rate limit hit (403) on page %d; unauthenticated requests are capped at 60/hour per IP -- retry later or configure a proxy", page)
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API returned HTTP %d for Python version list (page %d)", resp.StatusCode, page)
		}
		var releases []pythonRelease
		err = json.NewDecoder(resp.Body).Decode(&releases)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode Python version list (page %d): %w", page, err)
		}
		if len(releases) == 0 {
			break // reached the last page, stop paging
		}

		for _, rel := range releases {
			for _, asset := range rel.Assets {
				m := verRe.FindStringSubmatch(asset.Name)
				if m == nil {
					continue
				}
				pyVer := m[1]
				parts := strings.Split(pyVer, ".")
				major, _ := strconv.Atoi(parts[0])
				date := ""
				if t, err := time.Parse(time.RFC3339, rel.PublishedAt); err == nil {
					date = t.Format("2006-01-02")
				}
				// Releases are returned newest-first; keep the first (latest)
				// build we encounter for each Python version.
				if _, exists := seen[pyVer]; !exists {
					seen[pyVer] = VersionInfo{
						Version:     pyVer,
						Major:       major,
						DownloadURL: f.useEndpoint(asset.BrowserDownloadURL),
						FileName:    asset.Name,
						ReleaseDate: date,
					}
				}
			}
		}
		page++
	}

	if len(seen) == 0 {
		return nil, fmt.Errorf("no python-build-standalone releases found")
	}

	var versions []VersionInfo
	for _, v := range seen {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})
	return versions, nil
}

func (f *PythonFetcher) GetDownloadURL(version string) (string, string, error) {
	versions, err := f.FetchRemoteVersions()
	if err != nil {
		return "", "", err
	}
	for _, v := range versions {
		if v.Version == version {
			return v.DownloadURL, v.FileName, nil
		}
	}
	return "", "", fmt.Errorf("Python version not found: %s", version)
}

func (f *PythonFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Python))
	active := f.cfg.GetActiveVersion(string(Python))
	configured := active != ""

	needsSwitch := false
	if active != "" {
		found := false
		for _, v := range installed {
			if v == active {
				found = true
				break
			}
		}
		needsSwitch = !found
	}

	// Use the platform-specific verify command so macOS/Linux detect the
	// system python3 (not the absent `python`). When a PATH copy is found,
	// also resolve its real binary path and flag system-managed copies
	// (e.g. /usr/bin/python3) that cannot be safely imported -- the UI then
	// hides the import button and guides the user to install instead.
	cmdName, _ := f.VerifyCommand()
	pathConfigured := !configured && IsCommandAvailable(cmdName)
	systemProtected := false
	systemPath := ""
	if pathConfigured {
		if p, err := exec.LookPath(cmdName); err == nil && IsSystemPythonPath(p) {
			systemProtected = true
			systemPath = p
		}
	}

	return &SdkStatus{
		SdkType:           Python,
		DisplayName:       SdkDisplayName(Python),
		Configured:        configured,
		PathConfigured:    pathConfigured,
		SystemProtected:   systemProtected,
		SystemPath:        systemPath,
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(Python)),
		NeedsSwitch:       needsSwitch,
	}, nil
}
