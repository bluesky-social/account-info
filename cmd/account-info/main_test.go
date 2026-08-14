package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandCacheDefaults(t *testing.T) { //nolint:paralleltest // Process environment isolation.
	for _, key := range []string{
		"ACCOUNT_INFO_CACHE_TTL",
		"ACCOUNT_INFO_CACHE_ERROR_TTL",
		"ACCOUNT_INFO_CACHE_MAX_ENTRIES",
	} {
		unsetEnv(t, key)
	}

	var gotTTL time.Duration
	var gotErrorTTL time.Duration
	var gotMaxEntries int
	command := newCommand(func(
		_ context.Context,
		_ string,
		cacheTTL time.Duration,
		cacheErrorTTL time.Duration,
		cacheMaxEntries int,
	) error {
		gotTTL = cacheTTL
		gotErrorTTL = cacheErrorTTL
		gotMaxEntries = cacheMaxEntries
		return nil
	})

	err := command.Run(context.Background(), []string{"account-info"})
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, gotTTL)
	require.Equal(t, 30*time.Second, gotErrorTTL)
	require.Equal(t, 1_000_000, gotMaxEntries)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	value, exists := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if exists {
			require.NoError(t, os.Setenv(key, value))
			return
		}
		require.NoError(t, os.Unsetenv(key))
	})
}

func TestCommandCacheEnvironment(t *testing.T) {
	t.Setenv("ACCOUNT_INFO_CACHE_TTL", "90s")
	t.Setenv("ACCOUNT_INFO_CACHE_ERROR_TTL", "15s")
	t.Setenv("ACCOUNT_INFO_CACHE_MAX_ENTRIES", "12345")

	var gotTTL time.Duration
	var gotErrorTTL time.Duration
	var gotMaxEntries int
	command := newCommand(func(
		_ context.Context,
		_ string,
		cacheTTL time.Duration,
		cacheErrorTTL time.Duration,
		cacheMaxEntries int,
	) error {
		gotTTL = cacheTTL
		gotErrorTTL = cacheErrorTTL
		gotMaxEntries = cacheMaxEntries
		return nil
	})

	err := command.Run(context.Background(), []string{"account-info"})
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, gotTTL)
	require.Equal(t, 15*time.Second, gotErrorTTL)
	require.Equal(t, 12_345, gotMaxEntries)
}
