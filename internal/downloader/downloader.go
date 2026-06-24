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
	"strconv"
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
		Timeout:   0,
	}
}

// applyProxy applies a proxy URL to the transport, auto-detecting HTTP and SOCKS5
func applyProxy(transport *http.Transport, proxyURL *url.URL) {
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		dialer, err := xproxy.SOCKS5("tcp", proxyURL.Host, nil, xproxy.Direct)
		if err == nil {
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}
		}
	} else {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
}

// hasScheme checks whether a string contains a URL scheme (e.g. http://, socks5://)
// Strictly per RFC 3986: the first character of a scheme must be a letter, followed by letters, digits, +, -, .
func hasScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' && i > 0 {
			return true
		}
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && ((c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'))) {
			return false
		}
	}
	return false
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
func (d *Downloader) downloadSingle(ctx context.Context, client *http.Client, downloadURL, destPath string, callback ProgressCallback) error {
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

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

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

	// Create output file and pre-allocate size
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	if err := out.Truncate(totalBytes); err != nil {
		out.Close()
		return fmt.Errorf("failed to pre-allocate file: %w", err)
	}
	out.Close()

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

			if resp.StatusCode == http.StatusOK {
				errCh <- fmt.Errorf("fallback: server returned 200 instead of 206 for range request")
				return
			}
			if resp.StatusCode != http.StatusPartialContent {
				errCh <- fmt.Errorf("segmented download failed, HTTP status code: %d", resp.StatusCode)
				return
			}

			f, err := os.OpenFile(destPath, os.O_WRONLY, 0)
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
	return nil
}

// GetContentLength retrieves the size of a remote file (used by update downloads etc.)
func GetContentLength(ctx context.Context, client *http.Client, downloadURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", downloadURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.ContentLength, nil
}

// ParseContentLength parses Content-Length from a string
func ParseContentLength(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
