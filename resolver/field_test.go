package resolver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/resolver"
)

func TestApplyField(t *testing.T) {
	r := resolver.New(map[string]any{"metadata": map[string]any{"name": "web"}}, upper)

	dst := map[string]any{}
	require.NoError(t, r.ApplyField(dst, "replicas", resolver.Field{}, 3))
	assert.Equal(t, 3, dst["replicas"], "no transform writes the value as is at the key")

	require.NoError(t, r.ApplyField(dst, "image", resolver.Field{Path: "container.image", Value: "{{ upper .value }}:1"}, "nginx"))
	assert.Equal(t, map[string]any{"image": "NGINX:1"}, dst["container"])

	require.NoError(t, r.ApplyField(dst, "env", resolver.Field{Values: map[string]string{
		"tier":     "{{ .value }}",
		"labels.o": "{{ .metadata.name }}-{{ .value }}",
	}}, "prod"))
	assert.Equal(t, "prod", dst["tier"])
	assert.Equal(t, map[string]any{"o": "web-prod"}, dst["labels"])
}

func TestApplyField_Errors(t *testing.T) {
	r := resolver.New(nil, nil)
	err := r.ApplyField(map[string]any{}, "x", resolver.Field{Value: "{{ .value"}, 1)
	assert.ErrorContains(t, err, `field "x": value:`)

	err = r.ApplyField(map[string]any{"a": "flat"}, "x", resolver.Field{Values: map[string]string{"a.b": "v"}}, 1)
	assert.ErrorContains(t, err, `field "x": values["a.b"]:`)
}
