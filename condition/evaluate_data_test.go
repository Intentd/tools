package condition_test

import (
	"testing"

	"k8s.io/utils/ptr"

	"github.com/intentd/tools/condition"
)

func buildData(spec map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "demo.inrun.dev/v1alpha1",
		"kind":       "Website",
		"metadata": map[string]interface{}{
			"name":      "my-website",
			"namespace": "default",
		},
		"spec": spec,
	}
}

func evalConds(data map[string]interface{}, conds condition.Conditions) bool {
	return condition.Evaluate(data, conds, nil, nil)
}

func TestEvaluate_Empty(t *testing.T) {
	data := buildData(map[string]interface{}{})
	if !evalConds(data, nil) {
		t.Error("nil conditions should always return true")
	}
	if !evalConds(data, condition.Conditions{}) {
		t.Error("empty slice should always return true")
	}
}

func TestEvaluate_EqualsShorthand(t *testing.T) {
	data := buildData(map[string]interface{}{"environment": "production"})

	if !evalConds(data, condition.Conditions{{Field: "spec.environment", Equals: "production"}}) {
		t.Error("equals shorthand should pass when value matches")
	}
	if evalConds(data, condition.Conditions{{Field: "spec.environment", Equals: "staging"}}) {
		t.Error("equals shorthand should fail when value does not match")
	}
}

func TestEvaluate_AllMustPass(t *testing.T) {
	data := buildData(map[string]interface{}{
		"environment": "production",
		"enabled":     "true",
	})

	if !evalConds(data, condition.Conditions{
		{Field: "spec.environment", Equals: "production"},
		{Field: "spec.enabled", Equals: "true"},
	}) {
		t.Error("all conditions pass — should return true")
	}
	if evalConds(data, condition.Conditions{
		{Field: "spec.environment", Equals: "production"},
		{Field: "spec.enabled", Equals: "false"},
	}) {
		t.Error("one condition fails — should return false")
	}
}

func TestEvaluate_Operators(t *testing.T) {
	data := buildData(map[string]interface{}{
		"image":    "myorg/nginx:1.25",
		"replicas": int64(5),
		"logLevel": "info",
	})

	tests := []struct {
		name   string
		cond   condition.Condition
		expect bool
	}{
		{"prefix matches", condition.Condition{Field: "spec.image", Prefix: "myorg/"}, true},
		{"prefix no match", condition.Condition{Field: "spec.image", Prefix: "other/"}, false},
		{"suffix matches", condition.Condition{Field: "spec.image", Suffix: ":1.25"}, true},
		{"contains matches", condition.Condition{Field: "spec.image", Contains: "nginx"}, true},
		{"notEquals matches", condition.Condition{Field: "spec.logLevel", NotEquals: "debug"}, true},
		{"gt matches — 5 > 3", condition.Condition{Field: "spec.replicas", GreaterThan: "3"}, true},
		{"gt fails — 5 not > 10", condition.Condition{Field: "spec.replicas", GreaterThan: "10"}, false},
		{"lt matches — 5 < 10", condition.Condition{Field: "spec.replicas", LessThan: "10"}, true},
		{"exists — present field", condition.Condition{Field: "spec.image", Exists: ptr.To(true)}, true},
		{"exists — absent field", condition.Condition{Field: "spec.missing", Exists: ptr.To(true)}, false},
		{"notExists — absent field", condition.Condition{Field: "spec.missing", NotExists: ptr.To(true)}, true},
		{"notExists — present field", condition.Condition{Field: "spec.image", NotExists: ptr.To(true)}, false},
		{"in — value in list", condition.Condition{Field: "spec.logLevel", In: "debug,info,warn"}, true},
		{"in — value not in list", condition.Condition{Field: "spec.logLevel", In: "debug,warn"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if evalConds(data, condition.Conditions{tt.cond}) != tt.expect {
				t.Errorf("expected %v, got %v", tt.expect, !tt.expect)
			}
		})
	}
}

func TestEvaluate_NestedField(t *testing.T) {
	data := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				"tier": "premium",
			},
		},
		"spec": map[string]interface{}{
			"database": map[string]interface{}{
				"engine": "postgres",
			},
		},
	}

	tests := []struct {
		name   string
		field  string
		equals string
		expect bool
	}{
		{"nested metadata label", "metadata.labels.tier", "premium", true},
		{"nested spec field", "spec.database.engine", "postgres", true},
		{"nested field no match", "spec.database.engine", "mysql", false},
		{"nested field missing", "spec.database.version", "14", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := condition.Condition{Field: tt.field, Equals: tt.equals}
			if evalConds(data, condition.Conditions{cond}) != tt.expect {
				t.Errorf("expected %v, got %v", tt.expect, !tt.expect)
			}
		})
	}
}

func TestEvaluate_Or(t *testing.T) {
	data := buildData(map[string]interface{}{"phase": "Failed"})

	or := condition.Conditions{
		{Field: "spec.phase", Equals: "Failed"},
		{Field: "spec.phase", Equals: "Succeeded"},
	}
	if !condition.Evaluate(data, nil, or, nil) {
		t.Error("or should pass when at least one condition matches")
	}

	or = condition.Conditions{
		{Field: "spec.phase", Equals: "Pending"},
		{Field: "spec.phase", Equals: "Running"},
	}
	if condition.Evaluate(data, nil, or, nil) {
		t.Error("or should fail when no conditions match")
	}
}
