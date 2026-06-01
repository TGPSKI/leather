package cli

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// persistRunRecord appends rec as a JSON line to <dir>/<agent>.jsonl.
// If the file exceeds maxBytes it is rotated and gzipped asynchronously.
// Errors are non-fatal; callers should log them.
func persistRunRecord(dir string, rec model.RunRecord, maxBytes int64) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("persist/persistRunRecord: mkdir %s: %w", dir, err)
	}

	safe := sanitizeAgentName(rec.AgentName)
	path := filepath.Join(dir, safe+".jsonl")

	// Rotate if the active file has exceeded the size limit.
	if fi, err := os.Stat(path); err == nil && fi.Size() >= maxBytes {
		if err := rotateRunLog(path); err != nil {
			return fmt.Errorf("persist/persistRunRecord: rotate: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("persist/persistRunRecord: open %s: %w", path, err)
	}
	defer f.Close()

	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("persist/persistRunRecord: marshal: %w", err)
	}
	b = append(b, '\n')
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("persist/persistRunRecord: write: %w", err)
	}
	return nil
}

// rotateRunLog renames path to a timestamped copy and gzips it in the background.
func rotateRunLog(path string) error {
	ts := time.Now().UTC().Format("20060102T150405")
	rotated := strings.TrimSuffix(path, ".jsonl") + "-" + ts + ".jsonl"
	if err := os.Rename(path, rotated); err != nil {
		return fmt.Errorf("persist/rotateRunLog: rename: %w", err)
	}
	go func() { _ = gzipFile(rotated) }()
	return nil
}

// gzipFile compresses src to src.gz and removes the original on success.
func gzipFile(src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dst := src + ".gz"
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

// sanitizeAgentName maps characters that are unsafe in filenames to '-'.
func sanitizeAgentName(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		return r
	}, name)
}
