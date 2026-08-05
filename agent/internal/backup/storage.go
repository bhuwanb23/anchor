package backup

import (
	"context"
	"fmt"
	"log/slog"
)

// StorageManager provides on-demand storage stats and maintenance operations.
type StorageManager struct {
	repository *RepositoryManager
}

// NewStorageManager creates a new storage manager.
func NewStorageManager(repository *RepositoryManager) *StorageManager {
	return &StorageManager{
		repository: repository,
	}
}

// CollectStats gathers repository storage statistics via restic stats.
func (s *StorageManager) CollectStats(ctx context.Context) (*StatsResult, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("repository not initialized")
	}

	stats, err := s.repository.StatsRawData(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect storage stats: %w", err)
	}

	slog.Info("storage stats collected",
		"total_size", stats.TotalSize,
		"files", stats.TotalFileCount,
		"snapshots", stats.SnapshotsCount)
	return stats, nil
}

// RebuildIndex triggers a manual restic index rebuild.
func (s *StorageManager) RebuildIndex(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("repository not initialized")
	}

	slog.Info("manual index rebuild triggered")
	return s.repository.RebuildIndex(ctx)
}

// CacheCleanup triggers a manual restic cache cleanup.
func (s *StorageManager) CacheCleanup(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("repository not initialized")
	}

	slog.Info("manual cache cleanup triggered")
	return s.repository.CacheCleanup(ctx)
}

// CheckQuota compares current usage against a limit and returns usage percentage.
func (s *StorageManager) CheckQuota(ctx context.Context, limitBytes int64) (currentUsage int64, percentage float64, err error) {
	if limitBytes <= 0 {
		return 0, 0, nil
	}

	stats, err := s.CollectStats(ctx)
	if err != nil {
		return 0, 0, err
	}

	currentUsage = stats.TotalSize
	percentage = float64(currentUsage) / float64(limitBytes) * 100

	return currentUsage, percentage, nil
}

// RunMaintenance executes both cache cleanup and index rebuild.
func (s *StorageManager) RunMaintenance(ctx context.Context) (cacheErr, indexErr error) {
	cacheErr = s.CacheCleanup(ctx)
	indexErr = s.RebuildIndex(ctx)
	return cacheErr, indexErr
}
