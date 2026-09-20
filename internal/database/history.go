package database

import (
	"context"
	"database/sql"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/store"
)

// History is a store.HistoryStore backed by the job_history table.
type History struct {
	db *sql.DB
}

// Compile-time check that *History is a store.HistoryStore.
var _ store.HistoryStore = (*History)(nil)

func NewHistory(db *sql.DB) *History {
	return &History{db: db}
}

// Append records a finished job. Recording the same job twice is not an error.
func (h *History) Append(ctx context.Context, rec store.Record) error {
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO job_history (id, url, status, status_code, error, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		rec.ID, rec.URL, string(rec.Status), rec.StatusCode, rec.Error, rec.DurationMS, rec.CreatedAt,
	)
	return err
}

// List returns up to limit finished jobs, newest first.
func (h *History) List(ctx context.Context, limit int) ([]store.Record, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, url, status, status_code, error, duration_ms, created_at
		FROM job_history
		ORDER BY finished_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recs []store.Record
	for rows.Next() {
		var rec store.Record
		var status string
		if err := rows.Scan(&rec.ID, &rec.URL, &status, &rec.StatusCode, &rec.Error, &rec.DurationMS, &rec.CreatedAt); err != nil {
			return nil, err
		}
		rec.Status = job.Status(status)
		recs = append(recs, rec)
	}
	return recs, rows.Err() // an error that ended the iteration early shows up here
}
