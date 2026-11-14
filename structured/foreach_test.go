package structured

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Items are the rows as written.
func TestForEach_Items(t *testing.T) {
	rows, err := ForEach{Items: []any{"a", map[string]any{"ref": "main"}}}.Rows()
	require.NoError(t, err)
	assert.Equal(t, []any{"a", map[string]any{"ref": "main"}}, rows)
}

// A matrix is every combination, keys in name order with the first varying
// slowest; exclude drops the rows it matches, include adds rows after.
func TestForEach_Matrix(t *testing.T) {
	rows, err := ForEach{
		Matrix:  map[string][]any{"ref": {"main", "release/1.3"}, "dockerfile": {"Dockerfile", "Dockerfile.prod"}},
		Exclude: []map[string]any{{"ref": "release/1.3", "dockerfile": "Dockerfile.prod"}},
		Include: []map[string]any{{"ref": "main", "dockerfile": "Dockerfile.debug"}},
	}.Rows()
	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"dockerfile": "Dockerfile", "ref": "main"},
		map[string]any{"dockerfile": "Dockerfile", "ref": "release/1.3"},
		map[string]any{"dockerfile": "Dockerfile.prod", "ref": "main"},
		map[string]any{"ref": "main", "dockerfile": "Dockerfile.debug"},
	}, rows)
}

func TestForEach_Validate(t *testing.T) {
	for want, f := range map[string]ForEach{
		"not both":               {Items: []any{1}, Matrix: map[string][]any{"a": {1}}},
		"set items or matrix":    {},
		"go with a matrix":       {Items: []any{1}, Exclude: []map[string]any{{"a": 1}}},
		"matrix.a has no values": {Matrix: map[string][]any{"a": {}}},
		"exclude[0].b is not a":  {Matrix: map[string][]any{"a": {1}}, Exclude: []map[string]any{{"b": 1}}},
		"include[0] has no a":    {Matrix: map[string][]any{"a": {1}}, Include: []map[string]any{{"b": 1}}},
	} {
		_, err := f.Rows()
		assert.ErrorContains(t, err, want)
	}
}

// Each entry with forEach is repeated once per row, in place, one with no
// rows produces nothing; the others pass through.
func TestExpandForEach(t *testing.T) {
	type entry struct {
		name string
		each []any
	}
	got, err := ExpandForEach([]entry{{name: "a"}, {name: "b", each: []any{"x", "y"}}, {name: "c"}, {name: "empty", each: []any{}}},
		func(e entry) (bool, []any, error) { return e.each != nil, e.each, nil },
		func(e entry, i int, item any, r int) (entry, error) {
			return entry{name: fmt.Sprintf("%s-%d-%d-%v", e.name, i, r, item)}, nil
		})
	require.NoError(t, err)
	var names []string
	for _, e := range got {
		names = append(names, e.name)
	}
	assert.Equal(t, []string{"a", "b-1-0-x", "b-1-1-y", "c"}, names)
}
