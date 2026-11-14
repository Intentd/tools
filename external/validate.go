package external

import (
	"fmt"

	"github.com/intentd/kel/core"
)

// Validate checks c's static shape, labelling errors with location: a known
// protocol, a URL, each protocol's required query, HTTP-only fields only on
// HTTP, and durations that parse. Templates are not resolved.
func Validate(location string, c Call) error {
	switch c.Protocol {
	case "", ProtocolHTTP, ProtocolPrometheus, ProtocolRedis, ProtocolPostgres, ProtocolMongo, ProtocolKafka:
	default:
		return fmt.Errorf("%s: protocol %q is not supported: http, prometheus, redis, postgres, mongo or kafka", location, c.Protocol)
	}
	if c.URL == "" {
		return fmt.Errorf("%s: url is required", location)
	}
	queryExample := map[Protocol]string{
		ProtocolPrometheus: "",
		ProtocolRedis:      ` (e.g. "GET mykey")`,
		ProtocolPostgres:   "",
		ProtocolMongo:      ` (e.g. "mydb.mycollection")`,
		ProtocolKafka:      ` (e.g. "group/topic" or "@topic")`,
	}
	if example, needsQuery := queryExample[c.Protocol]; needsQuery {
		if c.Query == "" {
			return fmt.Errorf("%s: query is required for protocol: %s%s", location, c.Protocol, example)
		}
		if c.Body != "" || c.Method != "" || len(c.Headers) > 0 || len(c.ExpectedStatus) > 0 {
			return fmt.Errorf("%s: body, method, headers, and expectedStatus are HTTP-only fields", location)
		}
	}
	if (c.Protocol == "" || c.Protocol == ProtocolHTTP) && c.Query != "" {
		return fmt.Errorf("%s: query is not used by protocol: http — use url: to encode query parameters", location)
	}
	for _, s := range c.ExpectedStatus {
		if s < 100 || s > 599 {
			return fmt.Errorf("%s: expectedStatus %d is not an HTTP status (100-599)", location, s)
		}
	}
	for field, d := range map[string]string{"cacheFor": c.CacheFor, "timeout": c.Timeout} {
		if d == "" {
			continue
		}
		if _, err := core.ParseTimeDuration(d); err != nil {
			return fmt.Errorf("%s: %s %q is not a valid duration: %w", location, field, d, err)
		}
	}
	return nil
}
