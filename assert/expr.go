package assert

import (
	"encoding/json"
	"fmt"
	"strings"

	"cel.dev/cel-go/cel"

	"github.com/intentd/tools/resolver"
	"github.com/intentd/tools/structured"
)

// exprVars are what an assertion's expression reads.
var exprVars = []string{"value", "object"}

// CheckExpr reports whether expr can run as an assertion's expression,
// without running it: ${ … } compiles as CEL with value and object, a
// {{ … }} template parses with KEL's functions. Anything else is an error:
// an expression is written in one of the two forms, as everywhere else.
func CheckExpr(expr string) error {
	if !structured.IsCEL(expr) && !structured.IsTemplate(expr) {
		return fmt.Errorf("expr %q: write ${ … } (CEL) or {{ … }} (a template)", expr)
	}
	return resolver.Check(expr, nil, exprVars)
}

// checkExpr evaluates expr with value and object, as tools' resolver does
// (CEL for ${ … }, a template for {{ … }}, KEL's functions in both); it
// must come out true.
func checkExpr(expr string, value, object any) error {
	if !structured.IsCEL(expr) && !structured.IsTemplate(expr) {
		return fmt.Errorf("expr %q: write ${ … } (CEL) or {{ … }} (a template)", expr)
	}
	r := resolver.NewWithFunctions(nil).
		WithCEL(cel.CrossTypeNumericComparisons(true)).
		With("value", wholeNumbers(value)).
		With("object", wholeNumbers(object))
	out, err := r.Resolve(expr)
	if err != nil {
		return fmt.Errorf("%s fails %s: %w", render(value), expr, err)
	}
	if strings.TrimSpace(out) != "true" {
		return fmt.Errorf("%s fails %s", render(value), expr)
	}
	return nil
}

// wholeNumbers is v with every whole JSON number an integer, so value == 2
// holds for a replica count; other numbers stay doubles.
func wholeNumbers(v any) any {
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return int64(t)
		}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = wholeNumbers(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = wholeNumbers(e)
		}
		return out
	}
	return v
}

// parseJSON is s decoded when it is JSON (a number stays a number), else s.
func parseJSON(s string) any {
	t := strings.TrimSpace(s)
	var v any
	if t != "" && json.Unmarshal([]byte(t), &v) == nil {
		return v
	}
	return t
}

// render is value as a message shows it.
func render(value any) string {
	if s, ok := value.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return structured.RenderJSONValue(value)
}
