package tests

import (
	"testing"

	"github.com/aakash/godrop/internal/job"
)

func TestURLJobEmbedsBaseJob(t *testing.T) {
	a := job.NewURLJob("https://example.com")
	b := job.NewURLJob("https://example.org")

	if a.URL != "https://example.com" {
		t.Fatalf("URL = %q", a.URL)
	}
	if a.ID == "" || a.ID == b.ID { // ID is promoted from BaseJob
		t.Fatalf("IDs must be set and unique, got %q and %q", a.ID, b.ID)
	}
	if a.Age() < 0 { // Age() is a promoted method
		t.Fatalf("Age() = %v", a.Age())
	}
	if a.BaseJob.ID != a.ID {
		t.Fatal("a.ID and a.BaseJob.ID should be the same field")
	}
}
