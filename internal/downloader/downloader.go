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
)

// ProgressCallback 下载进度回调函数
type ProgressCallback func(downloadedBytes, totalBytes int64, speedBytesPerSec int64)

// ProxyConfig 代理配置
type ProxyConfig struct {
	Enabled  bool   // 是否启用代理
	Mode     string // "system" | "custom"
	URL      string // 自定义代理地址
	Protocol string // "http" | "socks5"（自定义代理无 scheme 时使用）
}

// Downloader HTTP 文件下载器
type Downloader struct{}

// NewDownloader 创建下载器
func NewDownloader() *Downloader {
	return &Downloader{}
}

// BuildClient 根据代理配置构建 HTTP Client
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

// applyProxy 将代理 URL 应用到 transport，自动识别 HTTP 和 SOCKS5
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

// hasScheme 判断字符串是否包含 URL scheme（如 http://、socks5://）
// 严格按 RFC 3986：scheme 首字符必须是字母，后续可含数字/+/-/.
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

const minMultiThreadSize = 5 * 1024 * 1024 // 5MB 以下不分段

// Download 下载文件到指定路径，threads 为并发线程数（<=1 则单线程）
func (d *Downloader) Download(ctx context.Context, downloadURL, destPath string, callback ProgressCallback, proxy ProxyConfig, threads int) error {
	client := BuildClient(proxy)

	// 确保目录存在
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 尝试多线程下载
	if threads > 1 {
		err := d.downloadMultiThread(ctx, client, downloadURL, destPath, callback, threads)
		if err == nil {
			return nil
		}
		// 不支持分段时回退到单线程
		if !strings.Contains(err.Error(), "fallback") {
			return err
		}
	}

	return d.downloadSingle(ctx, client, downloadURL, destPath, callback)
}

// downloadSingle 单线程下载（原有逻辑）
func (d *Downloader) downloadSingle(ctx context.Context, client *http.Client, downloadURL, destPath string, callback ProgressCallback) error {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP状态码: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
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
				return fmt.Errorf("写入文件失败: %w", writeErr)
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
			return fmt.Errorf("读取响应失败: %w", readErr)
		}
	}

	return nil
}

// downloadMultiThread 多线程分段下载
func (d *Downloader) downloadMultiThread(ctx context.Context, client *http.Client, downloadURL, destPath string, callback ProgressCallback, threads int) error {
	// HEAD 请求获取文件大小和 Range 支持
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

	// 创建输出文件并预分配大小
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	if err := out.Truncate(totalBytes); err != nil {
		out.Close()
		return fmt.Errorf("预分配文件失败: %w", err)
	}
	out.Close()

	// 计算分段
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

	// 并发下载
	var totalDownloaded atomic.Int64
	var wg sync.WaitGroup
	errCh := make(chan error, threads)
	startTime := time.Now()
	var stopProgress atomic.Bool

	// 进度报告协程
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

			if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("分段下载失败，HTTP状态码: %d", resp.StatusCode)
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

	// 检查错误
	select {
	case err := <-errCh:
		return err
	default:
	}

	// 最后一次回调
	if callback != nil {
		elapsed := time.Since(startTime).Seconds()
		var speed int64
		if elapsed > 0 {
			speed = int64(float64(totalBytes) / elapsed)
		}
		callback(totalBytes, totalBytes, speed)
	}

	return nil
}

// GetContentLength 获取远程文件大小（用于更新下载等场景）
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

// ParseContentLength 从字符串解析 Content-Length
func ParseContentLength(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
