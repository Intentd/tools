package duration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/intentd/tools/duration"
)

func TestParse(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{
		"30s":   30 * time.Second,
		"5m":    5 * time.Minute,
		"12h":   12 * time.Hour,
		"10d":   10 * day,
		"2w":    14 * day,
		"3mo":   90 * day,
		"1y":    365 * day,
		"0.5y":  time.Duration(0.5 * float64(365*day)),
		"1.5mo": time.Duration(1.5 * float64(30*day)),
		" 2d ":  2 * day,
		"never": 0,
	} {
		got, err := duration.Parse(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestParse_Invalid(t *testing.T) {
	for _, in := range []string{"", "xy", "xmo", "xw", "xd", "5x"} {
		_, err := duration.Parse(in)
		assert.Error(t, err, in)
	}
}

func TestDuration_YAML(t *testing.T) {
	var v struct {
		D duration.Duration `yaml:"d"`
		E duration.Duration `yaml:"e"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("d: 2w\ne: \"\"\n"), &v))
	assert.Equal(t, 14*24*time.Hour, v.D.Duration)
	assert.Zero(t, v.E.Duration)

	out, err := yaml.Marshal(v)
	require.NoError(t, err)
	assert.Contains(t, string(out), "d: 336h0m0s")
}
