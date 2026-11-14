package condition

import (
	"fmt"
	"strings"
)

// Comparison is one way a field is compared (equals: x, min: "3",
// unique: true, …): its YAML name and the operator it runs.
type Comparison struct {
	Name string
	Op   Operator
}

// flags are the comparisons set with true: they take no value.
var flags = []Comparison{
	{"exists", OpExists},
	{"notExists", OpNotExists},
	{"unique", OpUnique},
}

// comparisons take a value. min and max are inclusive bounds; greaterThan and
// lessThan are strict.
var comparisons = []Comparison{
	{"equals", OpEquals},
	{"notEquals", OpNotEquals},
	{"prefix", OpPrefix},
	{"notPrefix", OpNotPrefix},
	{"suffix", OpSuffix},
	{"notSuffix", OpNotSuffix},
	{"contains", OpContains},
	{"notContains", OpNotContains},
	{"regex", OpRegex},
	{"greaterThan", OpGt},
	{"lessThan", OpLt},
	{"min", OpGte},
	{"max", OpLte},
	{"between", OpBetween},
	{"notBetween", OpNotBetween},
	{"in", OpIn},
	{"notIn", OpNotIn},
	{"typeOf", OpTypeOf},
}

// Comparisons returns every comparison, flags first.
func Comparisons() []Comparison {
	return append(append([]Comparison{}, flags...), comparisons...)
}

// ComparisonNames returns every comparison's name, flags first, for messages
// that list them.
func ComparisonNames() []string { return Names(Comparisons()) }

// Declared returns the comparisons set among flagVals and values (both keyed
// by name), in table order: a flag counts when true, a comparison when
// non-empty.
func Declared(flagVals map[string]*bool, values map[string]string) []Comparison {
	var out []Comparison
	for _, f := range flags {
		if b := flagVals[f.Name]; b != nil && *b {
			out = append(out, f)
		}
	}
	for _, c := range comparisons {
		if values[c.Name] != "" {
			out = append(out, c)
		}
	}
	return out
}

// Resolve returns the name, operator and value of the one comparison
// declared; exists when none is. CheckComparisons rejects more than one, so
// the first wins only for an unchecked declaration.
func Resolve(flagVals map[string]*bool, values map[string]string) (string, Operator, string) {
	if set := Declared(flagVals, values); len(set) > 0 {
		return set[0].Name, set[0].Op, values[set[0].Name]
	}
	return "exists", OpExists, ""
}

// Names returns the names of set.
func Names(set []Comparison) []string {
	out := make([]string, len(set))
	for i, c := range set {
		out[i] = c.Name
	}
	return out
}

// CheckComparisons reports a declaration that sets more than one
// comparison, or a typeOf that is not a type.
func CheckComparisons(set []Comparison, typeOf FieldType) error {
	if len(set) > 1 {
		return fmt.Errorf("declares %s: a condition compares one way. Split it into several conditions "+
			"under when (all must hold) or or (one must hold)", strings.Join(Names(set), " and "))
	}
	if typeOf != "" && !typeOf.IsValid() {
		return fmt.Errorf("typeOf %q is not a type: use one of %s", typeOf, fieldTypeNames())
	}
	return nil
}

// Declared returns the names of the comparisons the condition sets; a valid
// condition sets at most one.
func (c Condition) Declared() []string { return Names(Declared(c.FlagValues(), c.Values())) }

// CheckComparison reports a condition that declares more than one comparison,
// or a typeOf that is not a type.
func (c Condition) CheckComparison() error {
	return CheckComparisons(Declared(c.FlagValues(), c.Values()), c.TypeOf)
}

// FlagValues returns the condition's flag comparisons by name.
func (c Condition) FlagValues() map[string]*bool {
	return map[string]*bool{"exists": c.Exists, "notExists": c.NotExists, "unique": c.Unique}
}

// Values returns the condition's valued comparisons by name.
func (c Condition) Values() map[string]string {
	return map[string]string{
		"equals": c.Equals, "notEquals": c.NotEquals,
		"prefix": c.Prefix, "notPrefix": c.NotPrefix,
		"suffix": c.Suffix, "notSuffix": c.NotSuffix,
		"contains": c.Contains, "notContains": c.NotContains,
		"regex":       c.Regex,
		"greaterThan": c.GreaterThan, "lessThan": c.LessThan,
		"min": c.Min, "max": c.Max,
		"between": c.Between, "notBetween": c.NotBetween,
		"in": c.In, "notIn": c.NotIn,
		"typeOf": string(c.TypeOf),
	}
}

// ResolveOp returns the condition's operator and comparison value.
func ResolveOp(c Condition) (Operator, string) {
	_, op, value := Resolve(c.FlagValues(), c.Values())
	return op, value
}
