// Package schemagen writes editor JSON Schemas for YAML files from the Go
// types they decode into.
//
// Field names come from the yaml tags and descriptions from the types' Go
// comments. Types whose decoder accepts a shorthand (a string for an object,
// a list for a map) get it added as an alternative, and the scalars are
// widened the way the YAML decoder widens them, so the schema accepts what
// the decoder does:
//
//	g := &schemagen.Generator{
//		BaseURL: "https://example.dev/schema/v1/",
//		OutDir:  "website/static/schema/v1",
//		Local:   "example.dev/app/pkg/types",
//	}
//	if err := g.LoadComments(schemagen.Source{Module: "example.dev/app", Dirs: []string{"pkg/types"}}); err != nil { … }
//	path, err := g.Write("app.json", "App", types.App{}, schemagen.Const("kind", "App"))
package schemagen

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"
)

// Shorthand rewrites a reflected definition, usually to accept another form
// beside it (see Either).
type Shorthand func(*jsonschema.Schema) *jsonschema.Schema

// Generator writes schemas. Its fields are set once; LoadComments then
// Write, once per file.
type Generator struct {
	// BaseURL is prefixed to each file for its $id.
	BaseURL string
	// OutDir is where files are written.
	OutDir string
	// Local is the package whose types are named without a prefix. Types of
	// other packages are prefixed with their package name ("condition.TimeWindow").
	Local string
	// Prefixes overrides that prefix for packages under a path:
	// {"github.com/intentd/kubewright/core/types": "kubewright"}.
	Prefixes map[string]string
	// Shorthands are applied to the definitions of those names a schema reaches.
	Shorthands map[string]Shorthand

	comments map[string]string
}

// Source is where Go comments are read: directories of a module, found with
// go list -m, so a dependency or workspace module works as the main one does.
type Source struct {
	Module string
	Dirs   []string
}

// LoadComments reads the Go comments of the sources' types, so each field's
// description in the editor is its comment in the code.
func (g *Generator) LoadComments(sources ...Source) error {
	r := &jsonschema.Reflector{}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(wd)
	for _, src := range sources {
		out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", src.Module).Output()
		if err != nil {
			return fmt.Errorf("finding module %s: %w", src.Module, err)
		}
		if err := os.Chdir(strings.TrimSpace(string(out))); err != nil {
			return err
		}
		for _, dir := range src.Dirs {
			if err := r.AddGoComments(src.Module, dir); err != nil {
				return fmt.Errorf("reading comments of %s/%s: %w", src.Module, dir, err)
			}
		}
		if err := os.Chdir(wd); err != nil {
			return err
		}
	}
	g.comments = r.CommentMap
	return nil
}

// Pin changes the root definition of a schema before it is written.
type Pin func(root *jsonschema.Schema) error

// Enum allows only values for property prop of the root.
func Enum(prop string, values ...any) Pin {
	return func(root *jsonschema.Schema) error {
		p, ok := root.Properties.Get(prop)
		if !ok {
			return fmt.Errorf("no %s field", prop)
		}
		p.Enum = values
		return nil
	}
}

// Const fixes property prop of the root to value.
func Const(prop string, value any) Pin {
	return func(root *jsonschema.Schema) error {
		p, ok := root.Properties.Get(prop)
		if !ok {
			return fmt.Errorf("no %s field", prop)
		}
		p.Const = value
		return nil
	}
}

// Required sets the properties of the root that must be present.
func Required(props ...string) Pin {
	return func(root *jsonschema.Schema) error {
		root.Required = props
		return nil
	}
}

// Write reflects v into OutDir/file with its $id and title, applies pins to
// the root, adds the shorthands and widens the scalars. It returns the path
// written.
func (g *Generator) Write(file, title string, v any, pins ...Pin) (string, error) {
	r := &jsonschema.Reflector{
		FieldNameTag: "yaml",
		// yaml tags mark few fields omitempty, so "required" is not inferred
		// from them; a Required pin sets it.
		RequiredFromJSONSchemaTags: true,
		Namer:                      g.namer(),
		CommentMap:                 g.comments,
	}
	s := r.Reflect(v)
	s.ID = jsonschema.ID(g.BaseURL + file)
	s.Title = title
	tidy(s)
	if len(pins) > 0 {
		root := s.Definitions[strings.TrimPrefix(s.Ref, "#/$defs/")]
		if root == nil {
			return "", fmt.Errorf("%s: root definition %q not found", file, s.Ref)
		}
		for _, pin := range pins {
			if err := pin(root); err != nil {
				return "", fmt.Errorf("%s: %w", file, err)
			}
		}
	}
	for name, alt := range g.Shorthands {
		if def := s.Definitions[name]; def != nil {
			s.Definitions[name] = alt(def)
		}
	}
	out, err := loosen(s)
	if err != nil {
		return "", err
	}
	dst := path.Join(g.OutDir, file)
	if err := os.MkdirAll(path.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// namer names definitions by type, prefixing types from packages other than
// Local, and panics on two types that would share a name.
func (g *Generator) namer() func(reflect.Type) string {
	seen := map[string]string{}
	return func(t reflect.Type) string {
		name := t.Name()
		if p := t.PkgPath(); p != g.Local && p != "" {
			name = path.Base(p) + "." + t.Name()
			for under, prefix := range g.Prefixes {
				if strings.HasPrefix(p, under) {
					name = prefix + "." + t.Name()
				}
			}
		}
		if prev, ok := seen[name]; ok && prev != t.PkgPath() {
			panic(fmt.Sprintf("definition name %q used by %s and %s", name, prev, t.PkgPath()))
		}
		seen[name] = t.PkgPath()
		return name
	}
}

// Ref is a reference to definition name.
func Ref(name string) *jsonschema.Schema { return &jsonschema.Schema{Ref: "#/$defs/" + name} }

// Str is a string with a description.
func Str(desc string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Description: desc}
}

// Either keeps the reflected form and accepts short beside it.
func Either(short *jsonschema.Schema) Shorthand {
	return func(full *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{Description: full.Description, AnyOf: []*jsonschema.Schema{short, full}}
	}
}

// divider matches a section-divider comment such as "── Options ───", which
// reads as a heading in code but not as a field's description.
var divider = regexp.MustCompile(`^\s*─+.*─+\s*$`)

// tidy removes divider lines from every description.
func tidy(s *jsonschema.Schema) {
	clean := func(d string) string {
		var keep []string
		for _, line := range strings.Split(d, "\n") {
			if !divider.MatchString(line) {
				keep = append(keep, line)
			}
		}
		return strings.TrimSpace(strings.Join(keep, "\n"))
	}
	for _, def := range s.Definitions {
		def.Description = clean(def.Description)
		if def.Properties == nil {
			continue
		}
		for pair := def.Properties.Oldest(); pair != nil; pair = pair.Next() {
			pair.Value.Description = clean(pair.Value.Description)
		}
	}
}

// loosen accepts what the YAML decoder accepts beyond the Go types: any scalar
// for a string field (replicas: 1 decodes to "1"), and null for every field
// (an empty key decodes to the zero value). A string with an enum or const
// keeps its exact values.
func loosen(s *jsonschema.Schema) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case []any:
			for _, v := range n {
				walk(v)
			}
		case map[string]any:
			_, enum := n["enum"]
			_, cnst := n["const"]
			if n["type"] == "string" && !enum && !cnst {
				n["type"] = []any{"string", "number", "boolean"}
			}
			for _, v := range n {
				walk(v)
			}
			props, _ := n["properties"].(map[string]any)
			for name, p := range props {
				props[name] = nullable(p)
			}
		}
	}
	walk(doc)
	return json.MarshalIndent(doc, "", "  ")
}

// nullable lets a property schema also be null.
func nullable(p any) any {
	m, ok := p.(map[string]any)
	if !ok {
		return p
	}
	null := map[string]any{"type": "null"}
	switch t := m["type"].(type) {
	case string:
		m["type"] = []any{t, "null"}
		return m
	case []any:
		m["type"] = append(t, "null")
		return m
	}
	if ref, ok := m["$ref"]; ok {
		delete(m, "$ref")
		m["anyOf"] = []any{map[string]any{"$ref": ref}, null}
		return m
	}
	if alts, ok := m["anyOf"].([]any); ok {
		m["anyOf"] = append(alts, null)
	}
	return m
}
