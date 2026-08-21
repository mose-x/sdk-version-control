package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xproxy "golang.org/x/net/proxy"
	"sdk_version_control/internal/logger"
)

// ProgressCallback is the download progress callback function
type ProgressCallback func(downloadedBytes, totalBytes int64, speedBytesPerSec int64)

// ProxyConfig holds proxy configuration
type ProxyConfig struct {
	Enabled  bool   // whether to enable proxy
	Mode     string // "system" | "custom"
	URL      string // custom proxy URL
	Protocol string // "http" | "socks5" (used when custom proxy has no scheme)
}

// Downloader is an HTTP file downloader
type Downloader struct{}

// NewDownloader creates a downloader
func NewDownloader() *Downloader {
	return &Downloader{}
}

// BuildClient builds an HTTP client based on the proxy configuration
func BuildClient(proxy ProxyConfig) *http.Client {
	transport := &http.Transport{}

	if proxy.Enabled {
		switch proxy.Mode {
		case "system":
			applySystemProxy(transport)
		case "custom":
			if proxy.URL != "" {
				proxyStr := proxy.URL
				if !hasScheme(proxyStr) {
					scheme := "http"
					if proxy.Protocol == "socks5" {
						scheme = "socks5"
					}
					proxyStr = scheme + "://" + proxyStr
				}
				proxyURL, err := url.Parse(proxyStr)
				if err == nil {
					applyProxy(transport, proxyURL)
				}
			}
		}
	}

	return &http.Client{
		Transport: transport,
		// M9: No global client timeout. The 5min default was too short for
		// large SDK archives (JDK, Android cmdline-tools ~150MB+) and aborted
		// legitimate downloads mid-flight. Per-call timeouts are set by
		// callers: 30s for API/version-list fetches (sdk_ops.go), 10s for the
		// proxy check (proxy.go). For file downloads, the caller's context
		// cancellation (InstallSdk installCtx) is the timeout authority.
		Timeout: 0,
	}
}

// applyProxy applies a proxy URL to the transport, auto-detecting HTTP and SOCKS5
func applyProxy(transport *http.Transport, proxyURL *url.URL) {
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		dialer, err := xproxy.SOCKS5("tcp", proxyURL.Host, nil, xproxy.Direct)
		if err == nil {
			// H6: Use context-aware dialing if available, so that
			// context cancellation/timeout works through SOCKS5 proxies.
			if cd, ok := dialer.(xproxy.ContextDialer); ok {
				transport.DialContext = cd.DialContext
			} else {
				transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				}
			}
		}
	} else {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
}

// hasScheme reports whether s carries an RFC 3986 URL scheme prefix (e.g.
// "http://", "socks5://"). It requires the "://" separator so a bare
// "host:port" (e.g. "localhost:7890") is NOT misclassified as scheme="localhost"
// — which previously skipped auto-prepending "http://" and produced a proxy
// URL the net/http transport would reject.
func hasScheme(s string) bool {
	idx := strings.Index(s, "://")
	if idx <= 0 {
		return false
	}
	// Validate the prefix before "://" is a valid RFC 3986 scheme:
	// letter, followed by letters, digits, '+', '-' or '.'.
	for i := 0; i < idx; i++ {
		c := s[i]
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return false
			}
			continue
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

const minMultiThreadSize = 5 * 1024 * 1024 // files smaller than 5MB are not split

// Download downloads a file to the specified path; threads is the concurrent thread count (<=1 means single-threaded)
func (d *Downloader) Download(ctx context.Context, downloadURL, destPath string, callback ProgressCallback, proxy ProxyConfig, threads int) error {
	client := BuildClient(proxy)

	// Ensure the directory exists
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	logger.Info("Starting download: %s", filepath.Base(destPath))
	logger.Info("Download threads: %d", threads)

	// Try multi-threaded download
	if threads > 1 {
		err := d.downloadMultiThread(ctx, client, downloadURL, destPath, callback, threads)
		if err == nil {
			return nil
		}
		// Fall back to single-threaded when range requests are not supported
		if strings.Contains(err.Error(), "fallback") {
			logger.Warn("Multi-thread download fallback: %v", err)
		} else {
			return err
		}
	}

	logger.Info("Using single-thread download")
	return d.downloadSingle(ctx, client, downloadURL, destPath, callback)
}

// downloadSingle single-threaded download (original logic)
func (d *Downloader) downloadSingle(ctx context.Context, client *http.Client, downloadURL, destPath string, callback ProgressCallback) (err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed, HTTP status code: %d", resp.StatusCode)
	}

	// H4: Write to a .part temp file, rename on success, delete on error.
	// Prevents partial downloads from being mistaken for complete files.
	partPath := destPath + ".part"
	out, err := os.Create(partPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	// Clean up .part on any error path; on success the rename consumes it.
	defer func() {
		if err != nil {
			os.Remove(partPath)
		}
	}()
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close file: %w", cerr)
		}
	}()

	totalBytes := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	startTime := time.Now()
	var lastCallbackTime time.Time

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write file: %w", writeErr)
			}
			downloaded += int64(n)

			now := time.Now()
			if callback != nil && (lastCallbackTime.IsZero() || now.Sub(lastCallbackTime) >= 500*time.Millisecond) {
				var speed int64
				elapsed := now.Sub(startTime).Seconds()
				if elapsed > 0 {
					speed = int64(float64(downloaded) / elapsed)
				}
				callback(downloaded, totalBytes, speed)
				lastCallbackTime = now
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				if callback != nil {
					elapsed := time.Since(startTime).Seconds()
					var speed int64
					if elapsed > 0 {
						speed = int64(float64(downloaded) / elapsed)
					}
					callback(downloaded, totalBytes, speed)
				}
				break
			}
			return fmt.Errorf("failed to read response: %w", readErr)
		}
	}

	// H4: Atomically rename .part to final destination on success.
	if err = os.Rename(partPath, destPath); err != nil {
		return fmt.Errorf("failed to rename download file: %w", err)
	}
	return nil
}

// downloadMultiThread multi-threaded segmented download
func (d *Downloader) downloadMultiThread(ctx context.Context, client *http.Client, downloadURL, destPath string, callback ProgressCallback, threads int) error {
	// HEAD request to get file size and Range support
	headReq, err := http.NewRequestWithContext(ctx, "HEAD", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("fallback: %w", err)
	}
	headResp, err := client.Do(headReq)
	if err != nil {
		return fmt.Errorf("fallback: %w", err)
	}
	headResp.Body.Close()

	if headResp.StatusCode != http.StatusOK {
		return fmt.Errorf("fallback: HEAD status %d", headResp.StatusCode)
	}

	totalBytes := headResp.ContentLength
	acceptRanges := headResp.Header.Get("Accept-Ranges")
	if totalBytes <= 0 || !strings.EqualFold(acceptRanges, "bytes") {
		return fmt.Errorf("fallback: server does not support range requests")
	}

	if totalBytes < minMultiThreadSize {
		return fmt.Errorf("fallback: file too small for multi-thread")
	}

	// M1: Write to .part temp file, rename on success (same as downloadSingle).
	partPath := destPath + ".part"
	os.Remove(partPath) // clear stale .part from previous failed attempt
	out, err := os.Create(partPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	// C1+H1 fix: always close out and try to remove .part on exit.
	// On success, rename consumes .part so Remove is a no-op.
	// On error, .part is cleaned up. This avoids the named-return capture
	// problem where scoped `err :=` in if/case blocks didn't update the
	// outer err that the defer checked.
	defer func() {
		out.Close()
		os.Remove(partPath)
	}()
	if err := out.Truncate(totalBytes); err != nil {
		return fmt.Errorf("failed to pre-allocate file: %w", err)
	}

	// Calculate segments
	chunkSize := totalBytes / int64(threads)
	type chunk struct {
		start int64
		end   int64
	}
	var chunks []chunk
	for i := 0; i < threads; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == threads-1 {
			end = totalBytes - 1
		}
		chunks = append(chunks, chunk{start, end})
	}

	// Concurrent download
	var totalDownloaded atomic.Int64
	var wg sync.WaitGroup
	errCh := make(chan error, threads)
	startTime := time.Now()
	var stopProgress atomic.Bool

	// Progress reporter goroutine
	go func() {
		for !stopProgress.Load() {
			time.Sleep(500 * time.Millisecond)
			if callback != nil {
				downloaded := totalDownloaded.Load()
				elapsed := time.Since(startTime).Seconds()
				var speed int64
				if elapsed > 0 {
					speed = int64(float64(downloaded) / elapsed)
				}
				callback(downloaded, totalBytes, speed)
			}
		}
	}()

	for _, c := range chunks {
		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
			if err != nil {
				errCh <- err
				return
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

			resp, err := client.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()

			// O6: If server returns 200 instead of 206, it ignored the Range
			// header and is sending the full file. Writing it at the chunk offset
			// would corrupt the output. Fall back to single-thread.
			if resp.StatusCode == http.StatusOK {
				errCh <- fmt.Errorf("fallback: server returned 200 instead of 206, range not supported")
				return
			}
			if resp.StatusCode != http.StatusPartialContent {
				errCh <- fmt.Errorf("segmented download failed, HTTP status code: %d", resp.StatusCode)
				return
			}

			f, err := os.OpenFile(partPath, os.O_WRONLY, 0)
			if err != nil {
				errCh <- err
				return
			}
			defer f.Close()

			buf := make([]byte, 32*1024)
			offset := start
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					_, writeErr := f.WriteAt(buf[:n], offset)
					if writeErr != nil {
						errCh <- writeErr
						return
					}
					offset += int64(n)
					totalDownloaded.Add(int64(n))
				}
				if readErr != nil {
					if readErr == io.EOF {
						break
					}
					errCh <- readErr
					return
				}
			}
		}(c.start, c.end)
	}

	wg.Wait()
	stopProgress.Store(true)

	// Check errors
	select {
	case err := <-errCh:
		return err
	default:
	}

	// Final callback
	if callback != nil {
		elapsed := time.Since(startTime).Seconds()
		var speed int64
		if elapsed > 0 {
			speed = int64(float64(totalBytes) / elapsed)
		}
		callback(totalBytes, totalBytes, speed)
	}

	logger.Info("Multi-thread download completed: %s (%d threads, %d bytes)", filepath.Base(destPath), threads, totalBytes)
	// C1 fix: Close out before rename (Windows: can't rename open file),
	// then atomically rename .part to final destination.
	out.Close()
	if err := os.Rename(partPath, destPath); err != nil {
		return fmt.Errorf("failed to rename download file: %w", err)
	}
	return nil
}
