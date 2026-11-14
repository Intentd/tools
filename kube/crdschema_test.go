package kube_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/intentd/tools/kube"
	"github.com/intentd/tools/kubeclient/kubefake"
)

func TestStorageAndSpecSchema(t *testing.T) {
	crd := &apiextensionsv1.CustomResourceDefinition{Spec: apiextensionsv1.CustomResourceDefinitionSpec{
		Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1"}, {Name: "v2", Storage: true}},
	}}
	v := kube.StorageVersion(crd)
	require.NotNil(t, v)
	assert.Equal(t, "v2", v.Name)
	assert.Nil(t, kube.SpecSchema(v), "no schema yet")

	kube.SetSpecSchema(v, apiextensionsv1.JSONSchemaProps{Type: "object", Description: "spec"})
	assert.Equal(t, "spec", kube.StorageSpecSchema(crd).Description)

	assert.Nil(t, kube.StorageVersion(&apiextensionsv1.CustomResourceDefinition{Spec: apiextensionsv1.CustomResourceDefinitionSpec{
		Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "a"}, {Name: "b"}},
	}}), "two versions and no storage one")
}

func TestOpensAnythingV1(t *testing.T) {
	yes := true
	assert.True(t, kube.OpensAnythingV1(&apiextensionsv1.JSONSchemaProps{XPreserveUnknownFields: &yes}))
	assert.True(t, kube.OpensAnythingV1(&apiextensionsv1.JSONSchemaProps{AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Allows: true}}))
	assert.False(t, kube.OpensAnythingV1(&apiextensionsv1.JSONSchemaProps{Type: "object"}))
}

func TestSchemaPathsV1(t *testing.T) {
	yes := true
	s := &apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
		"scaling": {Type: "object", Required: []string{"min"}, Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"min": {Type: "integer"},
		}},
		"open": {Type: "object", XPreserveUnknownFields: &yes},
	}, Required: []string{"scaling", "image"}}

	assert.True(t, kube.SchemaHoldsV1(s, "scaling.min"))
	assert.True(t, kube.SchemaHoldsV1(s, "open.any.path"), "an open object holds anything below it")
	assert.False(t, kube.SchemaHoldsV1(s, "scaling.max"))
	assert.Equal(t, "integer", kube.SchemaAtV1(s, "scaling.min").Type)
	assert.Nil(t, kube.SchemaAtV1(s, "open.any"), "SchemaAt returns only what is declared")

	kube.PutSchemaV1(s, "timeout.seconds", apiextensionsv1.JSONSchemaProps{Type: "integer"})
	assert.Equal(t, "object", s.Properties["timeout"].Type, "objects on the way are made")
	assert.Equal(t, "integer", kube.SchemaAtV1(s, "timeout.seconds").Type)

	kube.MakeOptionalV1(s, "scaling.min")
	assert.Equal(t, []string{"image"}, s.Required)
	assert.Empty(t, s.Properties["scaling"].Required)
	kube.MakeOptionalV1(s, "absent.path") // a path not declared is fine
}

func TestReadCRDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crd.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.example.com}
spec:
  group: example.com
  names: {kind: Widget, plural: widgets}
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec: {type: object, properties: {size: {type: integer}}}
`), 0o600))
	crd, err := kube.ReadCRDFile(path)
	require.NoError(t, err)
	assert.Equal(t, "integer", kube.SchemaAtV1(kube.StorageSpecSchema(crd), "size").Type)

	require.NoError(t, os.WriteFile(path, []byte("kind: ConfigMap\n"), 0o600))
	_, err = kube.ReadCRDFile(path)
	assert.ErrorContains(t, err, "expected kind: CustomResourceDefinition")
}

func TestGetCRD(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]any{"name": "widgets.example.com"},
		"spec":     map[string]any{"group": "example.com", "names": map[string]any{"kind": "Widget", "plural": "widgets"}},
	}}
	dyn := kubefake.NewDynamic(nil, map[schema.GroupVersionResource]string{kube.CRDResource: "CustomResourceDefinitionList"}, u)
	crd, err := kube.GetCRD(context.Background(), dyn, "widgets.example.com")
	require.NoError(t, err)
	assert.Equal(t, "Widget", crd.Spec.Names.Kind)
	_, err = kube.GetCRD(context.Background(), dyn, "absent.example.com")
	assert.ErrorContains(t, err, "read the CRD absent.example.com")
}

func TestReadObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "objs.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\n---\n---\nb: 2\n"), 0o600))
	objs, err := kube.ReadObjects(path)
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"a": float64(1)}, {"b": float64(2)}}, objs, "empty documents are skipped")
}
