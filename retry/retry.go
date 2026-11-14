package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/duration"
	"github.com/intentd/tools/schemagen"
	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
)

const (
	DefaultAttempts   = 3
	DefaultMax        = 30 * time.Second
	DefaultBase       = 500 * time.Millisecond
	DefaultMultiplier = 2.0
)

// BackoffConfig is declared under queue.retryBackoff or external[].retryBackoff.
// Shorthand: a plain duration string sets Initial only.
// Full form: set Initial, Max, Multiplier, and MaxAttempts individually.
type BackoffConfig struct {
	// Initial is the first backoff delay. Default: 500ms.
	Initial duration.Duration `yaml:"initial,omitempty" json:"initial,omitempty"`
	// Max caps the delay so it does not grow unboundedly. Default: 30s.
	Max duration.Duration `yaml:"max,omitempty" json:"max,omitempty"`
	// Multiplier scales the delay after each attempt. Default: 2.0.
	Multiplier float64 `yaml:"multiplier,omitempty" json:"multiplier,omitempty"`
	// MaxAttempts is the total number of calls including the first. Default: 3.
	MaxAttempts int `yaml:"maxAttempts,omitempty" json:"maxAttempts,omitempty"`
}

// UnmarshalYAML allows BackoffConfig to be written as either a plain duration
// string (shorthand for initial only) or the full struct form.
//
//	retryBackoff: 5s                    # shorthand — initial: 5s, defaults for the rest
//	retryBackoff:                       # full form
//	  initial: 100ms
//	  max: 10m
//	  multiplier: 2.0
//	  maxAttempts: 5
func (r *BackoffConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		d, err := core.ParseTimeDuration(value.Value)
		if err != nil {
			return fmt.Errorf("retryBackoff: %w", err)
		}
		r.Initial = duration.Duration{Duration: d}
		return nil
	}
	// Full struct form — avoid infinite recursion with alias type.
	type plain BackoffConfig
	return value.Decode((*plain)(r))
}

// JSONSchemaExtend accepts the initial delay beside the object, as UnmarshalYAML does.
func (BackoffConfig) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Initial delay, e.g. 500ms"))
}

// ToRetryDoOptions converts the declaration into retry.DoOptions, applying defaults.
func (r *BackoffConfig) ToRetryDoOptions() DoOptions {
	if r == nil {
		return DoOptions{}
	}
	return DoOptions{
		Base:        r.Initial.Duration,
		Max:         r.Max.Duration,
		Multiplier:  r.Multiplier,
		MaxAttempts: r.MaxAttempts,
	}
}

// WorstCaseDuration returns the maximum wall time a full retry sequence can
// take, assuming no jitter. Used by the validator to compare against resync.
func (r *BackoffConfig) WorstCaseDuration() time.Duration {
	opts := r.ToRetryDoOptions()
	opts.ApplyDefaults()
	delay := opts.Base
	var total time.Duration
	for i := 1; i < opts.MaxAttempts; i++ {
		total += delay
		next := time.Duration(float64(delay) * opts.Multiplier)
		if next > opts.Max {
			next = opts.Max
		}
		delay = next
	}
	return total
}

// DoOptions controls the behaviour of Do.
type DoOptions struct {
	// MaxAttempts is the total number of calls to fn (including the first). Default: 3.
	MaxAttempts int
	// Base is the initial backoff duration. Default: 500ms.
	Base time.Duration
	// Max caps the backoff so it does not grow unboundedly. Default: 30s.
	Max time.Duration
	// Multiplier scales the delay after each failed attempt. Default: 2.0.
	Multiplier float64
	// Retryable, when set, is called on every error. Returning false stops
	// retrying immediately and returns the error to the caller. nil means
	// every error is retryable.
	Retryable func(error) bool
}

func (o *DoOptions) ApplyDefaults() {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultAttempts
	}
	if o.Base <= 0 {
		o.Base = DefaultBase
	}
	if o.Max <= 0 {
		o.Max = DefaultMax
	}
	if o.Multiplier <= 0 {
		o.Multiplier = DefaultMultiplier
	}
}

// Do calls fn up to opts.MaxAttempts times with exponential backoff and ±50%
// jitter between attempts. ctx cancellation is honoured between attempts. If
// opts.Retryable is set and returns false the error is returned immediately
// without further attempts.
func Do[T any](ctx context.Context, opts DoOptions, fn func(ctx context.Context) (T, error)) (T, error) {
	opts.ApplyDefaults()
	delay := opts.Base
	var zero T
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		val, err := fn(ctx)
		if err == nil {
			return val, nil
		}
		if opts.Retryable != nil && !opts.Retryable(err) {
			return zero, err
		}
		if attempt == opts.MaxAttempts {
			return zero, err
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(Jitter(delay)):
		}
		next := time.Duration(float64(delay) * opts.Multiplier)
		if next > opts.Max {
			next = opts.Max
		}
		delay = next
	}
	return zero, nil
}
