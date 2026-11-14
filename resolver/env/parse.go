package env

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// ParseDotEnv reads a .env file as Docker Compose's env_file does: one
// KEY=VALUE per line, blank lines and # comments skipped, an optional
// export prefix, and a value in matching single or double quotes taken
// without them. Values are literal: nothing is expanded.
func ParseDotEnv(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: want KEY=VALUE, got %q", n, sc.Text())
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		out[key] = value
	}
	return out, sc.Err()
}

// Vars are environment variables: names and their values.
type Vars map[string]string

// Inherit returns e over parent: parent's values, with e's own winning,
// as an import has its parent's env under its own. e is not modified.
func (e Vars) Inherit(parent Vars) Vars {
	if len(parent) == 0 {
		return e
	}
	out := make(Vars, len(e)+len(parent))
	for k, v := range parent {
		out[k] = v
	}
	for k, v := range e {
		out[k] = v
	}
	return out
}
