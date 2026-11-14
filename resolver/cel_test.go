package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEval(t *testing.T) {
	spec := map[string]any{"replicas": int64(3), "timeoutSeconds": int64(30), "name": "web"}
	r := New(map[string]any{"spec": spec}, nil)

	// Typed results: what a spec holds, not text.
	got, err := r.Eval(`spec.replicas * 2`)
	require.NoError(t, err)
	assert.Equal(t, int64(6), got)

	got, err = r.Eval(`spec.replicas > 1`)
	require.NoError(t, err)
	assert.Equal(t, true, got)

	got, err = r.Eval(`{"min": spec.replicas, "names": [spec.name, "sidecar"]}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"min": int64(3), "names": []any{"web", "sidecar"}}, got)

	// KEL's functions are there.
	obj := map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}
	got, err = r.With("object", obj).Eval(`conditionsState(object)`)
	require.NoError(t, err)
	assert.Equal(t, "healthy", got)
}

func TestEvalErrors(t *testing.T) {
	r := New(map[string]any{"spec": map[string]any{}}, nil)
	_, err := r.Eval(`spec.replicas +`)
	assert.ErrorContains(t, err, "compiling")
	_, err = r.Eval(`missing.field`)
	assert.ErrorContains(t, err, "compiling", "an undeclared variable fails at compile")
	_, err = r.Eval(`spec.replicas`)
	assert.ErrorContains(t, err, "evaluating", "a missing field fails at run time")
}

func TestEvalCompilesOnce(t *testing.T) {
	r := New(map[string]any{"spec": map[string]any{"n": int64(1)}}, nil)
	for i := 0; i < 3; i++ {
		_, err := r.With("value", int64(i)).Eval(`value + spec.n`)
		require.NoError(t, err)
	}
	// With shares the CEL setup, so one program serves every call.
	assert.Len(t, r.cel.programs, 1)
	assert.Len(t, r.cel.envs, 1)
}

func TestApplyFieldByHowTheValueIsWritten(t *testing.T) {
	r := New(nil, nil)

	dst := map[string]any{}
	require.NoError(t, r.ApplyField(dst, "timeout", Field{Path: "spec.timeout", Value: `${ string(value) + "s" }`}, int64(30)))
	assert.Equal(t, map[string]any{"spec": map[string]any{"timeout": "30s"}}, dst)

	// CEL keeps the type: an int stays an int, where a template writes text.
	dst = map[string]any{}
	require.NoError(t, r.ApplyField(dst, "replicas", Field{Path: "scaling.min", Value: `${ value * 2 }`}, int64(3)))
	assert.Equal(t, map[string]any{"scaling": map[string]any{"min": int64(6)}}, dst)

	dst = map[string]any{}
	require.NoError(t, r.ApplyField(dst, "replicas", Field{Path: "scaling.min", Value: `{{ .value }}`}, int64(3)))
	assert.Equal(t, map[string]any{"scaling": map[string]any{"min": "3"}}, dst)

	// Anything else is written as is.
	dst = map[string]any{}
	require.NoError(t, r.ApplyField(dst, "tier", Field{Value: "web"}, "ignored"))
	assert.Equal(t, map[string]any{"tier": "web"}, dst)

	// Values fan out, each written by how it is.
	dst = map[string]any{}
	require.NoError(t, r.ApplyField(dst, "cron", Field{Values: map[string]string{
		"schedule.length": `${ size(value) }`, "schedule.raw": `{{ .value }}`,
	}}, "5 4 * * *"))
	assert.Equal(t, map[string]any{"schedule": map[string]any{"length": int64(9), "raw": "5 4 * * *"}}, dst)
}

func TestResolveFormatsCEL(t *testing.T) {
	r := New(map[string]any{"spec": map[string]any{"replicas": int64(3), "ports": []any{int64(80)}}}, nil)
	out, err := r.Resolve(`${ spec.replicas + 1 }`)
	require.NoError(t, err)
	assert.Equal(t, "4", out, "text where a string is expected")
	out, err = r.Resolve(`${ spec.ports }`)
	require.NoError(t, err)
	assert.Equal(t, "[80]", out)
	out, err = r.Resolve(`prefix-${x}`)
	require.NoError(t, err)
	assert.Equal(t, "prefix-${x}", out, "only a whole ${ } value is CEL")
}

func TestCheck(t *testing.T) {
	assert.NoError(t, Check(`plain`, nil, nil))
	assert.NoError(t, Check(`{{ conditionsState .obj }}`, nil, nil))
	assert.ErrorContains(t, Check(`{{ conditionsState .obj "x" }}`, nil, nil), "conditionsState takes 1 argument, got 2")
	assert.NoError(t, Check(`${ value * 2 }`, nil, []string{"value"}))
	assert.Error(t, Check(`${ other * 2 }`, nil, []string{"value"}), "an undeclared variable")
	assert.Error(t, Check(`${ conditionsState(value, "x") }`, nil, []string{"value"}), "wrong arguments")
	assert.NoError(t, Check(`${ anything.size() > all }`, nil, nil), "vars unknown: any name is a variable")
	assert.ErrorContains(t, Check(`${ conditionsState(obj, "x") }`, nil, nil), "conditionsState", "functions are still checked")
	assert.Error(t, Check(`${ notAFunction(obj) }`, nil, nil), "unknown functions too")
	assert.Error(t, Check(`${ value * }`, nil, nil), "a syntax error")
}
