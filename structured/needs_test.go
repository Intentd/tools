package structured

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func needsOf(m map[string][]string) func(string) []string {
	return func(n string) []string { return m[n] }
}

// Each name comes after the ones it needs; where needs leave a choice, the
// names' order holds.
func TestOrderByNeeds(t *testing.T) {
	got, err := OrderByNeeds([]string{"worker", "api", "auth", "web"}, needsOf(map[string][]string{
		"worker": {"auth", "api"}, "api": {"auth"},
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"auth", "api", "worker", "web"}, got)
}

// A wave holds the names whose needs are all in earlier waves.
func TestWavesByNeeds(t *testing.T) {
	got, err := WavesByNeeds([]string{"worker", "api", "auth", "web"}, needsOf(map[string][]string{
		"worker": {"auth", "api"}, "api": {"auth"},
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"auth", "web"}, {"api"}, {"worker"}}, got)

	none, err := WavesByNeeds(nil, needsOf(nil))
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestOrderByNeeds_Errors(t *testing.T) {
	_, err := OrderByNeeds([]string{"a", "b"}, needsOf(map[string][]string{"a": {"b"}, "b": {"a"}}))
	assert.EqualError(t, err, "needs form a cycle: a → b → a")

	_, err = OrderByNeeds([]string{"a"}, needsOf(map[string][]string{"a": {"ghost"}}))
	assert.EqualError(t, err, `a needs "ghost", which is not declared`)

	_, err = OrderByNeeds([]string{"a"}, needsOf(map[string][]string{"a": {"a"}}))
	assert.EqualError(t, err, "a needs itself")
}
