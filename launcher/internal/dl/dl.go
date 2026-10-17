// Package dl downloads files in parallel with SHA-1 verification and retries.
package dl

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const UserAgent = "NovaLauncher/1.0"

var Client = &http.Client{Timeout: 5 * time.Minute}

type Job struct {
	URL  string
	Path string
	SHA1 string // optional
	Size int64  // optional, used when SHA1 is empty
}

// Get performs a GET with the launcher's User-Agent and returns the body for 2xx responses.
func Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return body, &HTTPError{Status: resp.StatusCode, URL: url}
	}
	return body, nil
}

type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.URL) }

// IsNotFound reports whether err is an HTTP 404.
func IsNotFound(err error) bool {
	var h *HTTPError
	return errors.As(err, &h) && h.Status == http.StatusNotFound
}

// Valid reports whether the file at path already matches the job.
func Valid(j Job) bool {
	st, err := os.Stat(j.Path)
	if err != nil || st.IsDir() {
		return false
	}
	if j.SHA1 != "" {
		sum, err := FileSHA1(j.Path)
		return err == nil && strings.EqualFold(sum, j.SHA1)
	}
	if j.Size > 0 {
		return st.Size() == j.Size
	}
	return st.Size() > 0
}

func FileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// One downloads a single job (skipping it if already valid).
func One(ctx context.Context, j Job) error {
	if Valid(j) {
		return nil
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		if last = fetch(ctx, j); last == nil {
			return nil
		}
	}
	return fmt.Errorf("%s: %w", filepath.Base(j.Path), last)
}

func fetch(ctx context.Context, j Job) error {
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Status: resp.StatusCode, URL: j.URL}
	}
	tmp := j.Path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if j.SHA1 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), j.SHA1) {
		os.Remove(tmp)
		return fmt.Errorf("checksum mismatch")
	}
	return os.Rename(tmp, j.Path)
}

// All downloads jobs with the given number of workers. progress is called after each job.
func All(ctx context.Context, jobs []Job, workers int, progress func(done, total int)) error {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		done     atomic.Int64
		firstErr error
		errOnce  sync.Once
		wg       sync.WaitGroup
		ch       = make(chan Job)
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if err := One(ctx, j); err != nil {
					errOnce.Do(func() { firstErr = err; cancel() })
				}
				n := done.Add(1)
				if progress != nil {
					progress(int(n), len(jobs))
				}
			}
		}()
	}
	for _, j := range jobs {
		select {
		case ch <- j:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(ch)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
