package condition

import (
	"slices"
	"strings"

	"github.com/intentd/tools/duration"
)

// Condition is one test against a data map. Field is a dot-notation path
// (spec.environment, metadata.name, metrics.queueDepth) and the comparison
// fields say what its value must be. A condition sets at most one comparison;
// with none it means exists.
type Condition struct {
	// Field — dot-notation path to the value tested, e.g. "spec.environment",
	// "metrics.queueDepth". A template expression is resolved first.
	Field string `yaml:"field" validate:"required" json:"field"`

	// Equals passes when the field's value is exactly this text.
	Equals string `yaml:"equals,omitempty" json:"equals,omitempty"`
	// NotEquals passes when the field's value is not this text.
	NotEquals string `yaml:"notEquals,omitempty" json:"notEquals,omitempty"`
	// Contains passes when the field's value contains this text.
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	// NotContains passes when the field's value does not contain this text.
	NotContains string `yaml:"notContains,omitempty" json:"notContains,omitempty"`
	// Regex passes when the field's value matches this RE2 pattern.
	Regex string `yaml:"regex,omitempty" json:"regex,omitempty"`
	// Prefix passes when the field's value starts with this text.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	// NotPrefix passes when the field's value does not start with this text.
	NotPrefix string `yaml:"notPrefix,omitempty" json:"notPrefix,omitempty"`
	// Suffix passes when the field's value ends with this text.
	Suffix string `yaml:"suffix,omitempty" json:"suffix,omitempty"`
	// NotSuffix passes when the field's value does not end with this text.
	NotSuffix string `yaml:"notSuffix,omitempty" json:"notSuffix,omitempty"`

	// Exists, when true, passes when the field is present and non-empty. A
	// condition with no comparison at all means exists.
	Exists *bool `yaml:"exists,omitempty" json:"exists,omitempty"`

	// NotExists, when true, passes when the field is absent or empty.
	NotExists *bool `yaml:"notExists,omitempty" json:"notExists,omitempty"`

	// Unique, when true, passes when no other object has the same value at
	// Field. Always passes where no UniquenessChecker is in the data.
	Unique *bool `yaml:"unique,omitempty" json:"unique,omitempty"`

	// TypeOf passes when the field's value has this type: map, slice,
	// string, number, bool or null.
	TypeOf FieldType `yaml:"typeOf,omitempty" json:"typeOf,omitempty"`

	// GreaterThan passes when the field's number is above this one (strict).
	GreaterThan string `yaml:"greaterThan,omitempty" json:"greaterThan,omitempty"`
	// LessThan passes when the field's number is below this one (strict).
	LessThan string `yaml:"lessThan,omitempty" json:"lessThan,omitempty"`

	// Min is an inclusive lower bound: the field's number is at least this.
	Min string `yaml:"min,omitempty" json:"min,omitempty"`

	// Max is an inclusive upper bound: the field's number is at most this.
	Max string `yaml:"max,omitempty" json:"max,omitempty"`

	// Between passes when the field's number is inside an inclusive "min,max" range.
	Between string `yaml:"between,omitempty" json:"between,omitempty"`

	// NotBetween passes when the field's number is outside an inclusive "min,max" range.
	NotBetween string `yaml:"notBetween,omitempty" json:"notBetween,omitempty"`

	// In passes when the field's value is one of a comma-separated list.
	In string `yaml:"in,omitempty" json:"in,omitempty"`

	// NotIn passes when the field's value is none of a comma-separated list.
	NotIn string `yaml:"notIn,omitempty" json:"notIn,omitempty"`

	// ── Time-based fields ──────────────────────────────────────────────────

	// Time — active when the current time is within the declared window.
	// After and Before are both optional; omit one for a half-open range.
	//   or:
	//     - time:
	//         after: "08:00"
	//         before: "20:00"
	Time *TimeWindow `yaml:"time,omitempty" json:"time,omitempty"`

	// DayOfWeek — active on the specified days of the week.
	//   or:
	//     - dayOfWeek:
	//         in: [Monday, Tuesday, Wednesday, Thursday, Friday]
	DayOfWeek *DayOfWeek `yaml:"dayOfWeek,omitempty" json:"dayOfWeek,omitempty"`

	// Cron — a standard cron expression (5-field) that defines when the
	// window opens. Duration defines how long the window stays open.
	// Without Duration, the window stays open until the next fire. Add Duration to close it sooner.
	Cron string `yaml:"cron,omitempty" json:"cron,omitempty"`

	// Duration — how long a cron-opened window remains active.
	Duration duration.Duration `yaml:"duration,omitempty" json:"duration,omitempty"`

	// Negate — when true, the result of this condition is inverted.
	// Applies to all condition types: field comparisons, time windows, dayOfWeek, cron.
	//
	//   when:
	//     - dayOfWeek:
	//         weekday: true
	//       negate: true   # passes on weekends
	Negate bool `yaml:"negate,omitempty" json:"negate,omitempty"`
}

type Conditions []Condition

// Expressions returns what a template or path in the condition can be
// written in: its field and the operand it compares with (one, resolved from
// the comparison fields by ResolveOp).
func (c Condition) Expressions() []string {
	if _, operand := ResolveOp(c); operand != "" {
		return []string{c.Field, operand}
	}
	return []string{c.Field}
}

// Operator is how a condition's field is compared to its value.
type Operator string

const (
	// OpEquals — field value exactly equals the condition value (string comparison)
	OpEquals Operator = "equals"

	// OpNotEquals — field value does not equal the condition value
	OpNotEquals Operator = "notEquals"

	// OpContains — field value contains the condition value as a substring
	OpContains Operator = "contains"

	// OpPrefix — field value starts with the condition value
	OpPrefix Operator = "prefix"

	// OpNotPrefix — field value does not start with the condition value
	OpNotPrefix Operator = "notPrefix"

	// OpSuffix — field value ends with the condition value
	OpSuffix Operator = "suffix"

	// OpNotSuffix — field value does not end with the condition value
	OpNotSuffix Operator = "notSuffix"

	// OpExists — the field is present and non-empty. Value is ignored.
	OpExists Operator = "exists"

	// OpNotExists — the field is absent or empty. Value is ignored.
	//   when:
	//     - field: status.phase
	//       notExists: true
	OpNotExists Operator = "notExists"

	// OpGt — field value is numerically greater than condition value
	OpGt Operator = "gt"

	// OpLt — field value is numerically less than condition value
	OpLt Operator = "lt"

	// OpGte — field value is numerically greater than or equal to condition value
	OpGte Operator = "gte"

	// OpLte — field value is numerically less than or equal to condition value
	OpLte Operator = "lte"

	// OpIn — field value is one of a comma-separated list. An empty entry
	// matches an empty field:
	//   when:
	//     - field: status.phase
	//       in: ",Pending"   # empty or "Pending"
	OpIn Operator = "in"

	// OpNotIn — field value is none of a comma-separated list.
	OpNotIn Operator = "notIn"

	// OpNotContains — field value does not contain the condition value as a substring
	OpNotContains Operator = "notContains"

	// OpRegex — field value matches the condition value as an RE2 regular
	// expression. An invalid pattern fails the condition rather than
	// erroring, same as a non-numeric gt/lt value.
	OpRegex Operator = "regex"

	// OpBetween — field value is numerically within an inclusive range.
	// Value is a comma-separated "min,max" pair.
	//   when:
	//     - field: spec.replicas
	//       between: "1,10"
	OpBetween Operator = "between"

	// OpNotBetween — field value is numerically outside an inclusive
	// range. Value is a comma-separated "min,max" pair, same as between.
	OpNotBetween Operator = "notBetween"

	// OpUnique — no other object has the field's value, as the
	// UniquenessChecker in the data reports. Passes without one.
	OpUnique Operator = "unique"

	// OpTypeOf — field value has the FieldType given (typeOf: map).
	OpTypeOf Operator = "typeOf"
)

// FieldType is a value's type as typeOf names it: the names TypeOf returns.
type FieldType string

const (
	FieldTypeMap    FieldType = "map"
	FieldTypeSlice  FieldType = "slice"
	FieldTypeString FieldType = "string"
	FieldTypeNumber FieldType = "number"
	FieldTypeBool   FieldType = "bool"
	FieldTypeNull   FieldType = "null"
)

// FieldTypes is every FieldType, for validation and messages.
var FieldTypes = []FieldType{FieldTypeMap, FieldTypeSlice, FieldTypeString, FieldTypeNumber, FieldTypeBool, FieldTypeNull}

// IsValid reports whether t is one of FieldTypes.
func (t FieldType) IsValid() bool { return slices.Contains(FieldTypes, t) }

func fieldTypeNames() string {
	out := make([]string, len(FieldTypes))
	for i, t := range FieldTypes {
		out[i] = string(t)
	}
	return strings.Join(out, ", ")
}

// TypeOf names v's type as decoded JSON or YAML holds it: map, slice,
// string, number, bool, null, or unknown.
func TypeOf(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case float64, float32, int, int32, int64, uint, uint32, uint64:
		return "number"
	case bool:
		return "bool"
	case map[string]interface{}:
		return "map"
	case []interface{}:
		return "slice"
	default:
		return "unknown"
	}
}
