package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore is a JobStore backed by Redis. Each record is one JSON string
// under its own key, and expires after ttl so old jobs clean themselves up.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// Compile-time check that *RedisStore is a JobStore.
var _ JobStore = (*RedisStore)(nil)

func NewRedisStore(client *redis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{client: client, ttl: ttl}
}

func (s *RedisStore) key(id string) string {
	return "godrop:job:" + id
}

func (s *RedisStore) Save(ctx context.Context, rec Record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(rec.ID), data, s.ttl).Err()
}

func (s *RedisStore) Get(ctx context.Context, id string) (Record, error) {
	data, err := s.client.Get(ctx, s.key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}

	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	return s.client.Del(ctx, s.key(id)).Err()
}
