package resolver_test

import (
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver"
)

var upper = template.FuncMap{"upper": strings.ToUpper}

func data() map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": "web"},
		"spec":     map[string]any{"replicas": 3.0, "tags": []any{"a", "b"}, "tier": "gold"},
	}
}

func TestResolve(t *testing.T) {
	r := resolver.New(data(), upper)
	for in, want := range map[string]string{
		"":                                    "",
		"plain":                               "plain",
		"{{ .metadata.name }}-app":            "web-app",
		"{{ upper .spec.tier }}":              "GOLD",
		"{{ .spec.missing }}":                 "",
		"  {{ .metadata.name }}  ":            "web",
		"{{ .spec.missing }}-{{ .spec.env }}": "-",
	} {
		got, err := r.Resolve(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestResolve_Errors(t *testing.T) {
	r := resolver.New(nil, nil)
	_, err := r.Resolve("{{ .a")
	assert.ErrorContains(t, err, "parsing")
	_, err = r.Resolve("{{ nope }}")
	assert.ErrorContains(t, err, "parsing")
}

func TestResolveStrict_ReportsMissing(t *testing.T) {
	r := resolver.New(data(), nil)
	out, missing, err := r.ResolveStrict("{{ .spec.team }}-{{ .spec.env }}")
	require.NoError(t, err)
	assert.Equal(t, "-", out)
	assert.True(t, missing)

	_, missing, _ = r.ResolveStrict("{{ .metadata.name }}")
	assert.False(t, missing)
}

func TestWith_LeavesOriginal(t *testing.T) {
	r := resolver.New(data(), nil)
	r2 := r.With("value", "old")
	got, _ := r2.Resolve("{{ .value }}")
	assert.Equal(t, "old", got)
	got, _ = r.Resolve("{{ .value }}")
	assert.Empty(t, got)
	assert.NotContains(t, r.Data(), "value")
}

func TestSlicesAndMaps(t *testing.T) {
	r := resolver.New(data(), nil)
	s, err := r.ResolveStringSlice([]string{"{{ .metadata.name }}", "{{ .none }}", "x"})
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "x"}, s)

	a, err := r.ResolveArgs([]string{"{{ .none }}", "{{ .metadata.name }}"}, "args")
	require.NoError(t, err)
	assert.Equal(t, []string{"", "web"}, a, "ResolveArgs keeps empty elements")

	_, err = r.ResolveArgs([]string{"ok", "{{ .a"}, "command")
	assert.ErrorContains(t, err, "command[1]")

	m, err := r.ResolveMap(map[string]string{"app": "{{ .metadata.name }}"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"app": "web"}, m)
	assert.Equal(t, map[string]string{"bad": ""}, r.ResolveMapLenient(map[string]string{"bad": "{{ .a"}))
}

func TestResolveRaw_KeepsType(t *testing.T) {
	r := resolver.New(data(), nil)
	assert.Equal(t, []any{"a", "b"}, r.ResolveRaw("{{ .spec.tags }}"))
	assert.Equal(t, 3.0, r.ResolveRaw("{{ .spec.replicas }}"))
	assert.Nil(t, r.ResolveRaw("{{ .spec.none.deeper }}"))
	assert.Nil(t, r.ResolveRaw("{{ }}"))
}

func TestFunctions(t *testing.T) {
	d := data()
	fns := resolver.Functions(upper, func() map[string]any { return d },
		resolver.Function{Name: "isGold", Expression: `{{ eq .spec.tier "gold" }}`},
		resolver.Function{Name: "label", Expression: `{{ upper .metadata.name }}-{{ if isGold }}g{{ end }}`},
	)
	r := resolver.New(d, fns)
	got, err := r.Resolve("{{ label }}")
	require.NoError(t, err)
	assert.Equal(t, "WEB-g", got, "a function calls base functions and other functions")

	assert.Equal(t, true, fns["isGold"].(func() any)(), `"true" becomes a bool`)
	d["spec"].(map[string]any)["tier"] = "silver"
	assert.Equal(t, false, fns["isGold"].(func() any)(), "a function reads the data when called")
	assert.NotContains(t, upper, "isGold", "base is not changed")
}

func TestValues(t *testing.T) {
	r := resolver.New(nil, resolver.Values(upper, []string{"labelsChanged"}, map[string]string{"labelsChanged": "true"}))
	got, err := r.Resolve(`{{ if eq labelsChanged "true" }}{{ upper "yes" }}{{ end }}`)
	require.NoError(t, err)
	assert.Equal(t, "YES", got)

	stubs := resolver.New(nil, resolver.Values(nil, []string{"labelsChanged"}, nil))
	got, err = stubs.Resolve("{{ labelsChanged }}")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEvaluate_ResolvesTemplatesInConditions(t *testing.T) {
	r := resolver.New(data(), upper)
	when := condition.Conditions{{Field: "{{ upper .spec.tier }}", Equals: "GOLD"}}
	assert.True(t, r.Evaluate(when, nil))
	assert.False(t, r.Evaluate(condition.Conditions{{Field: "spec.tier", Equals: "{{ upper .spec.tier }}"}}, nil))
}

func TestObjectToMap(t *testing.T) {
	u := &unstructured.Unstructured{Object: data()}
	m, err := resolver.ObjectToMap(u)
	require.NoError(t, err)
	m["added"] = true
	assert.NotContains(t, u.Object, "added", "the object's map is copied")
}

func TestNewWithFunctionsAndEnv(t *testing.T) {
	r := resolver.NewWithFunctions(nil, resolver.Function{Name: "greeting", Expression: "hello"}).WithEnv(map[string]string{"NAME": "jig"})
	got, err := r.Resolve("{{ greeting }} {{ .env.NAME }}")
	if err != nil || got != "hello jig" {
		t.Fatalf("got %q, %v", got, err)
	}
	if v, err := r.Value("${ env.NAME + '!' }"); err != nil || v != "jig!" {
		t.Fatalf("CEL sees env: got %v, %v", v, err)
	}
	if resolver.NewWithFunctions(nil).WithEnv(nil).Data()["env"] != nil {
		t.Error("an empty env adds nothing")
	}
}
