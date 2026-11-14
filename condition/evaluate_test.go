// Tests for Evaluate, EvaluateOne, NavigateDotPath, NavigateRawPath,
// ResolveOp (evaluate.go).
package condition_test

import (
	"errors"
	"testing"
	"time"

	"k8s.io/utils/ptr"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/duration"
	"github.com/stretchr/testify/assert"
)

// ── NavigateDotPath ───────────────────────────────────────────────────────────

func TestNavigateDotPath_EmptyPath(t *testing.T) {
	data := map[string]interface{}{"key": "value"}
	assert.Equal(t, "", condition.NavigateDotPath(data, ""))
}

func TestNavigateDotPath_TopLevel(t *testing.T) {
	data := map[string]interface{}{"phase": "Running"}
	assert.Equal(t, "Running", condition.NavigateDotPath(data, "phase"))
}

func TestNavigateDotPath_Nested(t *testing.T) {
	data := map[string]interface{}{
		"status": map[string]interface{}{"phase": "Ready"},
	}
	assert.Equal(t, "Ready", condition.NavigateDotPath(data, "status.phase"))
}

func TestNavigateDotPath_DeepNested(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": "deep",
			},
		},
	}
	assert.Equal(t, "deep", condition.NavigateDotPath(data, "a.b.c"))
}

func TestNavigateDotPath_MissingKey(t *testing.T) {
	data := map[string]interface{}{"phase": "Running"}
	assert.Equal(t, "", condition.NavigateDotPath(data, "status.phase"))
}

func TestNavigateDotPath_NonMapIntermediate(t *testing.T) {
	data := map[string]interface{}{"status": "running"}
	assert.Equal(t, "", condition.NavigateDotPath(data, "status.phase"))
}

func TestNavigateDotPath_IntValue(t *testing.T) {
	data := map[string]interface{}{"replicas": 3}
	assert.Equal(t, "3", condition.NavigateDotPath(data, "replicas"))
}

func TestNavigateDotPath_NilValue(t *testing.T) {
	data := map[string]interface{}{"key": nil}
	assert.Equal(t, "", condition.NavigateDotPath(data, "key"))
}

// ── NavigateRawPath ───────────────────────────────────────────────────────────

func TestNavigateRawPath_EmptyPath(t *testing.T) {
	data := map[string]interface{}{"key": "val"}
	assert.Nil(t, condition.NavigateRawPath(data, ""))
}

func TestNavigateRawPath_TopLevelString(t *testing.T) {
	data := map[string]interface{}{"phase": "Running"}
	assert.Equal(t, "Running", condition.NavigateRawPath(data, "phase"))
}

func TestNavigateRawPath_TopLevelMap(t *testing.T) {
	inner := map[string]interface{}{"x": 1}
	data := map[string]interface{}{"spec": inner}
	assert.Equal(t, inner, condition.NavigateRawPath(data, "spec"))
}

func TestNavigateRawPath_TopLevelSlice(t *testing.T) {
	slice := []interface{}{"a", "b"}
	data := map[string]interface{}{"items": slice}
	assert.Equal(t, slice, condition.NavigateRawPath(data, "items"))
}

func TestNavigateRawPath_MissingKey(t *testing.T) {
	data := map[string]interface{}{}
	assert.Nil(t, condition.NavigateRawPath(data, "missing"))
}

func TestNavigateRawPath_BoolValue(t *testing.T) {
	data := map[string]interface{}{"enabled": true}
	assert.Equal(t, true, condition.NavigateRawPath(data, "enabled"))
}

// ── ResolveOp ────────────────────────────────────────────────────────

func TestResolveOp_Equals(t *testing.T) {
	c := condition.Condition{Equals: "Ready"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpEquals, op)
	assert.Equal(t, "Ready", val)
}

func TestResolveOp_NotEquals(t *testing.T) {
	c := condition.Condition{NotEquals: "Failed"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpNotEquals, op)
	assert.Equal(t, "Failed", val)
}

func TestResolveOp_Prefix(t *testing.T) {
	c := condition.Condition{Prefix: "prod-"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpPrefix, op)
	assert.Equal(t, "prod-", val)
}

func TestResolveOp_Suffix(t *testing.T) {
	c := condition.Condition{Suffix: "-v2"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpSuffix, op)
	assert.Equal(t, "-v2", val)
}

func TestResolveOp_Contains(t *testing.T) {
	c := condition.Condition{Contains: "error"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpContains, op)
	assert.Equal(t, "error", val)
}

func TestResolveOp_GreaterThan(t *testing.T) {
	c := condition.Condition{GreaterThan: "10"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpGt, op)
	assert.Equal(t, "10", val)
}

func TestResolveOp_LessThan(t *testing.T) {
	c := condition.Condition{LessThan: "5"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpLt, op)
	assert.Equal(t, "5", val)
}

func TestResolveOp_ExplicitOperator(t *testing.T) {
	c := condition.Condition{In: "a,b,c"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpIn, op)
	assert.Equal(t, "a,b,c", val)
}

func TestResolveOp_Default(t *testing.T) {
	c := condition.Condition{Field: "status.phase"}
	op, val := condition.ResolveOp(c)
	assert.Equal(t, condition.OpExists, op)
	assert.Equal(t, "", val)
}

// ── EvaluateOne — field operators ────────────────────────────────────────

func data(kv ...interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(kv)/2)
	for i := 0; i < len(kv)-1; i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestEvaluateOne_Equals_Match(t *testing.T) {
	d := data("phase", "Running")
	c := condition.Condition{Field: "phase", Equals: "Running"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Equals_NoMatch(t *testing.T) {
	d := data("phase", "Pending")
	c := condition.Condition{Field: "phase", Equals: "Running"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotEquals(t *testing.T) {
	d := data("phase", "Pending")
	c := condition.Condition{Field: "phase", NotEquals: "Running"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Contains(t *testing.T) {
	d := data("message", "connection refused")
	c := condition.Condition{Field: "message", Contains: "refused"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Contains_NoMatch(t *testing.T) {
	d := data("message", "all good")
	c := condition.Condition{Field: "message", Contains: "error"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Prefix(t *testing.T) {
	d := data("name", "prod-app")
	c := condition.Condition{Field: "name", Prefix: "prod-"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Prefix_NoMatch(t *testing.T) {
	d := data("name", "dev-app")
	c := condition.Condition{Field: "name", Prefix: "prod-"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Suffix(t *testing.T) {
	d := data("name", "app-v2")
	c := condition.Condition{Field: "name", Suffix: "-v2"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Exists_Present(t *testing.T) {
	d := data("phase", "Running")
	c := condition.Condition{Field: "phase", Exists: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Exists_Missing(t *testing.T) {
	d := map[string]interface{}{}
	c := condition.Condition{Field: "phase", Exists: ptr.To(true)}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotExists_Missing(t *testing.T) {
	d := map[string]interface{}{}
	c := condition.Condition{Field: "phase", NotExists: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotExists_Present(t *testing.T) {
	d := data("phase", "Running")
	c := condition.Condition{Field: "phase", NotExists: ptr.To(true)}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Gt_Pass(t *testing.T) {
	d := data("replicas", "5")
	c := condition.Condition{Field: "replicas", GreaterThan: "3"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Gt_Fail(t *testing.T) {
	d := data("replicas", "2")
	c := condition.Condition{Field: "replicas", GreaterThan: "3"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Gt_AbsentFieldTreatedAsZero(t *testing.T) {
	d := map[string]interface{}{}
	c := condition.Condition{Field: "count", GreaterThan: "0"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Lt_Pass(t *testing.T) {
	d := data("cpu", "50")
	c := condition.Condition{Field: "cpu", LessThan: "80"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_In_Match(t *testing.T) {
	d := data("env", "prod")
	c := condition.Condition{Field: "env", In: "dev,staging,prod"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_In_NoMatch(t *testing.T) {
	d := data("env", "test")
	c := condition.Condition{Field: "env", In: "dev,staging,prod"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotIn_Match(t *testing.T) {
	d := data("env", "canary")
	c := condition.Condition{Field: "env", NotIn: "dev,staging,prod"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotIn_NoMatch(t *testing.T) {
	d := data("env", "prod")
	c := condition.Condition{Field: "env", NotIn: "dev,staging,prod"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Gte_EqualBound(t *testing.T) {
	d := data("replicas", "3")
	c := condition.Condition{Field: "replicas", Min: "3"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Gte_BelowBound(t *testing.T) {
	d := data("replicas", "2")
	c := condition.Condition{Field: "replicas", Min: "3"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Lte_EqualBound(t *testing.T) {
	d := data("cpu", "80")
	c := condition.Condition{Field: "cpu", Max: "80"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Lte_AboveBound(t *testing.T) {
	d := data("cpu", "81")
	c := condition.Condition{Field: "cpu", Max: "80"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestResolveOp_Min(t *testing.T) {
	op, val := condition.ResolveOp(condition.Condition{Min: "1"})
	assert.Equal(t, condition.OpGte, op)
	assert.Equal(t, "1", val)
}

func TestResolveOp_Max(t *testing.T) {
	op, val := condition.ResolveOp(condition.Condition{Max: "10"})
	assert.Equal(t, condition.OpLte, op)
	assert.Equal(t, "10", val)
}

func TestEvaluateOne_Min_EqualBound(t *testing.T) {
	d := data("replicas", "1")
	c := condition.Condition{Field: "replicas", Min: "1"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Min_BelowBound(t *testing.T) {
	d := data("replicas", "0")
	c := condition.Condition{Field: "replicas", Min: "1"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Max_EqualBound(t *testing.T) {
	d := data("cpu", "80")
	c := condition.Condition{Field: "cpu", Max: "80"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Max_AboveBound(t *testing.T) {
	d := data("cpu", "81")
	c := condition.Condition{Field: "cpu", Max: "80"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Between_Inside(t *testing.T) {
	d := data("replicas", "5")
	c := condition.Condition{Field: "replicas", Between: "1,10"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Between_AtBounds(t *testing.T) {
	c := condition.Condition{Field: "replicas", Between: "1,10"}
	assert.True(t, condition.EvaluateOne(data("replicas", "1"), c, nil))
	assert.True(t, condition.EvaluateOne(data("replicas", "10"), c, nil))
}

func TestEvaluateOne_Between_Outside(t *testing.T) {
	d := data("replicas", "11")
	c := condition.Condition{Field: "replicas", Between: "1,10"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Between_MalformedRange(t *testing.T) {
	d := data("replicas", "5")
	c := condition.Condition{Field: "replicas", Between: "not,numbers"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotBetween_Outside(t *testing.T) {
	d := data("replicas", "11")
	c := condition.Condition{Field: "replicas", NotBetween: "1,10"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotBetween_Inside(t *testing.T) {
	d := data("replicas", "5")
	c := condition.Condition{Field: "replicas", NotBetween: "1,10"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotContains_Match(t *testing.T) {
	d := data("image", "myorg/app:latest")
	c := condition.Condition{Field: "image", NotContains: "docker.io"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_NotContains_NoMatch(t *testing.T) {
	d := data("image", "docker.io/app:latest")
	c := condition.Condition{Field: "image", NotContains: "docker.io"}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Regex_Match(t *testing.T) {
	d := data("name", "app-prod-01")
	c := condition.Condition{Field: "name", Regex: `^app-\w+-\d+$`}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Regex_NoMatch(t *testing.T) {
	d := data("name", "APP")
	c := condition.Condition{Field: "name", Regex: `^app-\w+-\d+$`}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Regex_InvalidPattern(t *testing.T) {
	d := data("name", "app")
	c := condition.Condition{Field: "name", Regex: `(unclosed`}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_Map(t *testing.T) {
	d := map[string]interface{}{
		"spec": map[string]interface{}{"key": "val"},
	}
	c := condition.Condition{Field: "spec", TypeOf: "map"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_List(t *testing.T) {
	d := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}
	c := condition.Condition{Field: "items", TypeOf: "slice"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_String(t *testing.T) {
	d := data("phase", "Running")
	c := condition.Condition{Field: "phase", TypeOf: "string"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_Bool(t *testing.T) {
	d := map[string]interface{}{"enabled": true}
	c := condition.Condition{Field: "enabled", TypeOf: "bool"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_Number(t *testing.T) {
	d := map[string]interface{}{"replicas": 3}
	c := condition.Condition{Field: "replicas", TypeOf: "number"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_Null(t *testing.T) {
	d := map[string]interface{}{"field": nil}
	c := condition.Condition{Field: "field", TypeOf: "null"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_TypeOf_Explicit(t *testing.T) {
	d := data("phase", "Running")
	c := condition.Condition{Field: "phase", TypeOf: "string"}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Unique_AlwaysTrue(t *testing.T) {
	d := data("name", "foo")
	c := condition.Condition{Field: "name", Unique: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

// ── Evaluate — allOf / or ──────────────────────────────────────────────

func TestEvaluate_EmptyBothPasses(t *testing.T) {
	assert.True(t, condition.Evaluate(nil, nil, nil, nil))
}

func TestEvaluate_AllOfAllPass(t *testing.T) {
	d := data("phase", "Running", "env", "prod")
	allOf := condition.Conditions{
		{Field: "phase", Equals: "Running"},
		{Field: "env", Equals: "prod"},
	}
	assert.True(t, condition.Evaluate(d, allOf, nil, nil))
}

func TestEvaluate_AllOfOneFails(t *testing.T) {
	d := data("phase", "Pending", "env", "prod")
	allOf := condition.Conditions{
		{Field: "phase", Equals: "Running"},
		{Field: "env", Equals: "prod"},
	}
	assert.False(t, condition.Evaluate(d, allOf, nil, nil))
}

func TestEvaluate_OrOneMatches(t *testing.T) {
	d := data("phase", "Failed")
	or := condition.Conditions{
		{Field: "phase", Equals: "Failed"},
		{Field: "phase", Equals: "Succeeded"},
	}
	assert.True(t, condition.Evaluate(d, nil, or, nil))
}

func TestEvaluate_OrNoneMatch(t *testing.T) {
	d := data("phase", "Running")
	or := condition.Conditions{
		{Field: "phase", Equals: "Failed"},
		{Field: "phase", Equals: "Succeeded"},
	}
	assert.False(t, condition.Evaluate(d, nil, or, nil))
}

func TestEvaluate_BothMustPass(t *testing.T) {
	d := data("env", "prod", "phase", "Failed")
	allOf := condition.Conditions{{Field: "env", Equals: "prod"}}
	or := condition.Conditions{
		{Field: "phase", Equals: "Failed"},
		{Field: "phase", Equals: "Succeeded"},
	}
	assert.True(t, condition.Evaluate(d, allOf, or, nil))
}

func TestEvaluate_AllOfPassOrFails(t *testing.T) {
	d := data("env", "prod", "phase", "Running")
	allOf := condition.Conditions{{Field: "env", Equals: "prod"}}
	or := condition.Conditions{
		{Field: "phase", Equals: "Failed"},
		{Field: "phase", Equals: "Succeeded"},
	}
	assert.False(t, condition.Evaluate(d, allOf, or, nil))
}

// ── EvaluateOne — cron window injection via _cronWindows ─────────────────

func TestEvaluateOne_CronWindowInjected_True(t *testing.T) {
	d := map[string]interface{}{
		"_cronWindows": map[string]interface{}{
			"0 9 * * 1-5": "true",
		},
	}
	c := condition.Condition{Cron: "0 9 * * 1-5", Duration: duration.Duration{Duration: time.Hour}}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_CronWindowInjected_False(t *testing.T) {
	d := map[string]interface{}{
		"_cronWindows": map[string]interface{}{
			"0 9 * * 1-5": "false",
		},
	}
	c := condition.Condition{Cron: "0 9 * * 1-5", Duration: duration.Duration{Duration: time.Hour}}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

// ── EvaluateOne — unique with a checker under UniquenessCheckerKey ────────

type fakeUniqueChecker struct {
	unique bool
	err    error
}

func (f *fakeUniqueChecker) IsUnique(field, value, selfNamespace, selfName string) (bool, error) {
	return f.unique, f.err
}

func TestEvaluateOne_Unique_NoCheckerInjected_AlwaysPasses(t *testing.T) {
	d := data("domain", "a.example.com")
	c := condition.Condition{Field: "domain", Unique: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Unique_CheckerReportsUnique(t *testing.T) {
	d := data("domain", "a.example.com")
	d[condition.UniquenessCheckerKey] = &fakeUniqueChecker{unique: true}
	c := condition.Condition{Field: "domain", Unique: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Unique_CheckerReportsDuplicate(t *testing.T) {
	d := data("domain", "shared.example.com")
	d[condition.UniquenessCheckerKey] = &fakeUniqueChecker{unique: false}
	c := condition.Condition{Field: "domain", Unique: ptr.To(true)}
	assert.False(t, condition.EvaluateOne(d, c, nil))
}

func TestEvaluateOne_Unique_CheckerErrors_FailsOpen(t *testing.T) {
	d := data("domain", "a.example.com")
	d[condition.UniquenessCheckerKey] = &fakeUniqueChecker{err: errors.New("list failed")}
	c := condition.Condition{Field: "domain", Unique: ptr.To(true)}
	assert.True(t, condition.EvaluateOne(d, c, nil))
}

// ── EvaluateOne — time window (via Condition.Time) ───────────────────────

func TestEvaluateOne_TimeWindow_WithinRange(t *testing.T) {
	// Use a window that is definitely open right now: 00:00–23:59
	c := condition.Condition{
		Time: &condition.TimeWindow{After: "00:00", Before: "23:59"},
	}
	assert.True(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_TimeWindow_OnlyAfter_Pass(t *testing.T) {
	// After 00:00 — always true
	c := condition.Condition{
		Time: &condition.TimeWindow{After: "00:00"},
	}
	assert.True(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_TimeWindow_OnlyBefore_Pass(t *testing.T) {
	// Before 23:59 — always true
	c := condition.Condition{
		Time: &condition.TimeWindow{Before: "23:59"},
	}
	assert.True(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_TimeWindow_InvalidAfter(t *testing.T) {
	c := condition.Condition{
		Time: &condition.TimeWindow{After: "not-a-time"},
	}
	assert.False(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_TimeWindow_InvalidBefore(t *testing.T) {
	c := condition.Condition{
		Time: &condition.TimeWindow{Before: "not-a-time"},
	}
	assert.False(t, condition.EvaluateOne(nil, c, nil))
}

// ── EvaluateOne — day of week (via Condition.DayOfWeek) ──────────────────

func TestEvaluateOne_DayOfWeek_InMatchesAllDays(t *testing.T) {
	// In: all 7 days — always true regardless of current weekday
	c := condition.Condition{
		DayOfWeek: &condition.DayOfWeek{
			In: []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		},
	}
	assert.True(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_DayOfWeek_NotInEmpty_ReturnsFalse(t *testing.T) {
	// Neither In nor NotIn set — evalDayOfWeek returns false
	c := condition.Condition{
		DayOfWeek: &condition.DayOfWeek{},
	}
	assert.False(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_DayOfWeek_NotInAllDays_ReturnsFalse(t *testing.T) {
	// Excluding all days — always false
	c := condition.Condition{
		DayOfWeek: &condition.DayOfWeek{
			NotIn: []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		},
	}
	assert.False(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_DayOfWeek_NotInNoMatch_ReturnsTrue(t *testing.T) {
	// NotIn: ["Funday"] — "Funday" never matches any real weekday, so always true
	c := condition.Condition{
		DayOfWeek: &condition.DayOfWeek{
			NotIn: []string{"Funday"},
		},
	}
	assert.True(t, condition.EvaluateOne(nil, c, nil))
}

func TestEvaluateOne_DayOfWeek_WeekdayTrue_OnWeekday(t *testing.T) {
	b := true
	c := condition.Condition{DayOfWeek: &condition.DayOfWeek{Weekday: &b}}
	// Monday is a weekday — must pass
	got := condition.DayOfWeekAt(c.DayOfWeek, mustParseTime("2026-07-13T12:00:00Z")) // Monday
	assert.True(t, got)
}

func TestEvaluateOne_DayOfWeek_WeekdayTrue_OnWeekend(t *testing.T) {
	b := true
	c := condition.Condition{DayOfWeek: &condition.DayOfWeek{Weekday: &b}}
	// Saturday is not a weekday — must fail
	got := condition.DayOfWeekAt(c.DayOfWeek, mustParseTime("2026-07-12T12:00:00Z")) // Saturday
	assert.False(t, got)
}

func TestEvaluateOne_DayOfWeek_WeekendTrue_OnWeekend(t *testing.T) {
	b := true
	c := condition.Condition{DayOfWeek: &condition.DayOfWeek{Weekend: &b}}
	got := condition.DayOfWeekAt(c.DayOfWeek, mustParseTime("2026-07-12T12:00:00Z")) // Saturday
	assert.True(t, got)
}

func TestEvaluateOne_DayOfWeek_WeekendTrue_OnWeekday(t *testing.T) {
	b := true
	c := condition.Condition{DayOfWeek: &condition.DayOfWeek{Weekend: &b}}
	got := condition.DayOfWeekAt(c.DayOfWeek, mustParseTime("2026-07-14T12:00:00Z")) // Monday
	assert.False(t, got)
}

func TestEvaluateOne_DayOfWeek_WeekdayAndWeekend_MutuallyExclusive(t *testing.T) {
	b := true
	// weekday: true on a weekday, weekend: true on a weekend — never both true simultaneously
	mon := mustParseTime("2026-07-13T12:00:00Z")
	sat := mustParseTime("2026-07-12T12:00:00Z")
	weekdayCond := &condition.DayOfWeek{Weekday: &b}
	weekendCond := &condition.DayOfWeek{Weekend: &b}
	assert.True(t, condition.DayOfWeekAt(weekdayCond, mon))
	assert.False(t, condition.DayOfWeekAt(weekendCond, mon))
	assert.False(t, condition.DayOfWeekAt(weekdayCond, sat))
	assert.True(t, condition.DayOfWeekAt(weekendCond, sat))
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ── EvaluateOne — cron (stateless fallback) ───────────────────────────────

func TestEvaluateOne_CronStateless_InvalidExpr(t *testing.T) {
	// Invalid cron expression → false
	c := condition.Condition{Cron: "not-a-cron", Duration: duration.Duration{Duration: time.Hour}}
	assert.False(t, condition.EvaluateOne(nil, c, nil))
}
