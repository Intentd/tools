package assert

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// match: is what the JSON output must contain, in its own shape: keys not
// given are ignored, lists go element by element, expressions must be true.
func TestMatch(t *testing.T) {
	doc := `{"spec":{"replicas":2,"paused":false,"containers":[{"name":"api","image":"shop-api:dev"},{"name":"side"}]},"status":{"ready":true}}`
	ok := Assertions{Match: map[string]any{
		"spec": map[string]any{
			"replicas":   2,
			"containers": []any{map[string]any{"name": "api", "image": `${ value.startsWith("shop-api:") }`}},
		},
		"status": map[string]any{"ready": true},
	}}
	assert.NoError(t, ok.Check(doc))
	assert.False(t, ok.Empty())

	bad := Assertions{Match: map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"image": `{{ hasSuffix .value ":prod" }}`}}}}}
	err := bad.Check(doc)
	assert.ErrorContains(t, err, `match: json .spec.containers[0].image: "shop-api:dev" fails`)

	assert.Equal(t, JSONAssertions{
		{Path: ".a[0].b", TextAssertion: TextAssertion{Equals: "1"}},
		{Path: ".c", TextAssertion: TextAssertion{Expr: "${ value > 1 }"}},
	}, MatchAssertions(map[string]any{"a": []any{map[string]any{"b": 1}}, "c": "${ value > 1 }"}))
}

// capture: saves the value at a path, by name; it asserts nothing itself.
func TestCaptured(t *testing.T) {
	doc := `{"metadata":{"uid":"abc"},"status":{"restarts":2}}`
	a := JSONAssertions{
		{Path: ".metadata.uid", Capture: "UID"},
		{Path: ".status.restarts", Capture: "RESTARTS"},
		{Path: ".status.restarts", TextAssertion: TextAssertion{Equals: "2"}},
	}
	assert.NoError(t, Assertions{JSON: a}.Check(doc))
	assert.Equal(t, map[string]string{"UID": "abc", "RESTARTS": "2"}, a.Captured(doc))
	assert.Nil(t, JSONAssertions{{Path: ".x", TextAssertion: TextAssertion{Equals: "1"}}}.Captured(doc))
}
