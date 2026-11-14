package condition

import (
	"fmt"
	"regexp"
	"time"

	"github.com/intentd/tools/structured"
)

// TemplateEvaluator resolves a template expression against the data being
// tested. It returns (result, true) on success and ("", false) on any error.
// nil turns template evaluation off.
type TemplateEvaluator func(tmpl string) (string, bool)

// cronWindowsKey is where a caller that tracks cron windows puts their state:
// data[cronWindowsKey][cronExpr] = "true" or "false". Without it a cron
// condition is evaluated from the schedule alone.
const cronWindowsKey = "_cronWindows"

// Evaluate reports whether every condition in allOf passes and, when or is
// set, at least one of or does. Empty lists pass.
func Evaluate(data map[string]interface{}, allOf, or Conditions, eval TemplateEvaluator) bool {
	for _, c := range allOf {
		if !EvaluateOne(data, c, eval) {
			return false
		}
	}
	if len(or) == 0 {
		return true
	}
	for _, c := range or {
		if EvaluateOne(data, c, eval) {
			return true
		}
	}
	return false
}

// EvaluateOne evaluates one condition against data, inverted by Negate.
//
// A Field or comparison value written as a template is resolved by eval
// first. Time conditions (time, dayOfWeek, cron) read the wall clock, not
// data.
func EvaluateOne(data map[string]interface{}, c Condition, eval TemplateEvaluator) bool {
	result := evaluateOne(data, c, eval)
	if c.Negate {
		return !result
	}
	return result
}

func evaluateOne(data map[string]interface{}, c Condition, eval TemplateEvaluator) bool {
	if c.Time != nil || c.DayOfWeek != nil || c.Cron != "" {
		return clockHolds(data, c, time.Now())
	}

	op, expected := ResolveOp(c)

	// A comparison value written as a template: equals: "{{ .spec.image }}".
	if structured.IsTemplate(expected) && eval != nil {
		if resolved, ok := eval(expected); ok {
			expected = resolved
		}
	}

	// A field written as a template is compared by its result; one that
	// fails to resolve fails, as a missing path does.
	if structured.IsTemplate(c.Field) && eval != nil {
		result, ok := eval(c.Field)
		if !ok {
			return false
		}
		return Apply(op, result, expected, data, c.Field)
	}

	return Apply(op, NavigateDotPath(data, c.Field), expected, data, c.Field)
}

// Apply compares fieldVal, the value at field in data, with expected by op.
// unique and typeOf read field from data; the rest compare the strings. A
// number that does not parse fails, except an empty field, which is 0.
func Apply(op Operator, fieldVal, expected string, data map[string]interface{}, field string) bool {
	switch op {
	case OpExists:
		return fieldVal != "" && fieldVal != "<no value>"
	case OpNotExists:
		return fieldVal == "" || fieldVal == "<no value>"
	case OpEquals:
		return fieldVal == expected
	case OpNotEquals:
		return fieldVal != expected
	case OpContains:
		return contains(fieldVal, expected)
	case OpNotContains:
		return !contains(fieldVal, expected)
	case OpRegex:
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(fieldVal)
	case OpPrefix:
		return hasPrefix(fieldVal, expected)
	case OpNotPrefix:
		return !hasPrefix(fieldVal, expected)
	case OpSuffix:
		return hasSuffix(fieldVal, expected)
	case OpNotSuffix:
		return !hasSuffix(fieldVal, expected)
	case OpGt, OpLt, OpGte, OpLte:
		fv, _ := parseFloat(fieldVal) // absent = 0
		ev, err := parseFloat(expected)
		if err != nil {
			return false
		}
		switch op {
		case OpGt:
			return fv > ev
		case OpLt:
			return fv < ev
		case OpGte:
			return fv >= ev
		}
		return fv <= ev
	case OpIn:
		return inList(fieldVal, expected)
	case OpNotIn:
		return !inList(fieldVal, expected)
	case OpBetween, OpNotBetween:
		lo, hi, ok := ParseBetween(expected)
		if !ok {
			return false
		}
		fv, err := parseFloat(fieldVal)
		if err != nil {
			return false
		}
		inside := fv >= lo && fv <= hi
		return inside == (op == OpBetween)
	case OpUnique:
		return IsUnique(data, field, fieldVal)
	case OpTypeOf:
		return TypeOf(NavigateRawPath(data, field)) == expected
	}
	return false
}

// NavigateRawPath returns the value at a dot-notation path in m, nil when
// any segment is missing.
func NavigateRawPath(m map[string]interface{}, path string) interface{} {
	if path == "" {
		return nil
	}
	var current interface{} = m
	for _, part := range structured.SplitField(path) {
		typed, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		if current, ok = typed[part]; !ok {
			return nil
		}
	}
	return current
}

// NavigateDotPath returns the value at a dot-notation path in m as text, ""
// when any segment is missing.
func NavigateDotPath(m map[string]interface{}, path string) string {
	switch v := NavigateRawPath(m, path).(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ParseBetween splits a "min,max" pair into two inclusive bounds. ok is
// false unless it is exactly two numbers.
func ParseBetween(expected string) (lo, hi float64, ok bool) {
	parts := splitComma(expected)
	if len(parts) != 2 {
		return 0, 0, false
	}
	var err error
	if lo, err = parseFloat(trimSpace(parts[0])); err != nil {
		return 0, 0, false
	}
	if hi, err = parseFloat(trimSpace(parts[1])); err != nil {
		return 0, 0, false
	}
	return lo, hi, true
}

func inList(value, list string) bool {
	for _, v := range splitComma(list) {
		if trimSpace(v) == value {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// trimSpace trims spaces and tabs only.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func hasSuffix(s, p string) bool { return len(s) >= len(p) && s[len(s)-len(p):] == p }

// parseFloat reads an empty string as 0: Kubernetes omits zero integers.
func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// clockHolds reports whether every clock field c sets holds at now: a
// condition with time and dayOfWeek is a time window on those days, not
// whichever is checked first.
func clockHolds(data map[string]interface{}, c Condition, now time.Time) bool {
	if c.Time != nil && !evalTimeWindow(c.Time, now) {
		return false
	}
	if c.DayOfWeek != nil && !DayOfWeekAt(c.DayOfWeek, now) {
		return false
	}
	if c.Cron != "" {
		if windows, ok := data[cronWindowsKey].(map[string]interface{}); ok {
			if v, ok := windows[c.Cron]; ok {
				return v == "true"
			}
		}
		return evalCronWindow(c.Cron, c.Duration.Duration, now)
	}
	return true
}
