package main

import (
	"io"
	"os"
	"strconv"
	"sync"
)

type rotatingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

func openRotatingLog(path string, maxMB float64, backups int) (io.WriteCloser, error) {
	if maxMB < 0.032 {
		maxMB = 0.032
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- path comes from CLI flag only, not network input
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &rotatingLog{
		path:     path,
		maxBytes: int64(maxMB * 1024 * 1024),
		backups:  backups,
		file:     f,
		size:     st.Size(),
	}, nil
}

func (r *rotatingLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxBytes > 0 && r.size+int64(len(p)) > r.maxBytes {
		_ = r.file.Close()
		if r.backups > 0 {
			_ = os.Remove(r.path + "." + strconv.Itoa(r.backups))
			for i := r.backups - 1; i >= 1; i-- {
				_ = os.Rename(r.path+"."+strconv.Itoa(i), r.path+"."+strconv.Itoa(i+1))
			}
			_ = os.Rename(r.path, r.path+".1")
		} else {
			_ = os.Remove(r.path)
		}
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, err
		}
		r.file = f
		r.size = 0
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingLog) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}
