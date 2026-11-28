package backup

import (
	"fmt"
	"os"
)

// PruneResult is the outcome of a successful Prune.
type PruneResult struct {
	// Kept is how many newest archives remain after pruning.
	Kept int
	// Removed is how many archives were deleted.
	Removed int
	// RemovedPaths are the full paths of archives that were deleted,
	// oldest first (the order Prune deleted them).
	RemovedPaths []string
}

// Prune deletes older *.zip archives under <dataDir>/backups so that at
// most keep newest archives remain (newest by List's CreatedAt ordering).
//
// Safety boundaries:
//   - Only files already returned by List are candidates — that is, *.zip
//     files directly inside <dataDir>/backups. Nested paths, non-zip files,
//     and anything outside that directory are never touched.
//   - keep < 1 is rejected (would otherwise delete every backup).
//   - A missing backups directory is not an error: nothing to prune.
func Prune(dataDir string, keep int) (PruneResult, error) {
	if keep < 1 {
		return PruneResult{}, fmt.Errorf("backup: prune keep must be at least 1, got %d", keep)
	}

	entries, err := List(dataDir)
	if err != nil {
		return PruneResult{}, err
	}
	if len(entries) <= keep {
		return PruneResult{Kept: len(entries)}, nil
	}

	// List returns newest first; everything past keep is excess (oldest last).
	excess := entries[keep:]
	removed := make([]string, 0, len(excess))
	// Delete oldest first so a mid-prune crash leaves the newest archives.
	for i := len(excess) - 1; i >= 0; i-- {
		path := excess[i].Path
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return PruneResult{Kept: keep, Removed: len(removed), RemovedPaths: removed},
				fmt.Errorf("backup: remove %q: %w", path, err)
		}
		removed = append(removed, path)
	}
	return PruneResult{Kept: keep, Removed: len(removed), RemovedPaths: removed}, nil
}
