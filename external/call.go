// Package external makes calls to services outside the cluster's API, HTTP,
// Prometheus, Postgres, Mongo, Redis, Kafka, and returns what they answered as
// a map templates and assertions read.
//
// A Call is the request as declared: its fields may be templates, resolved
// against a resolver when the call is made. Do makes one call.
package external

import (
	"context"
	"fmt"
	"net/http"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/logger"
	"github.com/intentd/tools/resolver"
)

// Protocol is how a call reaches its service.
type Protocol string

const (
	ProtocolHTTP       Protocol = "http"
	ProtocolRedis      Protocol = "redis"
	ProtocolPrometheus Protocol = "prometheus"
	ProtocolPostgres   Protocol = "postgres"
	ProtocolMongo      Protocol = "mongo"
	ProtocolGRPC       Protocol = "grpc"
	ProtocolKafka      Protocol = "kafka"
	ProtocolNATS       Protocol = "nats"
	ProtocolMQTT       Protocol = "mqtt"
)

// DefaultIdempotencyHeader is the header an idempotency key is sent in.
const DefaultIdempotencyHeader = "Idempotency-Key"

// Call is one external call. URL, Query, Body and header values may be
// templates.
type Call struct {
	Name     string
	Protocol Protocol // "" is HTTP
	URL      string
	Method   string // HTTP; "" is GET
	Headers  map[string]string
	Body     string
	Query    string // the protocol's query: PromQL, SQL, a Redis command, ...

	// ExpectedStatus are the HTTP statuses that succeed; empty: below 400.
	ExpectedStatus []int
	// Timeout is a duration ("10s"); "" is 10 seconds.
	Timeout string
	// CacheFor keeps a result for this long ("30s"); "" does not cache.
	CacheFor string
	// ContinueOnError returns a failed call's result instead of an error.
	ContinueOnError bool

	// When and Or decide whether the call is made; a call that isn't returns
	// a skipped result.
	When, Or condition.Conditions

	// Idempotency configures the key sent on POST and PATCH; nil sends the
	// default one.
	Idempotency *Idempotency

	// Credential is sent as "Bearer <credential>" in AuthHeader
	// (Authorization when empty); "" sends none.
	Credential string
	AuthHeader string
}

// Idempotency configures the idempotency key.
type Idempotency struct {
	Disabled bool   // send no key
	Header   string // "" is DefaultIdempotencyHeader
	Value    string // a template; "" derives the key from the request
}

// EffectiveHeader returns the header name, defaulted.
func (c *Idempotency) EffectiveHeader() string {
	if c == nil || c.Header == "" {
		return DefaultIdempotencyHeader
	}
	return c.Header
}

// IsDisabled reports whether no key is sent.
func (c *Idempotency) IsDisabled() bool { return c != nil && c.Disabled }

// Options are how Do reports and caches.
type Options struct {
	// Where labels errors, such as "external[2]".
	Where string
	// Scope keeps cached results apart, such as one kind's from another's.
	Scope string
	// Record is told about every call made (not skipped or cached): its
	// name, URL, duration and error ("" on success). nil records nothing.
	Record func(name, url string, durationSeconds float64, callErr string)
}

// Do makes c, its templates resolved with tmpl, and returns the result: for
// HTTP status, body, json, error and called; for other protocols result, raw,
// error and called. A call whose When/Or fail returns the skipped result.
//
// The error is set when the caller must stop: a template that can't be
// resolved, or a failure without ContinueOnError. A failed call's result is
// returned with it, so its error can be read.
func Do(ctx context.Context, tmpl *resolver.Resolver, c Call, o Options) (map[string]any, error) {
	log := logger.FromContext(ctx)

	if !condition.Evaluate(tmpl.Data(), c.When, c.Or, tmpl.Evaluator()) {
		log.Debug().Str("call", c.Name).Str("where", o.Where).Msg("external call skipped: when not met")
		return skippedResult(c.Protocol), nil
	}

	url, err := tmpl.Resolve(c.URL)
	if err != nil {
		return nil, fmt.Errorf("%s.url: %w", o.Where, err)
	}
	query, err := tmpl.Resolve(c.Query)
	if err != nil {
		return nil, fmt.Errorf("%s.query: %w", o.Where, err)
	}
	body, err := tmpl.Resolve(c.Body)
	if err != nil {
		return nil, fmt.Errorf("%s.body: %w", o.Where, err)
	}
	if c.Protocol == "" || c.Protocol == ProtocolHTTP || c.Protocol == ProtocolPrometheus {
		method := c.Method
		if method == "" {
			method = http.MethodGet
		}
		headers, err := RequestHeaders(tmpl.Resolve, c.Headers, c.Idempotency, ObjectIdentity(tmpl.Data()), method, url, body)
		if err != nil {
			return nil, fmt.Errorf("%s.%w", o.Where, err)
		}
		c.Headers = headers // c is this function's copy
	}

	var entry map[string]any
	cacheHit := false
	if c.CacheFor != "" {
		if cached, ok := cacheGet(cacheKey(o.Scope, c.Name, url, query)); ok {
			entry, cacheHit = cached, true
			log.Debug().Str("call", c.Name).Msg("external call: cache hit")
		}
	}

	if !cacheHit {
		entry, err = newProtocolClient(c.Protocol).Fetch(ctx, c, url, query, body, c.Credential)
		if err != nil {
			if !c.ContinueOnError {
				return nil, fmt.Errorf("external call %q: %w", c.Name, err)
			}
			log.Warn().Err(err).Str("call", c.Name).Msg("external call hard error: continuing")
			entry = errorResult(err.Error())
		}
		if c.CacheFor != "" {
			if ttl, err := core.ParseTimeDuration(c.CacheFor); err == nil {
				cacheSet(cacheKey(o.Scope, c.Name, url, query), entry, ttl)
			}
		}
	}

	callErr, _ := entry["error"].(string)
	if o.Record != nil {
		o.Record(c.Name, url, 0, callErr) // non-HTTP clients don't measure duration yet
	}

	if callErr != "" {
		log.Warn().Str("call", c.Name).Str("url", url).Str("error", callErr).Msg("external call failed")
		if !c.ContinueOnError {
			return entry, fmt.Errorf("external call %q failed: %s", c.Name, callErr)
		}
		return entry, nil
	}
	status, _ := entry["status"].(string)
	log.Debug().Str("call", c.Name).Str("url", url).Str("status", status).Msg("external call succeeded")
	return entry, nil
}

// skippedResult is the result of a call that wasn't made.
func skippedResult(protocol Protocol) map[string]any {
	switch protocol {
	case "", ProtocolHTTP:
		return map[string]any{"status": "", "body": "", "error": "", "called": "false"}
	default:
		return map[string]any{"result": "", "raw": map[string]any{}, "error": "", "called": "false"}
	}
}
