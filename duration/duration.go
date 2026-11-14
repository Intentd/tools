// Package duration is a time.Duration written as text: Go's units ("15s",
// "2m", "1h") plus days, weeks, months and years ("10d", "2w", "3mo", "1y").
package duration

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration that unmarshals from a YAML string in Parse's
// format.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// units are the suffixes Parse adds to Go's. Only "mo" means months, so "m"
// stays minutes.
var units = []struct {
	suffix, name string
	size         time.Duration
}{
	{"y", "year", 365 * 24 * time.Hour},
	{"mo", "month", 30 * 24 * time.Hour},
	{"w", "week", 7 * 24 * time.Hour},
	{"d", "day", 24 * time.Hour},
}

// Parse reads a duration: d is 24h, w 7d, mo 30d, y 365d, fractions allowed
// ("1.5mo"); anything else goes to time.ParseDuration. "never" is zero.
func Parse(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if s == "never" {
		return 0, nil
	}
	s = strings.TrimSpace(s)
	for _, u := range units {
		if !strings.HasSuffix(s, u.suffix) {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid %s duration %q: %w", u.name, s, err)
		}
		return time.Duration(n * float64(u.size)), nil
	}
	return time.ParseDuration(s)
}
