package profile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountCacheExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	cache := newAccountCache(time.Minute, 30*time.Second, 2)
	cache.now = func() time.Time { return now }

	alice := Account{DID: "did:plc:alice"}
	bob := Account{DID: "did:plc:bob"}
	carol := Account{DID: "did:plc:carol"}
	cache.put("alice.example", nil, &alice, nil)
	cache.put("bob.example", nil, &bob, nil)
	_, _, ok := cache.get("alice.example", nil)
	require.True(t, ok)

	cache.put("carol.example", nil, &carol, nil)
	_, _, ok = cache.get("bob.example", nil)
	require.False(t, ok, "least recently used entry should be evicted")
	_, _, ok = cache.get("alice.example", nil)
	require.True(t, ok, "a cache hit should update recency")

	now = now.Add(time.Minute)
	_, _, ok = cache.get("alice.example", nil)
	require.False(t, ok, "entry should expire at its TTL boundary")
}

func TestAccountCacheKeysCollectionsAndClonesValues(t *testing.T) {
	t.Parallel()

	cache := newAccountCache(time.Minute, 30*time.Second, 2)
	want := Account{
		DID: "did:plc:alice",
		Profiles: []Record{{
			Collection: "app.example.profile",
			Value:      []byte(`{"name":"Alice"}`),
		}},
		avatarRef: &BlobRef{CID: "bafycid", Size: 123},
	}
	cache.put(
		"alice.example",
		[]string{"app.example.profile"},
		&want,
		nil,
	)

	_, _, ok := cache.get("alice.example", nil)
	require.False(t, ok, "different collection selections must not collide")

	got, err, ok := cache.get(
		"alice.example",
		[]string{"app.example.profile"},
	)
	require.True(t, ok)
	require.NoError(t, err)
	got.Profiles[0].Value[0] = 'x'
	got.avatarRef.Size = 456

	again, err, ok := cache.get(
		"alice.example",
		[]string{"app.example.profile"},
	)
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, want, again, "callers must not mutate cached state")
}

func TestAccountCacheConcurrentAccessStaysBounded(t *testing.T) {
	t.Parallel()

	const maxEntries = 32
	cache := newAccountCache(time.Minute, 30*time.Second, maxEntries)
	var workers sync.WaitGroup
	for worker := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for request := range 100 {
				identifier := fmt.Sprintf("account-%d-%d.example", worker, request)
				account := Account{DID: "did:plc:" + identifier}
				cache.put(identifier, nil, &account, nil)
				_, _, _ = cache.get(identifier, nil)
			}
		}()
	}
	workers.Wait()

	cache.mu.Lock()
	entries := len(cache.entries)
	recency := cache.recency.Len()
	cache.mu.Unlock()
	require.LessOrEqual(t, entries, maxEntries)
	require.Equal(t, entries, recency)
}

func TestServiceLookupCachesSuccessfulAccountsAndErrors(t *testing.T) {
	t.Parallel()

	resolver := &countingResolver{
		identity: Identity{
			DID: "did:plc:alice",
			PDS: "https://pds.example",
		},
	}
	service := NewService(
		resolver,
		&fakeReader{records: map[string]Record{}},
		"app.example.profile",
		Source{Collection: "app.example.profile", RecordKey: "self"},
	)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	service.cache = newAccountCache(time.Minute, 30*time.Second, 2)
	service.cache.now = func() time.Time { return now }

	first, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	second, err := service.Lookup(context.Background(), "alice.example", nil)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, resolver.calls)

	resolver.err = errors.New("temporary resolver failure")
	_, err = service.Lookup(context.Background(), "bob.example", nil)
	require.Error(t, err)
	_, err = service.Lookup(context.Background(), "bob.example", nil)
	require.Error(t, err)
	require.Equal(t, 2, resolver.calls, "errors should use the shorter TTL")

	now = now.Add(30 * time.Second)
	_, err = service.Lookup(context.Background(), "bob.example", nil)
	require.Error(t, err)
	require.Equal(t, 3, resolver.calls, "cached error should expire")
}

func TestServiceLookupDoesNotCacheContextErrors(t *testing.T) {
	t.Parallel()

	resolver := &countingResolver{err: context.DeadlineExceeded}
	service := NewService(
		resolver,
		&fakeReader{},
		"app.example.profile",
		Source{Collection: "app.example.profile", RecordKey: "self"},
	)
	service.cache = newAccountCache(time.Minute, 30*time.Second, 2)

	_, err := service.Lookup(context.Background(), "alice.example", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = service.Lookup(context.Background(), "alice.example", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 2, resolver.calls)
}

func TestNewDefaultServiceValidatesCacheConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config CacheConfig
	}{
		{
			name: "zero success TTL",
			config: CacheConfig{
				ErrorTTL:   30 * time.Second,
				MaxEntries: 1,
			},
		},
		{
			name: "zero error TTL",
			config: CacheConfig{
				TTL:        time.Minute,
				MaxEntries: 1,
			},
		},
		{
			name: "error TTL not shorter",
			config: CacheConfig{
				TTL:        time.Minute,
				ErrorTTL:   time.Minute,
				MaxEntries: 1,
			},
		},
		{
			name: "zero maximum entries",
			config: CacheConfig{
				TTL:      time.Minute,
				ErrorTTL: 30 * time.Second,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, err := NewDefaultService(test.config)
			require.Error(t, err)
			require.Nil(t, service)
		})
	}
}

type countingResolver struct {
	identity Identity
	err      error
	calls    int
}

func (r *countingResolver) Resolve(
	_ context.Context,
	_ string,
) (Identity, error) {
	r.calls++
	return r.identity, r.err
}
