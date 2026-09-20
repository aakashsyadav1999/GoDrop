package store

import "context"

// HistoryStore keeps a permanent log of finished jobs. Unlike JobStore, whose
// records expire, history is append-only and is meant for looking back.
type HistoryStore interface {
	Append(ctx context.Context, rec Record) error
	List(ctx context.Context, limit int) ([]Record, error) // newest first
}

// NopHistory is used when no database is configured.
type NopHistory struct{}

func (NopHistory) Append(context.Context, Record) error        { return nil }
func (NopHistory) List(context.Context, int) ([]Record, error) { return nil, nil }
