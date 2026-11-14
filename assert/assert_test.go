package assert

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssertions_Check(t *testing.T) {
	yes := true
	cases := []struct {
		name   string
		output string
		a      Assertions
		pass   bool
	}{
		{"contains", "hello world", Assertions{TextAssertion: TextAssertion{Contains: "world"}}, true},
		{"contains missing", "hello world", Assertions{TextAssertion: TextAssertion{Contains: "missing"}}, false},
		{"notContains", "hello world", Assertions{TextAssertion: TextAssertion{NotContains: "missing"}}, true},
		{"notContains present", "hello world", Assertions{TextAssertion: TextAssertion{NotContains: "world"}}, false},
		{"equals trims", "  exact  \n", Assertions{TextAssertion: TextAssertion{Equals: "exact"}}, true},
		{"notEquals", "actual", Assertions{TextAssertion: TextAssertion{NotEquals: "expected"}}, true},
		{"exists on empty", "", Assertions{TextAssertion: TextAssertion{Exists: &yes}}, false},
		{"notExists on output", "something", Assertions{TextAssertion: TextAssertion{NotExists: &yes}}, false},
		{"in", "b", Assertions{TextAssertion: TextAssertion{In: "a,b,c"}}, true},
		{"in missing", "z", Assertions{TextAssertion: TextAssertion{In: "a,b,c"}}, false},
		{"notIn", "z", Assertions{TextAssertion: TextAssertion{NotIn: "a,b,c"}}, true},
		{"notIn present", "b", Assertions{TextAssertion: TextAssertion{NotIn: "a,b,c"}}, false},
		{"greaterThan is strict", "5", Assertions{TextAssertion: TextAssertion{GreaterThan: "5"}}, false},
		{"greaterThan", "6", Assertions{TextAssertion: TextAssertion{GreaterThan: "5"}}, true},
		{"lessThan is strict", "5", Assertions{TextAssertion: TextAssertion{LessThan: "5"}}, false},
		{"lessThan", "4", Assertions{TextAssertion: TextAssertion{LessThan: "5"}}, true},
		{"not a number", "not-a-number", Assertions{TextAssertion: TextAssertion{GreaterThan: "5"}}, false},
		{"min is inclusive", "5", Assertions{TextAssertion: TextAssertion{Min: "5"}}, true},
		{"min", "4", Assertions{TextAssertion: TextAssertion{Min: "5"}}, false},
		{"max is inclusive", "5", Assertions{TextAssertion: TextAssertion{Max: "5"}}, true},
		{"max", "6", Assertions{TextAssertion: TextAssertion{Max: "5"}}, false},
		{"between inclusive", "10", Assertions{TextAssertion: TextAssertion{Between: "1,10"}}, true},
		{"between outside", "11", Assertions{TextAssertion: TextAssertion{Between: "1,10"}}, false},
		{"notBetween outside", "11", Assertions{TextAssertion: TextAssertion{NotBetween: "1,10"}}, true},
		{"notBetween inside", "5", Assertions{TextAssertion: TextAssertion{NotBetween: "1,10"}}, false},
		{"regex", "app-prod-01", Assertions{TextAssertion: TextAssertion{Regex: `^app-\w+-\d+$`}}, true},
		{"regex no match", "APP", Assertions{TextAssertion: TextAssertion{Regex: `^app-\w+-\d+$`}}, false},
		{"prefix", "app-prod", Assertions{TextAssertion: TextAssertion{Prefix: "app-"}}, true},
		{"all must hold", "5", Assertions{TextAssertion: TextAssertion{Contains: "5", GreaterThan: "10"}}, false},
		{"none set", "anything", Assertions{}, true},
	}
	for _, c := range cases {
		err := c.a.Check(c.output)
		if c.pass {
			assert.NoError(t, err, c.name)
		} else {
			assert.Error(t, err, c.name)
		}
	}
}

func TestAssertions_FailureNamesTheAssertion(t *testing.T) {
	err := Assertions{TextAssertion: TextAssertion{Min: "10"}}.Check("3")
	assert.EqualError(t, err, `output "3" fails min "10"`)
}

func TestAssertionNames_FromTheTable(t *testing.T) {
	names := AssertionNames()
	assert.Equal(t, []string{"exists", "notExists"}, names[:2])
	assert.Contains(t, names, "min")
	assert.NotContains(t, names, "unique")
	assert.NotContains(t, names, "typeOf")
	assert.True(t, Assertions{}.Empty())
}

// One step checks its output inline, then text, then json: all must hold, and
// a failure says which form and entry failed.
func TestAssertions_TextAndJSON(t *testing.T) {
	out := `{"status":"ok","count":2}`
	all := Assertions{
		TextAssertion: TextAssertion{Contains: `"status":"ok"`},
		Text:          TextAssertions{{NotContains: "error"}, {Regex: `"count":[0-9]+`}},
		JSON:          JSONAssertions{{Path: ".count", TextAssertion: TextAssertion{GreaterThan: "1"}}},
	}
	assert.NoError(t, all.Check(out))
	assert.True(t, all.HasText())
	assert.True(t, all.HasJSON())

	for name, tc := range map[string]struct {
		a    Assertions
		want string
	}{
		"inline":   {Assertions{TextAssertion: TextAssertion{Contains: "missing"}}, `fails contains "missing"`},
		"text":     {Assertions{Text: TextAssertions{{Contains: "status"}, {Contains: "missing"}}}, "text[1]:"},
		"json":     {Assertions{JSON: JSONAssertions{{Path: ".count", TextAssertion: TextAssertion{Equals: "3"}}}}, "json .count:"},
		"not json": {Assertions{JSON: JSONAssertions{{Path: ".x"}}}, "not valid JSON"},
	} {
		err := tc.a.Check(map[bool]string{true: "plain text", false: out}[name == "not json"])
		if assert.Error(t, err, name) {
			assert.Contains(t, err.Error(), tc.want, name)
		}
	}

	inline := Assertions{TextAssertion: TextAssertion{Contains: "plain"}}
	assert.NoError(t, inline.Check("plain text"), "an inline-only step never parses its output as JSON")
	assert.False(t, inline.HasText())
	assert.False(t, inline.HasJSON())
}

// A flag set false means its opposite: exists: false is notExists: true.
func TestTextAssertion_FalseFlags(t *testing.T) {
	no, yes := false, true
	for _, tc := range []struct {
		name   string
		a      TextAssertion
		output string
		pass   bool
	}{
		{"exists false, empty", TextAssertion{Exists: &no}, "", true},
		{"exists false, output", TextAssertion{Exists: &no}, "x", false},
		{"notExists false, output", TextAssertion{NotExists: &no}, "x", true},
		{"notExists false, empty", TextAssertion{NotExists: &no}, "", false},
		{"exists true, output", TextAssertion{Exists: &yes}, "x", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.a.empty() {
				t.Fatal("a false flag is an assertion, not nothing")
			}
			if err := tc.a.check(tc.output); (err == nil) != tc.pass {
				t.Fatalf("check(%q) = %v, want pass %v", tc.output, err, tc.pass)
			}
		})
	}
	if !(TextAssertion{Exists: &no}).Absent() || !(TextAssertion{NotExists: &yes}).Absent() || (TextAssertion{Exists: &yes}).Absent() {
		t.Fatal("Absent: exists false and notExists true ask for no output")
	}
}

// expr: must come out true, CEL (${ }) or a template ({{ }}), on the value
// asserted on (a JSON number a number) and under json: the whole document
// as object; a bare expression is an error.
func TestExpr(t *testing.T) {
	ok := func(a Assertions, out string) {
		t.Helper()
		if err := a.Check(out); err != nil {
			t.Errorf("Check(%q): %v", out, err)
		}
	}
	fail := func(a Assertions, out, want string) {
		t.Helper()
		err := a.Check(out)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Check(%q) = %v, want it to contain %q", out, err, want)
		}
	}
	ok(Assertions{TextAssertion: TextAssertion{Expr: "${ value >= 2 && value <= 5 }"}}, "3")
	ok(Assertions{TextAssertion: TextAssertion{Expr: `${ value.startsWith("shop-api:") }`}}, "shop-api:dev")
	ok(Assertions{TextAssertion: TextAssertion{Expr: `{{ hasPrefix .value "shop-api:" }}`}}, "shop-api:dev")
	fail(Assertions{TextAssertion: TextAssertion{Expr: "${ value >= 2 }"}}, "1", "1 fails ${ value >= 2 }")
	fail(Assertions{TextAssertion: TextAssertion{Expr: "value >= 2"}}, "3", "write ${ … } (CEL) or {{ … }}")

	doc := `{"spec":{"replicas":2,"image":"shop-api:dev"},"status":{"readyReplicas":2}}`
	ok(Assertions{JSON: JSONAssertions{
		{Path: ".spec.replicas", TextAssertion: TextAssertion{Expr: "${ value == 2 }"}},
		{Path: ".status.readyReplicas", TextAssertion: TextAssertion{Expr: "${ value == object.spec.replicas }"}},
	}}, doc)
	fail(Assertions{JSON: JSONAssertions{{Path: ".spec.image", TextAssertion: TextAssertion{Expr: `${ value.endsWith(":prod") }`}}}}, doc,
		`json .spec.image: "shop-api:dev" fails`)

	if err := CheckExpr("${ value > }"); err == nil {
		t.Error("CheckExpr accepted a syntax error")
	}
	if err := CheckExpr("${ value >= 2 }"); err != nil {
		t.Errorf("CheckExpr: %v", err)
	}
	if err := CheckExpr("value >= 2"); err == nil {
		t.Error("CheckExpr accepted a bare expression")
	}
}
