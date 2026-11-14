package structured

import (
	"fmt"
	"reflect"
	"sort"
)

// MissingPaths returns, sorted, the dotted paths of want that got does not
// hold with the same value. Maps are walked; any other value, lists included,
// is compared whole after JSON normalisation. Keys only in got are ignored,
// and a nil in want is not required to be present.
func MissingPaths(want, got map[string]interface{}) []string {
	gotNorm, _ := JSONNormalize(got).(map[string]interface{})
	wantNorm, _ := JSONNormalize(want).(map[string]interface{})
	var out []string
	walkMissing("", wantNorm, gotNorm, &out)
	sort.Strings(out)
	return out
}

func walkMissing(prefix string, want, got map[string]interface{}, out *[]string) {
	for k, wv := range want {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		gv, ok := got[k]
		if !ok {
			if wv != nil {
				*out = append(*out, path)
			}
			continue
		}
		wm, wantMap := wv.(map[string]interface{})
		gm, gotMap := gv.(map[string]interface{})
		if wantMap && gotMap {
			walkMissing(path, wm, gm, out)
			continue
		}
		if !reflect.DeepEqual(wv, gv) {
			*out = append(*out, path)
		}
	}
}

// Difference is one path where two documents differ: Want is nil for a
// path only in got (added), Got is nil for one only in want (removed).
type Difference struct {
	Path      string
	Want, Got any
}

// String is the difference as a line: .spec.replicas: 2 → 3.
func (d Difference) String() string {
	switch {
	case d.Want == nil:
		return d.Path + ": added " + RenderJSONValue(d.Got)
	case d.Got == nil:
		return d.Path + ": removed (was " + RenderJSONValue(d.Want) + ")"
	}
	return d.Path + ": " + RenderJSONValue(d.Want) + " → " + RenderJSONValue(d.Got)
}

// Diff returns every path where want and got differ, sorted by path: maps
// key by key, lists element by element ([0], [1], …), anything else by
// value. A map or list that differs only inside is reported at the paths
// inside it, not as a whole.
func Diff(want, got any) []Difference {
	var out []Difference
	diffAt("", want, got, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func diffAt(path string, want, got any, out *[]Difference) {
	wm, wIsMap := want.(map[string]any)
	gm, gIsMap := got.(map[string]any)
	if wIsMap && gIsMap {
		for k, wv := range wm {
			if gv, ok := gm[k]; ok {
				diffAt(path+"."+k, wv, gv, out)
			} else {
				*out = append(*out, Difference{Path: path + "." + k, Want: wv})
			}
		}
		for k, gv := range gm {
			if _, ok := wm[k]; !ok {
				*out = append(*out, Difference{Path: path + "." + k, Got: gv})
			}
		}
		return
	}
	wl, wIsList := want.([]any)
	gl, gIsList := got.([]any)
	if wIsList && gIsList {
		for i := 0; i < max(len(wl), len(gl)); i++ {
			p := fmt.Sprintf("%s[%d]", path, i)
			switch {
			case i >= len(gl):
				*out = append(*out, Difference{Path: p, Want: wl[i]})
			case i >= len(wl):
				*out = append(*out, Difference{Path: p, Got: gl[i]})
			default:
				diffAt(p, wl[i], gl[i], out)
			}
		}
		return
	}
	if !reflect.DeepEqual(want, got) {
		if path == "" {
			path = "."
		}
		*out = append(*out, Difference{Path: path, Want: want, Got: got})
	}
}
