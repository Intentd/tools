// Package resolver evaluates Go text/template expressions against a data
// map with a function map. A value without "{{" is returned as is; a missing
// key resolves to "" (missingkey=zero, with "<no value>" stripped). The
// function map is the caller's: KEL's, the caller's own, and Functions
// compiled from name/expression pairs.
//
//	r := resolver.New(map[string]any{"spec": spec}, kelFuncs)
//	out, err := r.Resolve("{{ .spec.name }}-app")
package resolver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/kubeclient"
	"github.com/intentd/tools/structured"
)

// Resolver evaluates expressions against data with funcs. It does not
// change data.
type Resolver struct {
	data  map[string]any
	funcs template.FuncMap
}

// New returns a Resolver over data with funcs. data is read, never written;
// a nil data is an empty map.
func New(data map[string]any, funcs template.FuncMap) *Resolver {
	if data == nil {
		data = map[string]any{}
	}
	return &Resolver{data: data, funcs: funcs}
}

// Data returns the data expressions are evaluated against. Do not mutate it.
func (r *Resolver) Data() map[string]any { return r.data }

// Funcs returns the function map.
func (r *Resolver) Funcs() template.FuncMap { return r.funcs }

// With returns a Resolver whose data is r's plus key set to value; r is
// unchanged (the data map is copied one level deep).
func (r *Resolver) With(key string, value any) *Resolver {
	data := make(map[string]any, len(r.data)+1)
	maps.Copy(data, r.data)
	data[key] = value
	return &Resolver{data: data, funcs: r.funcs}
}

// Resolve evaluates value. Without "{{" it is returned as is.
func (r *Resolver) Resolve(value string) (string, error) {
	out, _, err := r.ResolveStrict(value)
	return out, err
}

// ResolveStrict is Resolve that also reports whether a referenced key was
// missing, so a composite like "{{ .a }}-{{ .b }}" that resolves to "-" can
// be told apart from a real value.
func (r *Resolver) ResolveStrict(value string) (out string, missing bool, err error) {
	if value == "" || !structured.IsTemplate(value) {
		return value, false, nil
	}
	tmpl, err := template.New("f").Option("missingkey=zero").Funcs(r.funcs).Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("parsing %q: %w", value, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, r.data); err != nil {
		return "", false, fmt.Errorf("executing %q: %w", value, err)
	}
	// missingkey=zero renders a missing key as "<no value>".
	raw := strings.TrimSpace(buf.String())
	return strings.ReplaceAll(raw, "<no value>", ""), strings.Contains(raw, "<no value>"), nil
}

// Render evaluates tmpl, reporting failure as false instead of an error.
func (r *Resolver) Render(tmpl string) (string, bool) {
	out, err := r.Resolve(tmpl)
	if err != nil {
		return "", false
	}
	return out, true
}

// Evaluator returns a condition.TemplateEvaluator bound to r, so a
// condition's field or value written as a template is resolved by r.
func (r *Resolver) Evaluator() condition.TemplateEvaluator { return r.Render }

// Evaluate evaluates when (all must pass) and or (one must pass) against
// r's data, resolving templates in them with r.
func (r *Resolver) Evaluate(when, or condition.Conditions) bool {
	return condition.Evaluate(r.data, when, or, r.Evaluator())
}

// ResolveMap resolves each value of m; keys are not templates.
func (r *Resolver) ResolveMap(m map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, v := range m {
		rv, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
		out[k] = rv
	}
	return out, nil
}

// ResolveMapLenient is ResolveMap that never fails: a value that fails to
// resolve becomes "".
func (r *Resolver) ResolveMapLenient(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k], _ = r.Resolve(v)
	}
	return out
}

// ResolveStringSlice resolves each element and drops those that resolve to "".
func (r *Resolver) ResolveStringSlice(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for i, v := range values {
		rv, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("index %d: %w", i, err)
		}
		if rv != "" {
			out = append(out, rv)
		}
	}
	return out, nil
}

// ResolveArgs resolves each element and keeps those that resolve to "", so
// the ones after them stay in place. field names the list in errors.
func (r *Resolver) ResolveArgs(values []string, field string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for i, v := range values {
		rv, err := r.Resolve(v)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", field, i, err)
		}
		out = append(out, rv)
	}
	return out, nil
}

// ResolveRaw returns the value at the path in a "{{ .a.b }}" expression with
// its type kept ([]any, map, string, float64, bool), or nil when the path
// does not exist.
func (r *Resolver) ResolveRaw(expr string) any {
	path := strings.TrimSpace(expr)
	path = strings.TrimPrefix(path, "{{")
	path = strings.TrimSuffix(path, "}}")
	path = strings.TrimPrefix(strings.TrimSpace(path), ".")
	if path == "" {
		return nil
	}
	var current any = r.data
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		if current, ok = m[part]; !ok {
			return nil
		}
	}
	return current
}

// Function is an expression called by name from other expressions.
type Function struct {
	Name       string
	Expression string
}

// Functions returns base plus each fn compiled under its name. A function is
// evaluated against data() when called, so it sees the data of the moment;
// it may call base functions and the other fns. Its result is "true" or
// "false" as a bool, otherwise the text.
func Functions(base template.FuncMap, data func() map[string]any, fns ...Function) template.FuncMap {
	merged := make(template.FuncMap, len(base)+len(fns))
	maps.Copy(merged, base)
	for _, fn := range fns {
		expr := fn.Expression
		merged[fn.Name] = func() any {
			tmpl, err := template.New("").Option("missingkey=zero").Funcs(merged).Parse(expr)
			if err != nil {
				return ""
			}
			var buf bytes.Buffer
			_ = tmpl.Execute(&buf, data())
			out := strings.TrimSpace(strings.ReplaceAll(buf.String(), "<no value>", ""))
			switch out {
			case "true":
				return true
			case "false":
				return false
			default:
				return out
			}
		}
	}
	return merged
}

// Values returns base plus each name as a function returning values[name]:
// names known before their values, such as event-time flags. A nil values
// gives stubs returning "", enough to parse expressions that call them.
func Values(base template.FuncMap, names []string, values map[string]string) template.FuncMap {
	merged := make(template.FuncMap, len(base)+len(names))
	maps.Copy(merged, base)
	for _, name := range names {
		n := name
		merged[n] = func() string { return values[n] }
	}
	return merged
}

// ObjectToMap returns obj as the map expressions see: an unstructured object's
// own map (copied one level deep), a typed one through its JSON form, so
// .metadata, .spec and .status read the same either way.
func ObjectToMap(obj kubeclient.Object) (map[string]any, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return maps.Clone(u.Object), nil
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("object to map: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("object to map: %w", err)
	}
	return out, nil
}
