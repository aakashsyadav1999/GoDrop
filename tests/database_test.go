package tests

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/database"
	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/store"
)

// openTestDB connects to the compose Postgres, and skips the test if it is not running.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://godrop:godrop@127.0.0.1:5433/godrop?sslmode=disable"
	}

	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t) // already migrated once
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("running the migrations again failed: %v", err)
	}
}

func TestHistoryAppendAndList(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	h := database.NewHistory(db)

	rec := store.Record{
		ID:         job.NewBaseJob().ID, // random, so tests do not collide with real rows
		URL:        "https://example.com",
		Status:     job.StatusFailed,
		Error:      "boom",
		DurationMS: 12,
		CreatedAt:  time.Now(),
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM job_history WHERE id = $1`, rec.ID) })

	if err := h.Append(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(ctx, rec); err != nil { // same id again: ignored, not an error
		t.Fatalf("appending a duplicate should be a no-op, got %v", err)
	}

	recs, err := h.List(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}

	var found []store.Record
	for _, r := range recs {
		if r.ID == rec.ID {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d rows for the job, want exactly 1", len(found))
	}
	got := found[0]
	if got.URL != rec.URL || got.Status != rec.Status || got.Error != "boom" ||
		got.DurationMS != 12 || !got.CreatedAt.Equal(rec.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("got %+v, want %+v", got, rec)
	}
}
