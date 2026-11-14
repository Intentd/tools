package resolver

import (
	"fmt"
	"strings"

	"github.com/intentd/tools/structured"
)

// Field is how one submitted value is written into a destination map: as
// is at Path, through the Value template, or fanned out by Values
// (destination path → template). The templates see the value as .value.
type Field struct {
	// Path is the destination path (dot notation); empty is the key.
	Path string
	// Value is a template producing the value to write at Path.
	Value string
	// Values writes one template's result at each destination path; Path
	// and Value are ignored when it is set.
	Values map[string]string
}

// ApplyField writes value into dst as f says. key names the field: it is
// the destination when f.Path is empty, and errors read "field <key>: …".
func (r *Resolver) ApplyField(dst map[string]any, key string, f Field, value any) error {
	if len(f.Values) > 0 {
		rv := r.With("value", value)
		var errs []string
		for path, expr := range f.Values {
			out, err := rv.Resolve(expr)
			if err == nil {
				err = setPath(dst, path, out)
			}
			if err != nil {
				errs = append(errs, fmt.Sprintf("field %q: values[%q]: %s", key, path, err.Error()))
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil
	}

	path := f.Path
	if path == "" {
		path = key
	}
	if f.Value != "" {
		out, err := r.With("value", value).Resolve(f.Value)
		if err != nil {
			return fmt.Errorf("field %q: value: %s", key, err.Error())
		}
		return setPath(dst, path, out)
	}
	return setPath(dst, path, value)
}

func setPath(dst map[string]any, path string, value any) error {
	if structured.IsNestedPath(path) {
		return structured.SetNestedPath(dst, path, value)
	}
	dst[path] = value
	return nil
}
