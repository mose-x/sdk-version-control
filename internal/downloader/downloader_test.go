package downloader

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadSingle_PartRename verifies that downloadSingle writes to a .part
// temp file and atomically renames to the final destination on success.
func TestDownloadSingle_PartRename(t *testing.T) {
	payload := make([]byte, 256*1024) // 256KB
	for i := range payload {
		payload[i] = byte(i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 1)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify final file exists and has correct content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
	for i := range payload {
		if data[i] != payload[i] {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, data[i], payload[i])
		}
	}

	// Verify .part file does NOT exist (was renamed away)
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should not exist after successful download")
	}
}

// TestDownloadSingle_ErrorCleansPartFile verifies that .part is cleaned up when
// the connection breaks mid-download (after .part is created). This tests the
// actual defer cleanup path, not just a pre-creation HTTP error.
func TestDownloadSingle_ErrorCleansPartFile(t *testing.T) {
	payload := make([]byte, 256*1024) // 256KB

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		// Write partial data then hijack/close to simulate broken connection
		w.Write(payload[:1024]) // only 1KB of 256KB
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, _ := hijacker.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 1)
	if err == nil {
		t.Fatal("expected error for broken connection")
	}

	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should be cleaned up on error")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destPath should not exist on error")
	}
}

// TestDownloadMultiThread_PartRename verifies that downloadMultiThread writes to
// .part temp file, goroutines write to .part (not destPath), and atomically
// renames to destPath on success. This is the regression test for the C1 bug
// where goroutines used destPath instead of partPath.
func TestDownloadMultiThread_PartRename(t *testing.T) {
	payload := make([]byte, 10*1024*1024) // 10MB (> minMultiThreadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			// HEAD request
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Parse Range: bytes=start-end
		var start, end int64
		fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err != nil {
		t.Fatalf("Multi-thread download failed: %v", err)
	}

	// Verify final file exists with correct content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
	for i := 0; i < len(payload); i++ {
		if data[i] != payload[i] {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, data[i], payload[i])
		}
	}

	// Verify .part file does NOT exist
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should not exist after successful download")
	}
}

// TestDownloadMultiThread_ErrorCleansPartFile verifies multi-thread .part
// cleanup on error. HEAD advertises range support so the multi-thread path is
// genuinely taken (and the .part file pre-allocated); the ranged GET requests
// then fail with HTTP 500. (The previous version of this test returned 404
// for HEAD too, which silently fell back to single-thread before .part was
// ever created — the cleanup assertion never ran against real state.)
func TestDownloadMultiThread_ErrorCleansPartFile(t *testing.T) {
	const totalBytes = 6 * 1024 * 1024 // > minMultiThreadSize

	var rangeGets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", totalBytes))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Range") != "" {
			rangeGets.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err == nil {
		t.Fatal("expected error when ranged GET returns 500")
	}
	if rangeGets.Load() == 0 {
		t.Fatal("server never saw a ranged GET — the multi-thread path was not exercised")
	}
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should be cleaned up on error")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destPath should not exist on error")
	}
}

// TestDownloadMultiThread_ShortSegmentDetected verifies that a worker whose
// 206 response ends with a CLEAN EOF before the requested range is complete
// is treated as an error instead of silently leaving a zero hole in the
// pre-allocated .part file. The short body is delivered with chunked transfer
// encoding (no Content-Length), which lets the server terminate the stream
// without a protocol-level error — the exact scenario where trusting io.EOF
// produces a corrupt download.
func TestDownloadMultiThread_ShortSegmentDetected(t *testing.T) {
	const totalBytes = 6 * 1024 * 1024 // > minMultiThreadSize

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", totalBytes))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			// Non-ranged GET (only reachable via fallback): fail it too so
			// the test can never pass through the single-thread path.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var start, end int64
		fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		// 206 with chunked encoding and only HALF the requested bytes: the
		// stream ends with a clean io.EOF.
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, (end-start+1)/2))
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err == nil {
		t.Fatal("expected error for truncated segment, got success (silent corruption)")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error should report the incomplete segment, got: %v", err)
	}
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should be cleaned up on error")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destPath should not exist on error")
	}
}

// TestDownload_FallbackToSingleThread verifies that when a server doesn't support
// Range requests, the downloader falls back to single-thread and still succeeds.
func TestDownload_FallbackToSingleThread(t *testing.T) {
	payload := make([]byte, 10*1024*1024) // 10MB
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 200 with full body (no Range support)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		if r.Method == "GET" {
			io.Copy(w, &byteReader{data: payload})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err != nil {
		t.Fatalf("Download with fallback failed: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
}

// byteReader is a simple reader for fixed byte data.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// socks5Result reports what the fake SOCKS5 server observed.
type socks5Result struct {
	gotUser  string // username received during auth sub-negotiation
	gotPass  string // password received during auth sub-negotiation
	noAuth   bool   // client offered and used the no-auth method
	servedOK bool   // the tunneled HTTP request was served successfully
	err      error  // server-side protocol error (if any)
}

// startFakeSOCKS5Server starts a minimal single-connection SOCKS5 server on
// 127.0.0.1. If wantUser is non-empty it demands username/password
// authentication (RFC 1929) and only accepts the exact credentials;
// otherwise it only accepts the no-auth method. After authentication it
// accepts any CONNECT target and serves exactly one canned HTTP 200 response.
// What the server observed is reported on the returned channel.
func startFakeSOCKS5Server(t *testing.T, wantUser, wantPass string) (string, <-chan socks5Result) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	resCh := make(chan socks5Result, 1)

	go func() {
		var res socks5Result
		defer func() { resCh <- res }()

		conn, err := ln.Accept()
		if err != nil {
			res.err = err
			return
		}
		defer conn.Close()

		// Greeting: VER NMETHODS METHODS
		var hdr [2]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			res.err = err
			return
		}
		if hdr[0] != 5 {
			res.err = fmt.Errorf("unexpected SOCKS version %d", hdr[0])
			return
		}
		methods := make([]byte, int(hdr[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			res.err = err
			return
		}
		hasNoAuth := bytes.IndexByte(methods, 0x00) >= 0
		hasUserPass := bytes.IndexByte(methods, 0x02) >= 0

		switch {
		case wantUser != "":
			if !hasUserPass {
				conn.Write([]byte{5, 0xff})
				res.err = fmt.Errorf("client did not offer username/password auth (methods=%v)", methods)
				return
			}
			conn.Write([]byte{5, 0x02})
			// Auth sub-negotiation: VER ULEN USER PLEN PASS
			var fixed [2]byte
			if _, err := io.ReadFull(conn, fixed[:]); err != nil { // VER + ULEN
				res.err = err
				return
			}
			user := make([]byte, int(fixed[1]))
			if _, err := io.ReadFull(conn, user); err != nil {
				res.err = err
				return
			}
			var plen [1]byte
			if _, err := io.ReadFull(conn, plen[:]); err != nil {
				res.err = err
				return
			}
			pass := make([]byte, int(plen[0]))
			if _, err := io.ReadFull(conn, pass); err != nil {
				res.err = err
				return
			}
			res.gotUser, res.gotPass = string(user), string(pass)
			if res.gotUser != wantUser || res.gotPass != wantPass {
				conn.Write([]byte{0x01, 0x01}) // auth failed
				return
			}
			conn.Write([]byte{0x01, 0x00}) // auth OK
		default:
			if !hasNoAuth {
				conn.Write([]byte{5, 0xff})
				res.err = fmt.Errorf("client did not offer the no-auth method")
				return
			}
			res.noAuth = true
			conn.Write([]byte{5, 0x00})
		}

		// CONNECT request: VER CMD RSV ATYP <address> PORT
		var reqHdr [4]byte
		if _, err := io.ReadFull(conn, reqHdr[:]); err != nil {
			res.err = err
			return
		}
		switch reqHdr[3] {
		case 0x01: // IPv4
			addr := make([]byte, net.IPv4len)
			if _, err := io.ReadFull(conn, addr); err != nil {
				res.err = err
				return
			}
		case 0x03: // domain name
			var l [1]byte
			if _, err := io.ReadFull(conn, l[:]); err != nil {
				res.err = err
				return
			}
			dom := make([]byte, int(l[0]))
			if _, err := io.ReadFull(conn, dom); err != nil {
				res.err = err
				return
			}
		case 0x04: // IPv6
			addr := make([]byte, net.IPv6len)
			if _, err := io.ReadFull(conn, addr); err != nil {
				res.err = err
				return
			}
		default:
			res.err = fmt.Errorf("unsupported address type %d", reqHdr[3])
			return
		}
		var port [2]byte
		if _, err := io.ReadFull(conn, port[:]); err != nil {
			res.err = err
			return
		}

		// Accept the CONNECT with a dummy bound address.
		if _, err := conn.Write([]byte{5, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			res.err = err
			return
		}

		// Serve exactly one HTTP request with a canned response.
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			res.err = err
			return
		}
		if _, err := conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")); err != nil {
			res.err = err
			return
		}
		res.servedOK = true
	}()

	return ln.Addr().String(), resCh
}

// TestApplyProxy_SOCKS5Auth verifies that credentials embedded in a SOCKS5
// proxy URL (socks5://user:pass@host:port) are actually forwarded during the
// RFC 1929 username/password handshake. Previously nil auth was passed to
// xproxy.SOCKS5, silently dropping the credentials.
func TestApplyProxy_SOCKS5Auth(t *testing.T) {
	tests := []struct {
		name       string
		proxyURL   func(addr string) string
		serverUser string // credentials the fake server expects
		serverPass string
		wantNoAuth bool // client should use the no-auth method
		wantErr    bool // client.Get should fail
	}{
		{
			name:       "credentials forwarded and accepted",
			proxyURL:   func(addr string) string { return "socks5://alice:s3cret@" + addr },
			serverUser: "alice",
			serverPass: "s3cret",
		},
		{
			name:       "credentials forwarded but rejected",
			proxyURL:   func(addr string) string { return "socks5://alice:wrongpass@" + addr },
			serverUser: "alice",
			serverPass: "s3cret",
			wantErr:    true,
		},
		{
			name:       "no credentials offered for bare URL",
			proxyURL:   func(addr string) string { return "socks5://" + addr },
			wantNoAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, resCh := startFakeSOCKS5Server(t, tt.serverUser, tt.serverPass)

			transport := &http.Transport{}
			proxyURL, err := url.Parse(tt.proxyURL(addr))
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			applyProxy(transport, proxyURL)
			if transport.DialContext == nil {
				t.Fatal("applyProxy did not set DialContext for socks5 URL")
			}

			// The target host never touches the real network: the dial goes
			// through the fake SOCKS5 proxy, which accepts any CONNECT target.
			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			resp, err := client.Get("http://svc.test/file")
			if tt.wantErr {
				if err == nil {
					resp.Body.Close()
					t.Fatal("expected SOCKS5 auth failure, got success")
				}
			} else {
				if err != nil {
					t.Fatalf("GET through SOCKS5 proxy failed: %v", err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if string(body) != "ok" {
					t.Errorf("body = %q, want %q", body, "ok")
				}
			}

			res := <-resCh
			if !tt.wantErr && res.err != nil {
				t.Errorf("server-side protocol error: %v", res.err)
			}
			if tt.wantNoAuth {
				if !res.noAuth {
					t.Error("expected the client to use the no-auth method")
				}
				return
			}
			if res.gotUser != "alice" {
				t.Errorf("server saw username %q, want %q (credentials not forwarded)", res.gotUser, "alice")
			}
			if !tt.wantErr && res.gotPass != tt.serverPass {
				t.Errorf("server saw password %q, want %q", res.gotPass, tt.serverPass)
			}
			if !tt.wantErr && !res.servedOK {
				t.Error("expected the server to serve the tunneled request")
			}
		})
	}
}

// TestApplyProxyConfig covers the shared proxy-config application logic used
// by both BuildClient and the update checker's transport.
func TestApplyProxyConfig(t *testing.T) {
	tests := []struct {
		name         string
		cfg          ProxyConfig
		wantProxyURL string // expected transport.Proxy(req) URL; "" means no proxy func
		wantDialer   bool   // expect DialContext set (socks5)
		wantErr      bool
	}{
		{
			name: "disabled proxy applies nothing",
			cfg:  ProxyConfig{Enabled: false, Mode: "custom", URL: "127.0.0.1:7890"},
		},
		{
			name:         "custom bare host:port gets http scheme",
			cfg:          ProxyConfig{Enabled: true, Mode: "custom", URL: "127.0.0.1:7890"},
			wantProxyURL: "http://127.0.0.1:7890",
		},
		{
			name:       "custom bare host:port with socks5 protocol sets dialer",
			cfg:        ProxyConfig{Enabled: true, Mode: "custom", URL: "127.0.0.1:1080", Protocol: "socks5"},
			wantDialer: true,
		},
		{
			name:         "custom full http URL kept as-is",
			cfg:          ProxyConfig{Enabled: true, Mode: "custom", URL: "http://proxy.example:3128"},
			wantProxyURL: "http://proxy.example:3128",
		},
		{
			name:    "custom invalid URL returns error",
			cfg:     ProxyConfig{Enabled: true, Mode: "custom", URL: "a%zzb"},
			wantErr: true,
		},
		{
			name: "enabled custom with empty URL applies nothing",
			cfg:  ProxyConfig{Enabled: true, Mode: "custom", URL: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &http.Transport{}
			err := ApplyProxyConfig(transport, tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyProxyConfig: %v", err)
			}
			if tt.wantDialer {
				if transport.DialContext == nil {
					t.Error("expected DialContext to be set for socks5 proxy")
				}
				return
			}
			if tt.wantProxyURL == "" {
				if transport.Proxy != nil {
					req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
					u, _ := transport.Proxy(req)
					if u != nil {
						t.Errorf("expected no proxy, got %v", u)
					}
				}
				return
			}
			if transport.Proxy == nil {
				t.Fatal("expected transport.Proxy to be set")
			}
			req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
			u, err := transport.Proxy(req)
			if err != nil {
				t.Fatalf("transport.Proxy: %v", err)
			}
			if u == nil || u.String() != tt.wantProxyURL {
				t.Errorf("proxy URL = %v, want %s", u, tt.wantProxyURL)
			}
		})
	}
}
