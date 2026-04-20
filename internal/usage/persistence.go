package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const statisticsSnapshotVersion = 1

type statisticsSnapshotFile struct {
	Version int                `json:"version"`
	SavedAt time.Time          `json:"saved_at"`
	Usage   StatisticsSnapshot `json:"usage"`
}

// StatisticsPersister saves and restores usage statistics snapshots.
type StatisticsPersister struct {
	stats    *RequestStatistics
	path     string
	debounce time.Duration

	mu      sync.Mutex
	started bool
	stopped bool

	triggerCh chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
}

var (
	defaultStatisticsPersisterMu sync.RWMutex
	defaultStatisticsPersister   *StatisticsPersister
)

// NewStatisticsPersister creates a persister backed by a JSON snapshot file.
func NewStatisticsPersister(stats *RequestStatistics, path string) *StatisticsPersister {
	return &StatisticsPersister{
		stats:     stats,
		path:      filepath.Clean(path),
		debounce:  2 * time.Second,
		triggerCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// SetDebounce overrides the autosave debounce interval.
func (p *StatisticsPersister) SetDebounce(delay time.Duration) {
	if p == nil || delay < 0 {
		return
	}
	p.mu.Lock()
	p.debounce = delay
	p.mu.Unlock()
}

// Start launches the background autosave worker.
func (p *StatisticsPersister) Start() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.started || p.stopped {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()

	go p.run()
}

func (p *StatisticsPersister) run() {
	var (
		timer   *time.Timer
		timerCh <-chan time.Time
	)

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerCh = nil
	}

	for {
		select {
		case <-p.triggerCh:
			delay := p.currentDebounce()
			if delay <= 0 {
				if err := p.Flush(); err != nil {
					log.WithError(err).Warn("usage: failed to flush snapshot")
				}
				stopTimer()
				continue
			}
			if timer == nil {
				timer = time.NewTimer(delay)
			} else {
				stopTimer()
				timer.Reset(delay)
			}
			timerCh = timer.C
		case <-timerCh:
			if err := p.Flush(); err != nil {
				log.WithError(err).Warn("usage: failed to flush snapshot")
			}
			timerCh = nil
		case <-p.stopCh:
			stopTimer()
			close(p.doneCh)
			return
		}
	}
}

func (p *StatisticsPersister) currentDebounce() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.debounce
}

// MarkDirty schedules a snapshot save.
func (p *StatisticsPersister) MarkDirty() {
	if p == nil {
		return
	}
	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if stopped {
		return
	}
	select {
	case p.triggerCh <- struct{}{}:
	default:
	}
}

// Load restores a previously saved snapshot into the target statistics store.
func (p *StatisticsPersister) Load() (bool, error) {
	if p == nil || p.stats == nil || p.path == "" {
		return false, nil
	}

	data, err := os.ReadFile(p.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read usage snapshot: %w", err)
	}

	var snapshotFile statisticsSnapshotFile
	if err = json.Unmarshal(data, &snapshotFile); err != nil {
		return false, fmt.Errorf("decode usage snapshot: %w", err)
	}
	if snapshotFile.Version != 0 && snapshotFile.Version != statisticsSnapshotVersion {
		return false, fmt.Errorf("unsupported usage snapshot version %d", snapshotFile.Version)
	}

	p.stats.MergeSnapshot(snapshotFile.Usage)
	return true, nil
}

// Flush writes the current statistics snapshot to disk.
func (p *StatisticsPersister) Flush() error {
	if p == nil || p.stats == nil || p.path == "" {
		return nil
	}

	snapshotFile := statisticsSnapshotFile{
		Version: statisticsSnapshotVersion,
		SavedAt: time.Now().UTC(),
		Usage:   p.stats.Snapshot(),
	}

	data, err := json.MarshalIndent(snapshotFile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode usage snapshot: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return fmt.Errorf("create usage snapshot directory: %w", err)
	}
	if err = writeFileAtomic(p.path, data, 0o644); err != nil {
		return fmt.Errorf("write usage snapshot: %w", err)
	}
	return nil
}

// Stop stops the autosave worker and flushes the latest snapshot.
func (p *StatisticsPersister) Stop() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	started := p.started
	p.mu.Unlock()

	var flushErr error
	if started {
		close(p.stopCh)
		<-p.doneCh
	}
	if err := p.Flush(); err != nil {
		flushErr = err
	}
	return flushErr
}

// SetDefaultStatisticsPersister swaps the process-wide persister used by usage hooks.
func SetDefaultStatisticsPersister(p *StatisticsPersister) {
	defaultStatisticsPersisterMu.Lock()
	defaultStatisticsPersister = p
	defaultStatisticsPersisterMu.Unlock()
}

// DefaultStatisticsPersister returns the process-wide persister used by usage hooks.
func DefaultStatisticsPersister() *StatisticsPersister {
	defaultStatisticsPersisterMu.RLock()
	defer defaultStatisticsPersisterMu.RUnlock()
	return defaultStatisticsPersister
}

// MarkStatisticsDirty schedules a save on the process-wide statistics persister.
func MarkStatisticsDirty() {
	if p := DefaultStatisticsPersister(); p != nil {
		p.MarkDirty()
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".usage-statistics-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.WithError(removeErr).Warn("usage: failed to remove temporary snapshot file")
		}
	}

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err = tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
