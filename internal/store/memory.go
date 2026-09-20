package store

import (
	"context"
	"sync"
)

// MemoryStore is a JobStore backed by a map. The map is not safe for
// concurrent use on its own, so every access goes through mu.
type MemoryStore struct {
	mu   sync.RWMutex
	jobs map[string]Record
}

// Compile-time check that *MemoryStore is a JobStore.
var _ JobStore = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]Record)}
}

func (s *MemoryStore) Save(_ context.Context, rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[rec.ID] = rec
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.jobs[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

// Delete removes a record. Deleting an ID that does not exist is not an error.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.jobs, id)
	return nil
}
