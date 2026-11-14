package structured

import (
	"fmt"
	"slices"
	"strings"
)

// ForEach declares the rows an entry is repeated for: written out (Items),
// or generated as GitHub Actions' strategy.matrix does (Matrix, with
// Exclude and Include). Each row is the item a repeated entry's templates
// read as .item, and as .<As> when As is set.
//
//	forEach:
//	  items: [{ref: main, port: 8080}, {ref: release/1.3, port: 8081}]
//
//	forEach:
//	  matrix: {ref: [main, release/1.3], dockerfile: [Dockerfile, Dockerfile.prod]}
//	  exclude: [{ref: release/1.3, dockerfile: Dockerfile.prod}]
//	  include: [{ref: main, dockerfile: Dockerfile.debug}]
type ForEach struct {
	// Items are the rows, as written: each a map ({{ .item.ref }}) or a
	// scalar ({{ .item }}).
	Items []any `yaml:"items,omitempty" json:"items,omitempty"`
	// Matrix generates a row for every combination of its values: keys in
	// name order, the first key varying slowest, values in their order.
	Matrix map[string][]any `yaml:"matrix,omitempty" json:"matrix,omitempty"`
	// Exclude removes the generated rows it matches: every key it gives
	// equal.
	Exclude []map[string]any `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	// Include adds rows as written, after the generated ones: each gives a
	// value for every matrix key.
	Include []map[string]any `yaml:"include,omitempty" json:"include,omitempty"`
	// As names the item besides item.
	As string `yaml:"as,omitempty" json:"as,omitempty"`
}

// Validate reports what keeps f from producing rows: items and matrix
// both or neither, a matrix key with no values, exclude or include
// without a matrix, an exclude key the matrix doesn't have, an include row
// missing a matrix key.
func (f ForEach) Validate() error {
	var problems []string
	switch {
	case len(f.Items) > 0 && len(f.Matrix) > 0:
		problems = append(problems, "set items or matrix, not both")
	case len(f.Items) == 0 && len(f.Matrix) == 0:
		problems = append(problems, "set items or matrix")
	}
	if len(f.Matrix) == 0 && (len(f.Exclude) > 0 || len(f.Include) > 0) {
		problems = append(problems, "exclude and include go with a matrix")
	}
	keys := SortedKeys(f.Matrix)
	for _, k := range keys {
		if len(f.Matrix[k]) == 0 {
			problems = append(problems, fmt.Sprintf("matrix.%s has no values", k))
		}
	}
	if len(f.Matrix) > 0 {
		for i, ex := range f.Exclude {
			for _, k := range SortedKeys(ex) {
				if _, ok := f.Matrix[k]; !ok {
					problems = append(problems, fmt.Sprintf("exclude[%d].%s is not a matrix key (%s)", i, k, strings.Join(keys, ", ")))
				}
			}
		}
		for i, in := range f.Include {
			for _, k := range keys {
				if _, ok := in[k]; !ok {
					problems = append(problems, fmt.Sprintf("include[%d] has no %s: an included row gives every matrix key", i, k))
				}
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("forEach: %s", strings.Join(problems, "; "))
	}
	return nil
}

// Rows returns the rows f declares: Items as written, or the matrix's
// combinations without the excluded ones, then the included.
func (f ForEach) Rows() ([]any, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if len(f.Items) > 0 {
		return slices.Clone(f.Items), nil
	}
	keys := SortedKeys(f.Matrix)
	combos := []map[string]any{{}}
	for _, k := range keys {
		var next []map[string]any
		for _, c := range combos {
			for _, v := range f.Matrix[k] {
				row := make(map[string]any, len(c)+1)
				for ck, cv := range c {
					row[ck] = cv
				}
				row[k] = v
				next = append(next, row)
			}
		}
		combos = next
	}
	var rows []any
	for _, c := range combos {
		if !slices.ContainsFunc(f.Exclude, func(ex map[string]any) bool { return matches(c, ex) }) {
			rows = append(rows, c)
		}
	}
	for _, in := range f.Include {
		rows = append(rows, in)
	}
	return rows, nil
}

// matches reports whether row has every key of sub with the same value.
func matches(row, sub map[string]any) bool {
	for k, v := range sub {
		if fmt.Sprint(row[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// ExpandForEach repeats each entry of srcs that has forEach once per row,
// in order; the others pass through. rows returns whether an entry has
// forEach, and its rows: an entry with forEach and no rows (an empty list
// it reads) produces nothing. expand returns the entry for one row, its
// templates resolved with that row as the item. Callers resolve with their
// own resolver: the rows are the same everywhere.
func ExpandForEach[T any](srcs []T, rows func(T) (bool, []any, error), expand func(src T, entry int, item any, row int) (T, error)) ([]T, error) {
	var out []T
	for i, src := range srcs {
		has, items, err := rows(src)
		if err != nil {
			return nil, err
		}
		if !has {
			out = append(out, src)
			continue
		}
		for r, item := range items {
			e, err := expand(src, i, item, r)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
	}
	return out, nil
}
