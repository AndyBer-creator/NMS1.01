package postgres

import (
	"context"
	"fmt"
)

func (r *Repo) SaveMetric(ctx context.Context, deviceID int, oid, value string) error {
	return r.saveMetricWithExec(ctx, r.db, deviceID, oid, value)
}

func (r *Repo) saveMetricWithExec(ctx context.Context, exec sqlExecutor, deviceID int, oid, value string) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO metrics (device_id, oid, value) VALUES ($1, $2, $3)`, deviceID, oid, value)
	return err
}

func (r *Repo) PruneOldMetricPartitions(ctx context.Context, retainMonths int) (int, error) {
	return r.pruneOldMetricPartitionsWithExec(ctx, r.db, retainMonths)
}

func (r *Repo) pruneOldMetricPartitionsWithExec(ctx context.Context, exec sqlExecutor, retainMonths int) (int, error) {
	if retainMonths < 1 {
		return 0, fmt.Errorf("retainMonths must be >= 1")
	}
	var dropped int
	if err := exec.QueryRowContext(ctx, `SELECT prune_old_metrics_partitions($1)`, retainMonths).Scan(&dropped); err != nil {
		return 0, err
	}
	return dropped, nil
}
