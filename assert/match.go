package assert

import (
	"fmt"
	"slices"

	"github.com/intentd/tools/structured"
)

// MatchAssertions flattens match, what a JSON document must contain in its
// own shape, into json: assertions, one per leaf in key order: a literal is
// equals, a ${ … } or {{ … }} value is expr:. Maps need only the keys given;
// lists are matched element by element ([0], [1], …), as KUTTL and
// Chainsaw match them.
func MatchAssertions(match map[string]any) JSONAssertions {
	var out JSONAssertions
	flattenMatch("", match, &out)
	return out
}

func flattenMatch(path string, v any, out *JSONAssertions) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			flattenMatch(path+"."+k, t[k], out)
		}
	case []any:
		for i, e := range t {
			flattenMatch(fmt.Sprintf("%s[%d]", path, i), e, out)
		}
	case string:
		if structured.IsCEL(t) || structured.IsTemplate(t) {
			*out = append(*out, JSONAssertion{Path: path, TextAssertion: TextAssertion{Expr: t}})
			return
		}
		*out = append(*out, JSONAssertion{Path: path, TextAssertion: TextAssertion{Equals: t}})
	default:
		*out = append(*out, JSONAssertion{Path: path, TextAssertion: TextAssertion{Equals: structured.RenderJSONValue(t)}})
	}
}
