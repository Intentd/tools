package structured

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMissingPaths_Equal(t *testing.T) {
	spec := map[string]interface{}{"a": int64(1), "b": map[string]interface{}{"c": "x"}}
	assert.Empty(t, MissingPaths(spec, spec))
}

func TestMissingPaths_ServerDefaultsIgnored(t *testing.T) {
	want := map[string]interface{}{"a": 1}
	got := map[string]interface{}{"a": float64(1), "defaulted": true, "nested": map[string]interface{}{"x": 1}}
	assert.Empty(t, MissingPaths(want, got))
}

func TestMissingPaths_DroppedAndChanged(t *testing.T) {
	want := map[string]interface{}{
		"keep":    "v",
		"dropped": "v",
		"nested":  map[string]interface{}{"changed": "old", "same": 1},
		"list":    []interface{}{"a", "b"},
	}
	got := map[string]interface{}{
		"keep":   "v",
		"nested": map[string]interface{}{"changed": "new", "same": 1},
		"list":   []interface{}{"a"},
	}
	assert.Equal(t, []string{"dropped", "list", "nested.changed"}, MissingPaths(want, got))
}

func TestMissingPaths_NilWantIsNotMissing(t *testing.T) {
	assert.Empty(t, MissingPaths(map[string]interface{}{"gone": nil}, map[string]interface{}{}))
}

func TestMissingPaths_TypeChange(t *testing.T) {
	want := map[string]interface{}{"a": map[string]interface{}{"b": 1}}
	got := map[string]interface{}{"a": "flat"}
	assert.Equal(t, []string{"a"}, MissingPaths(want, got))
}

func TestMissingPaths_NilGot(t *testing.T) {
	assert.Equal(t, []string{"a"}, MissingPaths(map[string]interface{}{"a": 1}, nil))
}

func TestMissingPaths_NilWant(t *testing.T) {
	assert.Empty(t, MissingPaths(nil, map[string]interface{}{"a": 1}))
}

// Diff names each path that differs: changed, added, removed, inside maps
// and lists, sorted; equal documents have none.
func TestDiff(t *testing.T) {
	want := map[string]any{
		"spec":     map[string]any{"replicas": 2, "ports": []any{80, 443}, "paused": false},
		"metadata": map[string]any{"name": "api"},
	}
	got := map[string]any{
		"spec":     map[string]any{"replicas": 3, "ports": []any{80}, "image": "api:2"},
		"metadata": map[string]any{"name": "api"},
	}
	var lines []string
	for _, d := range Diff(want, got) {
		lines = append(lines, d.String())
	}
	if want := []string{
		".spec.image: added api:2",
		".spec.paused: removed (was false)",
		".spec.ports[1]: removed (was 443)",
		".spec.replicas: 2 → 3",
	}; strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("Diff:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if d := Diff(want, want); len(d) != 0 {
		t.Errorf("equal documents: %v", d)
	}
}
