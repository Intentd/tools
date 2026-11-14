package kube

import (
	"bytes"
	"errors"
	"io"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/intentd/tools/fileio"
)

// ReadObjects reads every object of a YAML or JSON manifest file; YAML
// documents are separated by ---, and empty ones are skipped.
func ReadObjects(path string) ([]map[string]any, error) {
	data, err := fileio.ReadLocal(path)
	if err != nil {
		return nil, err
	}
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		if len(obj) > 0 {
			out = append(out, obj)
		}
	}
}
