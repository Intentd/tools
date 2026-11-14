// Package env resolves ${NAME} and ${{ env.NAME }} references: values
// declared for a run first, then the process environment. A bare $NAME is not
// expanded, so a bare $ (a strategic merge patch's $patch, a password) passes
// through; a command's shell expands $NAME itself.
package env

import (
	"os"
	"regexp"
)

// Resolver resolves ${NAME} and ${{ env.NAME }} through declared values, then
// the process environment. The zero Resolver declares nothing.
type Resolver struct {
	values map[string]string
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

// refName is the name in an envRef match.
func refName(m []string) string {
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// Expand replaces each ${NAME} and ${{ env.NAME }} in s with its value:
// declared first, then the process environment. An unknown name is left as
// written, so a typo shows where it is used instead of becoming an empty
// string.
func (e Resolver) Expand(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := e.Lookup(refName(envRef.FindStringSubmatch(m))); ok {
			return v
		}
		return m
	})
}

// expandActions replaces only ${{ env.NAME }}, leaving $NAME and ${NAME} to a
// shell.
func (e Resolver) expandActions(s string) string {
	return actionsRef.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := e.Lookup(actionsRef.FindStringSubmatch(m)[1]); ok {
			return v
		}
		return m
	})
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

// Pairs returns the declared key=value entries for exec.Cmd.Env. The process
// environment is not included: append os.Environ() to keep it.
func (e Resolver) Pairs() []string {
	pairs := make([]string, 0, len(e.values))
	for k, v := range e.values {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}
