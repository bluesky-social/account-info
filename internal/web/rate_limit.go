package web

import (
	"container/list"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const maxRateLimitSourceIPs = 100_000

type sourceIPLimitEntry struct {
	source             netip.Addr
	theoreticalArrival time.Time
}

// sourceIPLimiter implements a bounded, per-source-IP GCRA rate limiter. Its
// burst capacity is equal to the configured per-second rate.
type sourceIPLimiter struct {
	mu             sync.Mutex
	entries        map[netip.Addr]*list.Element
	recency        *list.List
	interval       time.Duration
	burstTolerance time.Duration
	maxEntries     int
	now            func() time.Time
}

func newSourceIPLimiter(rate, maxEntries int) (*sourceIPLimiter, error) {
	if rate <= 0 {
		return nil, fmt.Errorf("lookup rate limit must be positive: %d", rate)
	}
	if rate > int(time.Second) {
		return nil, fmt.Errorf(
			"lookup rate limit exceeds timer resolution: %d",
			rate,
		)
	}
	if maxEntries <= 0 {
		return nil, fmt.Errorf(
			"rate limiter maximum entries must be positive: %d",
			maxEntries,
		)
	}

	interval := time.Second / time.Duration(rate)
	return &sourceIPLimiter{
		entries:        make(map[netip.Addr]*list.Element),
		recency:        list.New(),
		interval:       interval,
		burstTolerance: time.Duration(rate-1) * interval,
		maxEntries:     maxEntries,
		now:            time.Now,
	}, nil
}

func (l *sourceIPLimiter) allow(source netip.Addr) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	element, exists := l.entries[source]
	if !exists {
		if len(l.entries) >= l.maxEntries {
			oldest := rateLimitEntry(l.recency.Back())
			if now.Before(oldest.theoreticalArrival) {
				l.mu.Unlock()
				return false, oldest.theoreticalArrival.Sub(now)
			}
			l.remove(l.recency.Back())
		}
		entry := &sourceIPLimitEntry{
			source:             source,
			theoreticalArrival: now,
		}
		element = l.recency.PushFront(entry)
		l.entries[source] = element
	} else {
		l.recency.MoveToFront(element)
	}

	entry := rateLimitEntry(element)
	allowAt := entry.theoreticalArrival.Add(-l.burstTolerance)
	if now.Before(allowAt) {
		l.mu.Unlock()
		return false, allowAt.Sub(now)
	}
	if now.After(entry.theoreticalArrival) {
		entry.theoreticalArrival = now
	}
	entry.theoreticalArrival = entry.theoreticalArrival.Add(l.interval)
	l.mu.Unlock()
	return true, 0
}

func (l *sourceIPLimiter) remove(element *list.Element) {
	entry := rateLimitEntry(element)
	delete(l.entries, entry.source)
	l.recency.Remove(element)
}

func rateLimitEntry(element *list.Element) *sourceIPLimitEntry {
	entry, ok := element.Value.(*sourceIPLimitEntry)
	if !ok {
		panic("web: invalid source IP rate limit entry")
	}
	return entry
}

func limitLookups(limiter *sourceIPLimiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		source, err := sourceIP(request.RemoteAddr)
		if err != nil {
			slog.Error("determine lookup source IP", "error", err)
			writeError(
				w,
				http.StatusInternalServerError,
				"internal_error",
				"failed to determine request source",
				nil,
			)
			return
		}

		allowed, retryAfter := limiter.allow(source)
		if !allowed {
			w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds(retryAfter), 10))
			writeError(
				w,
				http.StatusTooManyRequests,
				"rate_limit_exceeded",
				"lookup rate limit exceeded",
				nil,
			)
			return
		}

		next.ServeHTTP(w, request)
	})
}

func sourceIP(remoteAddress string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote address: %w", err)
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote IP: %w", err)
	}
	return address.Unmap(), nil
}

func retryAfterSeconds(duration time.Duration) int64 {
	seconds := int64(duration / time.Second)
	if duration%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}
