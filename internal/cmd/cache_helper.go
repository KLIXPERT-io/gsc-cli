package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/KLIXPERT-io/gsc-cli/internal/output"
)

// cachedOrCall is the standard cache-get / api-call / cache-put flow.
// It respects --no-cache, --refresh, and --cache-ttl on the State.
func cachedOrCall(
	ctx context.Context,
	s *State,
	key string,
	ttl time.Duration,
	fetch func(ctx context.Context) (json.RawMessage, error),
) (json.RawMessage, output.Meta, error) {
	if s.CacheTTL > 0 {
		ttl = s.CacheTTL
	}
	if !s.NoCache && !s.Refresh {
		if entry, _ := s.Cache.Get(key); entry != nil {
			meta := output.MetaFromCache(true, entry.CachedAt, entry.Remaining(), 0)
			return entry.Payload, meta, nil
		}
	}
	payload, err := fetch(ctx)
	if err != nil {
		return nil, output.Meta{}, err
	}
	if !s.NoCache {
		_ = s.Cache.Put(key, payload, ttl)
	}
	return payload, output.Meta{Cached: false, APICalls: 1}, nil
}
