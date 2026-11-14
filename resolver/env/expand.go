package env

import (
	"os"
	"reflect"
	"sort"
	"strings"
)

// ExpandAll returns a copy of v with ${NAME}, ${{ env.NAME }} and the
// contexts' ${{ name.key }} expanded through env in every string it holds, so the code that uses v sees only
// final values. Fields tagged env:"-" keep their text, such as the env
// declarations the values come from. Fields tagged env:"shell" (a command)
// expand only ${{ env.NAME }}, as in GitHub Actions: their shell expands
// $NAME and ${NAME} from the environment it is given. v is not modified.
func ExpandAll[T any](env Resolver, v T) T {
	out := walkEnv(reflect.ValueOf(&v).Elem(), env.Expand, env.expandActions)
	return out.Interface().(T)
}

// Unknown returns the names referenced in v (${NAME} and ${{ env.NAME }}; in
// env:"shell" fields only ${{ env.NAME }}), outside fields tagged env:"-",
// that env cannot resolve: declared nowhere and unset in the process
// environment. Sorted, each name once.
func Unknown(env Resolver, v any) []string {
	seen := map[string]bool{}
	check := func(name string) {
		if _, ok := env.Lookup(name); !ok {
			seen[name] = true
		}
	}
	walkEnv(reflect.ValueOf(v), func(s string) string {
		for _, m := range envRef.FindAllStringSubmatch(s, -1) {
			check(refName(m))
		}
		return s
	}, func(s string) string {
		for _, m := range actionsRef.FindAllStringSubmatch(s, -1) {
			check(m[1])
		}
		return s
	})
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// UnknownContext returns the references in v, as name.key, into an added
// context that has no such key, outside fields tagged env:"-". A context
// never added is not checked. Sorted, each reference once.
func UnknownContext(env Resolver, v any) []string {
	seen := map[string]bool{}
	check := func(s string) string {
		for _, m := range contextRef.FindAllStringSubmatch(s, -1) {
			if _, added := env.contexts[m[1]]; !added {
				continue
			}
			if _, ok := env.LookupContext(m[1], m[2]); !ok {
				seen[m[1]+"."+m[2]] = true
			}
		}
		return s
	}
	walkEnv(reflect.ValueOf(v), check, check)
	refs := make([]string, 0, len(seen))
	for r := range seen {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	return refs
}

// Map returns every value env resolves: the process environment overlaid by
// the declared env.
func (e Resolver) Map() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	for k, v := range e.values {
		out[k] = v
	}
	return out
}

// walkEnv returns a deep copy of v with fn applied to every string: shell
// instead under struct fields tagged env:"shell", nothing under env:"-".
// Map keys are kept as written.
func walkEnv(v reflect.Value, fn, shell func(string) string) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		out := reflect.New(v.Type()).Elem()
		out.SetString(fn(v.String()))
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			switch f.Tag.Get("env") {
			case "-":
			case "shell":
				out.Field(i).Set(walkEnv(v.Field(i), shell, shell))
			default:
				out.Field(i).Set(walkEnv(v.Field(i), fn, shell))
			}
		}
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(walkEnv(v.Elem(), fn, shell))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(walkEnv(v.Index(i), fn, shell))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for iter := v.MapRange(); iter.Next(); {
			out.SetMapIndex(iter.Key(), walkEnv(iter.Value(), fn, shell))
		}
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(walkEnv(v.Elem(), fn, shell))
		return out
	}
	return v
}
