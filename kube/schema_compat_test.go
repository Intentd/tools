package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/utils/ptr"
)

func schemaObj(props map[string]apiextensions.JSONSchemaProps) *apiextensions.JSONSchemaProps {
	return &apiextensions.JSONSchemaProps{Type: "object", Properties: props}
}

func typ(t string) apiextensions.JSONSchemaProps { return apiextensions.JSONSchemaProps{Type: t} }

func TestSchemaLeafPaths(t *testing.T) {
	s := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"image":   typ("string"),
		"scaling": *schemaObj(map[string]apiextensions.JSONSchemaProps{"min": typ("integer")}),
		"ports": {Type: "array", Items: &apiextensions.JSONSchemaPropsOrArray{
			Schema: schemaObj(map[string]apiextensions.JSONSchemaProps{"port": typ("integer")}),
		}},
		"tags":  {Type: "array", Items: &apiextensions.JSONSchemaPropsOrArray{Schema: &apiextensions.JSONSchemaProps{Type: "string"}}},
		"extra": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
	})
	assert.Equal(t, map[string]string{
		"image":        "string",
		"scaling.min":  "integer",
		"ports[].port": "integer",
		"tags":         "array",
		"extra":        "object",
	}, SchemaLeafPaths(s))
}

func TestCompareSchemas_Compatible(t *testing.T) {
	src := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"image": typ("string"),
		"count": typ("integer"),
	})
	dst := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"image": typ("string"),
		"count": typ("number"),
		"new":   typ("boolean"),
	})
	assert.Empty(t, CompareSchemas(src, dst))
}

func TestCompareSchemas_MissingAndTypeChange(t *testing.T) {
	src := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"image":    typ("string"),
		"replicas": typ("integer"),
		"logLevel": typ("string"),
	})
	dst := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"image":    typ("string"),
		"replicas": typ("string"),
	})
	assert.Equal(t, []string{
		"logLevel: missing in destination",
		"replicas: source is integer, destination is string",
	}, CompareSchemas(src, dst))
}

func TestCompareSchemas_OpenDestinationHoldsAnything(t *testing.T) {
	src := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"config": *schemaObj(map[string]apiextensions.JSONSchemaProps{"a": typ("string")}),
		"labels": *schemaObj(map[string]apiextensions.JSONSchemaProps{"x": typ("string")}),
	})
	dst := schemaObj(map[string]apiextensions.JSONSchemaProps{
		"config": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
		"labels": {Type: "object", AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{Schema: &apiextensions.JSONSchemaProps{Type: "string"}}},
	})
	assert.Empty(t, CompareSchemas(src, dst))
}

func TestCompareSchemas_ArrayOfObjects(t *testing.T) {
	item := func(props map[string]apiextensions.JSONSchemaProps) apiextensions.JSONSchemaProps {
		return apiextensions.JSONSchemaProps{Type: "array", Items: &apiextensions.JSONSchemaPropsOrArray{Schema: schemaObj(props)}}
	}
	src := schemaObj(map[string]apiextensions.JSONSchemaProps{"ports": item(map[string]apiextensions.JSONSchemaProps{"port": typ("integer"), "name": typ("string")})})
	dst := schemaObj(map[string]apiextensions.JSONSchemaProps{"ports": item(map[string]apiextensions.JSONSchemaProps{"port": typ("integer")})})
	assert.Equal(t, []string{"ports[].name: missing in destination"}, CompareSchemas(src, dst))

	flat := schemaObj(map[string]apiextensions.JSONSchemaProps{"ports": typ("string")})
	assert.Equal(t, []string{
		"ports[].name: destination is string, not an array",
		"ports[].port: destination is string, not an array",
	}, CompareSchemas(src, flat))
}

func TestSplitPath(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "[]", "c"}, splitPath("a.b[].c"))
	assert.Equal(t, []string{"a"}, splitPath("a"))
}

func TestSchemaTypeAtAndTypeFits(t *testing.T) {
	yes := true
	s := &apiextensions.JSONSchemaProps{Properties: map[string]apiextensions.JSONSchemaProps{
		"replicas": {Type: "integer"},
		"open":     {Type: "object", XPreserveUnknownFields: &yes},
	}}
	typ, ok := SchemaTypeAt(s, "replicas")
	assert.True(t, ok)
	assert.Equal(t, "integer", typ)
	typ, ok = SchemaTypeAt(s, "open.x.y")
	assert.True(t, ok)
	assert.Empty(t, typ, "held with no known type")
	_, ok = SchemaTypeAt(s, "missing")
	assert.False(t, ok)

	assert.True(t, TypeFits("integer", "number"))
	assert.False(t, TypeFits("number", "integer"))
	assert.True(t, TypeFits("string", "string"))
}
