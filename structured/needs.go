package structured

import (
	"fmt"
	"slices"
	"strings"
)

// OrderByNeeds returns names with each after the ones it needs, as GitHub
// Actions jobs and KOME migrations run: the order to do them one at a time.
// needs returns what a name needs. Where needs leave a choice, names keep
// their order, so the result is the same every run. A name that needs
// itself, an unknown name and a cycle are errors naming the items involved.
func OrderByNeeds(names []string, needs func(string) []string) ([]string, error) {
	known := map[string]bool{}
	for _, n := range names {
		known[n] = true
	}
	state := map[string]int{} // 0 unseen, 1 in progress, 2 done
	var out []string
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("needs form a cycle: %s", strings.Join(append(path, name), " → "))
		}
		if !known[name] {
			return fmt.Errorf("%s needs %q, which is not declared", path[len(path)-1], name)
		}
		state[name] = 1
		deps := slices.Clone(needs(name))
		slices.Sort(deps)
		for _, d := range deps {
			if d == name {
				return fmt.Errorf("%s needs itself", name)
			}
			if err := visit(d, append(slices.Clone(path), name)); err != nil {
				return err
			}
		}
		state[name] = 2
		out = append(out, name)
		return nil
	}
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// WavesByNeeds groups names into levels: a wave holds the names whose needs
// are all in earlier waves, so the names in a wave can be done together.
// Each wave keeps the names' order. Errors are OrderByNeeds's.
func WavesByNeeds(names []string, needs func(string) []string) ([][]string, error) {
	order, err := OrderByNeeds(names, needs)
	if err != nil {
		return nil, err
	}
	level := map[string]int{}
	depth := 0
	for _, n := range order {
		l := 0
		for _, d := range needs(n) {
			l = max(l, level[d]+1)
		}
		level[n] = l
		depth = max(depth, l+1)
	}
	waves := make([][]string, depth)
	for _, n := range names {
		waves[level[n]] = append(waves[level[n]], n)
	}
	return waves, nil
}
