package installer

import "sync"

// fetcherLocks hands out a per-SDK mutex used to serialize "SetHTTPClient +
// the HTTP calls that must observe it" (FetchRemoteVersions / GetDownloadURL
// / FetchChecksum). The registry hands out shared fetcher singletons whose
// HTTP client is a bare field set via SetHTTPClient; without this lock a
// background version refresh and an install of the same SDK would race on
// that field. The guard mutex (mu) protects the map itself, NOT the
// fetchers.
type fetcherLocks struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// get returns the per-SDK mutex, lazily initializing the map so a zero
// fetcherLocks value is usable (tests rely on this).
func (l *fetcherLocks) get(key string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = make(map[string]*sync.Mutex)
	}
	m, ok := l.m[key]
	if !ok {
		m = &sync.Mutex{}
		l.m[key] = m
	}
	return m
}
