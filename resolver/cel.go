package resolver

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"

	"cel.dev/cel-go/cel"
	celast "cel.dev/cel-go/common/ast"

	"github.com/intentd/kel"
	kelcel "github.com/intentd/kel/cel"
	"github.com/intentd/tools/structured"
)

// celConfig is a Resolver's CEL setup, shared by the Resolvers With derives
// from it, so an expression is compiled once for all of them.
type celConfig struct {
	opts []cel.EnvOption

	mu       sync.Mutex
	envs     map[string]*cel.Env    // by the sorted names of the variables
	programs map[string]cel.Program // by variables and expression
}

// WithCEL returns a Resolver whose CEL environment has opts added to KEL's
// functions (other libraries, extensions). r is unchanged. Every resolver
// evaluates CEL; this only extends it.
func (r *Resolver) WithCEL(opts ...cel.EnvOption) *Resolver {
	out := *r
	out.cel = newCELConfig(opts)
	return &out
}

func newCELConfig(opts []cel.EnvOption) *celConfig {
	return &celConfig{
		opts:     append([]cel.EnvOption(nil), opts...),
		envs:     map[string]*cel.Env{},
		programs: map[string]cel.Program{},
	}
}

// Eval evaluates a bare CEL expression (no ${ }) against r's data and
// returns its value with its type: a bool, int64, float64, string, []any or
// map[string]any. Each top-level data key is a variable, so a Resolver
// With("value", v) evaluates `string(value) + "s"`.
func (r *Resolver) Eval(expr string) (any, error) {
	if r.cel == nil {
		r.cel = newCELConfig(nil) // a Resolver built without New
	}
	prg, err := r.cel.program(expr, keys(r.data))
	if err != nil {
		return nil, err
	}
	out, _, err := prg.Eval(r.data)
	if err != nil {
		return nil, fmt.Errorf("evaluating %q: %w", expr, err)
	}
	return kelcel.Native(out), nil
}

// program returns expr compiled for vars, compiling it on first use.
func (c *celConfig) program(expr string, vars []string) (cel.Program, error) {
	envKey := strings.Join(vars, ",")
	prgKey := envKey + "\x00" + expr

	c.mu.Lock()
	defer c.mu.Unlock()
	if prg, ok := c.programs[prgKey]; ok {
		return prg, nil
	}
	env, ok := c.envs[envKey]
	if !ok {
		var err error
		if env, err = kelcel.Env(vars, c.opts...); err != nil {
			return nil, err
		}
		c.envs[envKey] = env
	}
	ast, iss := env.Compile(expr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("compiling %q: %w", expr, iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("compiling %q: %w", expr, err)
	}
	c.programs[prgKey] = prg
	return prg, nil
}

// keys returns m's keys, sorted.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Check validates value without data, by how it is written: a template with
// kel.Check against funcs (KEL's when nil), a ${ } expression with
// kelcel.Check against vars (its data keys; when nil, every name it uses is
// taken as a variable, so functions and their arguments are still checked),
// anything else not at all. It is the one check for every place a resolver
// value is declared.
func Check(value string, funcs template.FuncMap, vars []string) error {
	switch {
	case structured.IsCEL(value):
		expr := structured.CELExpression(value)
		if vars == nil {
			names, err := identifiers(expr)
			if err != nil {
				return fmt.Errorf("parsing %q: %w", value, err)
			}
			vars = names
		}
		return kelcel.Check(expr, vars)
	case structured.IsTemplate(value):
		return kel.Check(value, funcs)
	}
	return nil
}

// identifiers are the names expr uses as variables, from its parsed form.
func identifiers(expr string) ([]string, error) {
	env, err := kelcel.Env(nil)
	if err != nil {
		return nil, err
	}
	parsed, iss := env.Parse(expr)
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range celast.MatchDescendants(celast.NavigateAST(parsed.NativeRep()), celast.KindMatcher(celast.IdentKind)) {
		if name := e.AsIdent(); !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}
