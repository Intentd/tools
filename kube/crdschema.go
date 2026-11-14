package kube

import (
	"context"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/intentd/tools/fileio"
)

// OpensAnythingV1 is OpensAnything for the v1 schema type, as a CRD
// manifest or the API server holds it.
func OpensAnythingV1(s *apiextensionsv1.JSONSchemaProps) bool {
	if s.XPreserveUnknownFields != nil && *s.XPreserveUnknownFields {
		return true
	}
	return s.AdditionalProperties != nil && (s.AdditionalProperties.Allows || s.AdditionalProperties.Schema != nil)
}

// StorageVersion returns crd's storage version, else its only version; nil
// when neither exists.
func StorageVersion(crd *apiextensionsv1.CustomResourceDefinition) *apiextensionsv1.CustomResourceDefinitionVersion {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Storage {
			return &crd.Spec.Versions[i]
		}
	}
	if len(crd.Spec.Versions) == 1 {
		return &crd.Spec.Versions[0]
	}
	return nil
}

// SpecSchema returns the spec schema of version v, nil when it has none.
// The schema is a copy: write changes back with SetSpecSchema.
func SpecSchema(v *apiextensionsv1.CustomResourceDefinitionVersion) *apiextensionsv1.JSONSchemaProps {
	if v == nil || v.Schema == nil || v.Schema.OpenAPIV3Schema == nil {
		return nil
	}
	spec, ok := v.Schema.OpenAPIV3Schema.Properties["spec"]
	if !ok {
		return nil
	}
	return &spec
}

// SetSpecSchema sets the spec schema of version v, making the schema around
// it when v has none.
func SetSpecSchema(v *apiextensionsv1.CustomResourceDefinitionVersion, spec apiextensionsv1.JSONSchemaProps) {
	if v.Schema == nil {
		v.Schema = &apiextensionsv1.CustomResourceValidation{}
	}
	if v.Schema.OpenAPIV3Schema == nil {
		v.Schema.OpenAPIV3Schema = &apiextensionsv1.JSONSchemaProps{Type: "object"}
	}
	if v.Schema.OpenAPIV3Schema.Properties == nil {
		v.Schema.OpenAPIV3Schema.Properties = map[string]apiextensionsv1.JSONSchemaProps{}
	}
	v.Schema.OpenAPIV3Schema.Properties["spec"] = spec
}

// StorageSpecSchema returns the spec schema of crd's storage version.
func StorageSpecSchema(crd *apiextensionsv1.CustomResourceDefinition) *apiextensionsv1.JSONSchemaProps {
	return SpecSchema(StorageVersion(crd))
}

// ReadCRDFile reads a CRD manifest as the v1 type, to change and write back.
func ReadCRDFile(path string) (*apiextensionsv1.CustomResourceDefinition, error) {
	data, err := fileio.ReadLocal(path)
	if err != nil {
		return nil, err
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := sigsyaml.Unmarshal(data, &crd); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if crd.Kind != "CustomResourceDefinition" {
		return nil, fmt.Errorf("%s: expected kind: CustomResourceDefinition, got %q", path, crd.Kind)
	}
	return &crd, nil
}

// CRDResource is the resource of CustomResourceDefinitions.
var CRDResource = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// GetCRD reads the CRD named name ("<plural>.<group>") from the cluster.
func GetCRD(ctx context.Context, dyn dynamic.Interface, name string) (*apiextensionsv1.CustomResourceDefinition, error) {
	u, err := dyn.Resource(CRDResource).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read the CRD %s: %w", name, err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &crd); err != nil {
		return nil, fmt.Errorf("decode the CRD %s: %w", name, err)
	}
	return &crd, nil
}

// SchemaHoldsV1 reports whether s declares a dot path. Below an object that
// opens anything (see OpensAnythingV1) every path is held.
func SchemaHoldsV1(s *apiextensionsv1.JSONSchemaProps, path string) bool {
	cur := s
	for seg := range strings.SplitSeq(path, ".") {
		if cur == nil {
			return false
		}
		next, ok := cur.Properties[seg]
		if !ok {
			return OpensAnythingV1(cur)
		}
		cur = &next
	}
	return true
}

// SchemaAtV1 returns the schema s declares at a dot path, nil when it
// declares none.
func SchemaAtV1(s *apiextensionsv1.JSONSchemaProps, path string) *apiextensionsv1.JSONSchemaProps {
	cur := s
	for seg := range strings.SplitSeq(path, ".") {
		if cur == nil {
			return nil
		}
		next, ok := cur.Properties[seg]
		if !ok {
			return nil
		}
		cur = &next
	}
	return cur
}

// PutSchemaV1 declares v at a dot path under s, making the objects on the way.
func PutSchemaV1(s *apiextensionsv1.JSONSchemaProps, path string, v apiextensionsv1.JSONSchemaProps) {
	head, rest, nested := strings.Cut(path, ".")
	if s.Properties == nil {
		s.Properties = map[string]apiextensionsv1.JSONSchemaProps{}
	}
	if !nested {
		s.Properties[head] = v
		return
	}
	child, ok := s.Properties[head]
	if !ok {
		child = apiextensionsv1.JSONSchemaProps{Type: "object"}
	}
	PutSchemaV1(&child, rest, v)
	s.Properties[head] = child
}

// MakeOptionalV1 takes a dot path, and each object above it, out of their
// parents' required lists.
func MakeOptionalV1(s *apiextensionsv1.JSONSchemaProps, path string) {
	head, rest, nested := strings.Cut(path, ".")
	var req []string
	for _, r := range s.Required {
		if r != head {
			req = append(req, r)
		}
	}
	s.Required = req
	if !nested {
		return
	}
	child, ok := s.Properties[head]
	if !ok {
		return
	}
	MakeOptionalV1(&child, rest)
	s.Properties[head] = child
}
