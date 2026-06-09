package source

import (
	"context"
	"errors"
	"fmt"
)

// MultiStrategy selects how MultiSource aggregates its sub-sources.
type MultiStrategy int

const (
	// MultiStrategyFallback tries sub-sources in order; the first success wins and
	// subsequent sources are skipped. Suitable for "S3 primary + local backup" scenarios.
	MultiStrategyFallback MultiStrategy = iota

	// MultiStrategyFirstOK fetches from all sub-sources concurrently and returns the
	// first successful result. Suitable for low-latency reads across mirrored sources.
	MultiStrategyFirstOK
)

// MultiSource aggregates multiple sub-sources under a single Reader interface.
// The strategy controls how Load selects among them.
type MultiSource struct {
	sources  []Reader
	strategy MultiStrategy
}

// NewMultiSource creates a MultiSource. At least one sub-source is required.
func NewMultiSource(strategy MultiStrategy, sources ...Reader) (*MultiSource, error) {
	if len(sources) == 0 {
		return nil, errors.New("multi source: at least one source is required")
	}
	for i, s := range sources {
		if s == nil {
			return nil, fmt.Errorf("multi source: source[%d] is nil", i)
		}
	}
	return &MultiSource{sources: sources, strategy: strategy}, nil
}

// NewFallbackSource is a shortcut for NewMultiSource(MultiStrategyFallback, sources...).
func NewFallbackSource(sources ...Reader) (*MultiSource, error) {
	return NewMultiSource(MultiStrategyFallback, sources...)
}

// NewFirstOKSource is a shortcut for NewMultiSource(MultiStrategyFirstOK, sources...).
func NewFirstOKSource(sources ...Reader) (*MultiSource, error) {
	return NewMultiSource(MultiStrategyFirstOK, sources...)
}

// Load dispatches to the strategy-specific loader.
func (ms *MultiSource) Load(ctx context.Context, key string) (string, error) {
	switch ms.strategy {
	case MultiStrategyFirstOK:
		return ms.loadFirstOK(ctx, key)
	default: // MultiStrategyFallback
		return ms.loadFallback(ctx, key)
	}
}

func (ms *MultiSource) loadFallback(ctx context.Context, key string) (string, error) {
	var errs []error
	for _, s := range ms.sources {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		code, err := s.Load(ctx, key)
		if err == nil {
			return code, nil
		}
		errs = append(errs, err)
	}
	return "", fmt.Errorf("multi source(fallback): all sources failed for %q: %w", key, errors.Join(errs...))
}

func (ms *MultiSource) loadFirstOK(ctx context.Context, key string) (string, error) {
	type result struct {
		code string
		err  error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan result, len(ms.sources))
	for _, s := range ms.sources {
		s := s
		go func() {
			code, err := s.Load(ctx, key)
			ch <- result{code, err}
		}()
	}

	var errs []error
	for i := 0; i < len(ms.sources); i++ {
		r := <-ch
		if r.err == nil {
			cancel() // tell other goroutines to bail out ASAP
			return r.code, nil
		}
		errs = append(errs, r.err)
	}
	return "", fmt.Errorf("multi source(first-ok): all sources failed for %q: %w", key, errors.Join(errs...))
}

// Close closes every sub-source and returns the aggregated error.
func (ms *MultiSource) Close() error {
	var errs []error
	for _, s := range ms.sources {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("multi source: close errors: %w", errors.Join(errs...))
}

// Watch delegates to the first sub-source that implements Watcher.
// It returns an error if no sub-source supports watching.
//
// The strategy is: try each sub-source in order; return the first successful Watch.
// This allows mixing sources with and without watch support.
func (ms *MultiSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	var errs []error
	for i, s := range ms.sources {
		if w, ok := s.(Watcher); ok {
			ch, err := w.Watch(ctx, key)
			if err == nil {
				return ch, nil
			}
			errs = append(errs, fmt.Errorf("source[%d]: %w", i, err))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("multi source: no watcher available: %w", errors.Join(errs...))
	}
	return nil, errors.New("multi source: none of the sub-sources implement Watcher")
}

// Compile-time assertion: *MultiSource implements source.ReadWatcher.
var _ ReadWatcher = (*MultiSource)(nil)
