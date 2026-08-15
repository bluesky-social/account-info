package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSourceIPLimiterAllowsBurstAndRefills(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	limiter, err := newSourceIPLimiter(3, 100)
	require.NoError(t, err)
	limiter.now = func() time.Time { return now }

	for range 3 {
		allowed, retryAfter := limiter.allow(netip.MustParseAddr("192.0.2.1"))
		require.True(t, allowed)
		require.Zero(t, retryAfter)
	}
	allowed, retryAfter := limiter.allow(netip.MustParseAddr("192.0.2.1"))
	require.False(t, allowed)
	require.Equal(t, time.Second/3, retryAfter)

	now = now.Add(time.Second / 3)
	allowed, retryAfter = limiter.allow(netip.MustParseAddr("192.0.2.1"))
	require.True(t, allowed)
	require.Zero(t, retryAfter)
}

func TestSourceIPLimiterIsolatesSourcesAndBoundsState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	limiter, err := newSourceIPLimiter(1, 2)
	require.NoError(t, err)
	limiter.now = func() time.Time { return now }

	for _, source := range []string{"192.0.2.1", "192.0.2.2"} {
		allowed, retryAfter := limiter.allow(netip.MustParseAddr(source))
		require.True(t, allowed)
		require.Zero(t, retryAfter)
	}
	allowed, retryAfter := limiter.allow(netip.MustParseAddr("192.0.2.3"))
	require.False(t, allowed, "active entries must not be evicted to admit a new source")
	require.Equal(t, time.Second, retryAfter)

	limiter.mu.Lock()
	entries := len(limiter.entries)
	recency := limiter.recency.Len()
	limiter.mu.Unlock()
	require.Equal(t, 2, entries)
	require.Equal(t, entries, recency)

	allowed, _ = limiter.allow(netip.MustParseAddr("192.0.2.2"))
	require.False(t, allowed, "one source must not gain another source's capacity")

	now = now.Add(time.Second)
	allowed, retryAfter = limiter.allow(netip.MustParseAddr("192.0.2.3"))
	require.True(t, allowed, "fully replenished entries should be reclaimable")
	require.Zero(t, retryAfter)
}

func TestSourceIPLimiterConcurrentAccessStaysBounded(t *testing.T) {
	t.Parallel()

	const maxEntries = 128
	limiter, err := newSourceIPLimiter(3, maxEntries)
	require.NoError(t, err)

	var workers sync.WaitGroup
	for worker := range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for request := range 100 {
				source := netip.AddrFrom4([4]byte{
					192,
					byte(worker),
					byte(request),
					1,
				})
				_, _ = limiter.allow(source)
			}
		}()
	}
	workers.Wait()

	limiter.mu.Lock()
	entries := len(limiter.entries)
	recency := limiter.recency.Len()
	limiter.mu.Unlock()
	require.LessOrEqual(t, entries, maxEntries)
	require.Equal(t, entries, recency)
}

func TestNewSourceIPLimiterRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		rate       int
		maxEntries int
	}{
		{name: "zero rate", rate: 0, maxEntries: 1},
		{name: "negative rate", rate: -1, maxEntries: 1},
		{name: "rate exceeds timer resolution", rate: 1_000_000_001, maxEntries: 1},
		{name: "zero entries", rate: 1, maxEntries: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limiter, err := newSourceIPLimiter(test.rate, test.maxEntries)
			require.Error(t, err)
			require.Nil(t, limiter)
		})
	}
}

func TestServeRejectsNegativeLookupRateLimit(t *testing.T) {
	t.Parallel()

	err := Serve(
		context.Background(),
		":0",
		time.Minute,
		30*time.Second,
		1,
		-1,
	)
	require.EqualError(t, err, "lookup rate limit must not be negative: -1")
}

func TestLookupRateLimitReturns429BeforeCallingUpstream(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	limiter, err := newSourceIPLimiter(1, 100)
	require.NoError(t, err)
	handler := routes(lookup, limiter)

	first := newRequest(http.MethodGet, "/alice.example", "192.0.2.1:1234")
	first.Header.Set("Accept", "application/json")
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	require.Equal(t, http.StatusOK, firstResponse.Code)

	second := newRequest(http.MethodGet, "/bob.example", "192.0.2.1:5678")
	second.Header.Set("Accept", "application/json")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)

	require.Equal(t, http.StatusTooManyRequests, secondResponse.Code)
	require.Equal(t, "1", secondResponse.Header().Get("Retry-After"))
	require.Equal(t, "no-store", secondResponse.Header().Get("Cache-Control"))
	var body errorResponse
	require.NoError(t, json.Unmarshal(secondResponse.Body.Bytes(), &body))
	require.Equal(t, "rate_limit_exceeded", body.Error)
	require.Equal(t, 1, lookup.lookupCalls)
}

func TestLookupRateLimitUsesRemoteAddressNotForwardingHeaders(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	limiter, err := newSourceIPLimiter(1, 100)
	require.NoError(t, err)
	handler := routes(lookup, limiter)

	for requestNumber := range 2 {
		request := newRequest(http.MethodGet, "/alice.example", "192.0.2.1:1234")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", requestNumber+1))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if requestNumber == 0 {
			require.Equal(t, http.StatusOK, response.Code)
		} else {
			require.Equal(t, http.StatusTooManyRequests, response.Code)
		}
	}
}

func TestLookupRateLimitIsPerSourceAndExemptsNonLookupRoutes(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	limiter, err := newSourceIPLimiter(1, 100)
	require.NoError(t, err)
	handler := routes(lookup, limiter)

	for _, source := range []string{"192.0.2.1:1234", "192.0.2.2:1234"} {
		request := newRequest(http.MethodGet, "/alice.example", source)
		request.Header.Set("Accept", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
	}

	for range 2 {
		request := newRequest(http.MethodGet, "/healthz", "192.0.2.1:1234")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
	}
}

func TestAccountAndAvatarShareSourceRateLimit(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	limiter, err := newSourceIPLimiter(1, 100)
	require.NoError(t, err)
	handler := routes(lookup, limiter)

	accountRequest := newRequest(http.MethodGet, "/alice.example", "192.0.2.1:1234")
	accountRequest.Header.Set("Accept", "application/json")
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, accountRequest)
	require.Equal(t, http.StatusOK, accountResponse.Code)

	avatarRequest := newRequest(
		http.MethodGet,
		"/avatar/alice.example",
		"192.0.2.1:5678",
	)
	avatarResponse := httptest.NewRecorder()
	handler.ServeHTTP(avatarResponse, avatarRequest)
	require.Equal(t, http.StatusTooManyRequests, avatarResponse.Code)
	require.Zero(t, lookup.avatarCalls)
}

func TestSourceIPParsesCanonicalPeerAddresses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		remoteAddress string
		want          string
	}{
		{name: "IPv4", remoteAddress: "192.0.2.1:1234", want: "192.0.2.1"},
		{name: "IPv6", remoteAddress: "[2001:db8::1]:1234", want: "2001:db8::1"},
		{
			name:          "IPv4-mapped IPv6",
			remoteAddress: "[::ffff:192.0.2.1]:1234",
			want:          "192.0.2.1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := sourceIP(test.remoteAddress)
			require.NoError(t, err)
			require.Equal(t, test.want, got.String())
		})
	}
}

func TestLookupRateLimitRejectsMissingSourceAddress(t *testing.T) {
	t.Parallel()

	lookup := &fakeAccountLookup{account: testAccount(1)}
	limiter, err := newSourceIPLimiter(1, 100)
	require.NoError(t, err)
	handler := routes(lookup, limiter)
	request := newRequest(http.MethodGet, "/alice.example", "")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Zero(t, lookup.lookupCalls)
}

func newRequest(method, target, remoteAddress string) *http.Request {
	request := httptest.NewRequestWithContext(
		context.Background(),
		method,
		target,
		http.NoBody,
	)
	request.RemoteAddr = remoteAddress
	return request
}
