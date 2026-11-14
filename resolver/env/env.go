// Package env resolves ${NAME} and ${{ env.NAME }} references: values
// declared for a run first, then the process environment. A bare $NAME is not
// expanded, so a bare $ (a strategic merge patch's $patch, a password) passes
// through; a command's shell expands $NAME itself.
//
// A caller can add contexts, as GitHub Actions has env and secrets:
// ${{ name.key }} resolves to the context's value for key, which may hold
// dots (${{ ecosystem.postgres.url }}), wherever ${{ env.NAME }} does.
package env

import (
	"maps"
	"os"
	"regexp"
	"sort"
)

// Resolver resolves ${NAME} and ${{ env.NAME }} through declared values, then
// the process environment, and ${{ name.key }} through its contexts. The zero
// Resolver declares nothing.
type Resolver struct {
	values   map[string]string
	contexts map[string]map[string]string
}

// New returns a Resolver over values, which it does not modify.
func New(values map[string]string) Resolver {
	return Resolver{values: values}
}

// envRef matches the forms Expand expands: ${NAME}, and ${{ env.NAME }} as in
// GitHub Actions.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// actionsRef matches only ${{ env.NAME }}: what expands in a command before
// its shell runs, as in GitHub Actions.
var actionsRef = regexp.MustCompile(`\$\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// contextRef matches ${{ name.key }}: a context's value. ${{ env.NAME }} is
// matched too, and left to envRef and actionsRef.
var contextRef = regexp.MustCompile(`\$\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*\}\}`)

// WithContext returns e with the context name: ${{ name.key }} resolves to
// values[key]. A reference into a context with no such key is unknown
// (UnknownContext); a reference to a context never added is left as
// written, for whatever else reads the text. e is not modified.
func (e Resolver) WithContext(name string, values map[string]string) Resolver {
	contexts := make(map[string]map[string]string, len(e.contexts)+1)
	maps.Copy(contexts, e.contexts)
	if values == nil {
		values = map[string]string{}
	}
	contexts[name] = values
	e.contexts = contexts
	return e
}

// LookupContext returns the value of key in the context name, and whether
// the context has it.
func (e Resolver) LookupContext(name, key string) (string, bool) {
	v, ok := e.contexts[name][key]
	return v, ok
}

// ContextKeys returns the keys of the context name, sorted: what a message
// about an unknown reference lists.
func (e Resolver) ContextKeys(name string) []string {
	keys := make([]string, 0, len(e.contexts[name]))
	for k := range e.contexts[name] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// expandContexts replaces each ${{ name.key }} of an added context with its
// value; an unknown key, or a context never added, stays as written.
func (e Resolver) expandContexts(s string) string {
	if len(e.contexts) == 0 {
		return s
	}
	return contextRef.ReplaceAllStringFunc(s, func(m string) string {
		sm := contextRef.FindStringSubmatch(m)
		if v, ok := e.LookupContext(sm[1], sm[2]); ok {
			return v
		}
		return m
	})
}

// refName is the name in an envRef match.
func refName(m []string) string {
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// Expand replaces each ${NAME} and ${{ env.NAME }} in s with its value:
// declared first, then the process environment; then each ${{ name.key }}
// of an added context. An unknown name is left as
// written, so a typo shows where it is used instead of becoming an empty
// string.
func (e Resolver) Expand(s string) string {
	s = envRef.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := e.Lookup(refName(envRef.FindStringSubmatch(m))); ok {
			return v
		}
		return m
	})
	return e.expandContexts(s)
}

// expandActions replaces only ${{ env.NAME }}, leaving $NAME and ${NAME} to a
// shell.
func (e Resolver) expandActions(s string) string {
	s = actionsRef.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := e.Lookup(actionsRef.FindStringSubmatch(m)[1]); ok {
			return v
		}
		return m
	})
	return e.expandContexts(s)
}

// Lookup returns the value for key, declared first, then the process
// environment, and whether it is set at all, so a caller can tell unset from
// empty.
func (e Resolver) Lookup(key string) (string, bool) {
	if v, ok := e.values[key]; ok {
		return v, true
	}
	return os.LookupEnv(key)
}

// Vars returns a copy of the declared values, for a command's Env. The
// process environment is not included.
func (e Resolver) Vars() Vars {
	vars := make(Vars, len(e.values))
	for k, v := range e.values {
		vars[k] = v
	}
	return vars
}
