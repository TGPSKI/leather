package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
)

// replayLiveState holds the mutable playback state for --replay-live mode.
// All exported methods are safe for concurrent use.
type replayLiveState struct {
	mu        sync.RWMutex
	records   []model.RunRecord // all records sorted by Time.StartTs ascending
	speed     float64
	paused    bool
	startWall time.Time // wall-clock instant when replay started (or resumed)
	startTS   int64     // replay-clock origin: Unix timestamp of the first visible record
}

// clock returns the current replay-clock position as a Unix timestamp.
func (s *replayLiveState) clock() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.paused {
		return s.startTS
	}
	elapsed := time.Since(s.startWall).Seconds() * s.speed
	return s.startTS + int64(elapsed)
}

// speedAndPaused returns the current speed and paused state.
func (s *replayLiveState) speedAndPaused() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.speed, s.paused
}

// visible returns records whose Time.StartTs <= current replay clock, sorted descending.
func (s *replayLiveState) visible() []model.RunRecord {
	cutoff := s.clock()
	s.mu.RLock()
	defer s.mu.RUnlock()
	// records is sorted ascending; binary-search for the cutoff.
	hi := sort.Search(len(s.records), func(i int) bool {
		return s.records[i].Time.StartTs > cutoff
	})
	if hi == 0 {
		return nil
	}
	out := make([]model.RunRecord, hi)
	copy(out, s.records[:hi])
	// Reverse to descending order (most recent first).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// setSpeed updates the playback speed, adjusting startTS so the clock is continuous.
func (s *replayLiveState) setSpeed(speed float64) {
	if speed <= 0 {
		speed = 1.0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	elapsed := time.Since(s.startWall).Seconds() * s.speed
	s.startTS += int64(elapsed)
	s.startWall = time.Now()
	s.speed = speed
}

// setPaused pauses or resumes the replay clock.
func (s *replayLiveState) setPaused(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if paused == s.paused {
		return
	}
	if paused {
		// Freeze startTS at the current clock position.
		elapsed := time.Since(s.startWall).Seconds() * s.speed
		s.startTS += int64(elapsed)
	} else {
		// Resume: reset the wall-clock origin.
		s.startWall = time.Now()
	}
	s.paused = paused
}

// runReplay starts leather in read-only snapshot replay mode.
func runReplay(cfg model.Config, stderr io.Writer, log *logging.Logger, version, commit string) int {
	f, err := os.Open(cfg.ReplayFile)
	if err != nil {
		fmt.Fprintf(stderr, "leather serve --replay: open %s: %v\n", cfg.ReplayFile, err)
		return 1
	}
	defer f.Close()

	var snap snapshotResponse
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		fmt.Fprintf(stderr, "leather serve --replay: decode snapshot: %v\n", err)
		return 1
	}
	log.Info("replay mode: snapshot loaded", "captured_at", snap.CapturedAt, "agents", len(snap.Metrics))

	if !cfg.API {
		fmt.Fprintln(stderr, "leather serve --replay: API must be enabled; add --api")
		return 1
	}

	deps := apiDeps{
		cfg:       cfg,
		startedAt: time.Now(),
		version:   version,
		commit:    commit,
		log:       log,
		replay:    &snap,
	}
	srv := startAPIServer(deps)
	log.Info("replay server ready", "addr", cfg.APIAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Info("received signal, shutting down replay server")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	return 0
}

// runReplayLive starts leather in live JSONL replay mode.
func runReplayLive(cfg model.Config, stderr io.Writer, log *logging.Logger, version, commit string) int {
	records, err := loadJSONLDir(cfg.ReplayLiveDir)
	if err != nil {
		fmt.Fprintf(stderr, "leather serve --replay-live: %v\n", err)
		return 1
	}
	if len(records) == 0 {
		fmt.Fprintln(stderr, "leather serve --replay-live: no run records found in directory")
		return 1
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Time.StartTs < records[j].Time.StartTs })

	speed := cfg.ReplaySpeed
	if speed <= 0 {
		speed = 1.0
	}
	state := &replayLiveState{
		records:   records,
		speed:     speed,
		startWall: time.Now(),
		startTS:   records[0].Time.StartTs,
	}
	log.Info("replay-live mode ready",
		"records", len(records),
		"speed", speed,
		"first", time.Unix(records[0].Time.StartTs, 0).Format("2006-01-02 15:04:05"),
		"last", time.Unix(records[len(records)-1].Time.StartTs, 0).Format("2006-01-02 15:04:05"),
	)

	if !cfg.API {
		fmt.Fprintln(stderr, "leather serve --replay-live: API must be enabled; add --api")
		return 1
	}

	deps := apiDeps{
		cfg:        cfg,
		startedAt:  time.Now(),
		version:    version,
		commit:     commit,
		log:        log,
		replayLive: state,
	}
	srv := startAPIServer(deps)
	log.Info("replay-live server ready", "addr", cfg.APIAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Info("received signal, shutting down replay-live server")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	return 0
}

// loadJSONLDir reads all *.jsonl files from dir and returns the merged RunRecords.
func loadJSONLDir(dir string) ([]model.RunRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("loadJSONLDir: readdir %s: %w", dir, err)
	}
	var all []model.RunRecord
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		recs, err := readJSONL(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("loadJSONLDir: read %s: %w", e.Name(), err)
		}
		all = append(all, recs...)
	}
	return all, nil
}

// readJSONL reads and decodes all RunRecords from a JSONL file.
func readJSONL(path string) ([]model.RunRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var recs []model.RunRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB line buffer for large model outputs
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec model.RunRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("readJSONL %s: unmarshal: %w", path, err)
		}
		recs = append(recs, rec)
	}
	return recs, sc.Err()
}
