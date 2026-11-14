package condition_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/condition"
)

func TestComparisons_NoneMeansExists(t *testing.T) {
	op, _ := condition.ResolveOp(condition.Condition{Field: "spec.x"})
	assert.Equal(t, condition.OpExists, op)
}

func TestComparisons_MoreThanOneRejected(t *testing.T) {
	c := condition.Condition{Field: "spec.x", Min: "1", Max: "3"}
	assert.Equal(t, []string{"min", "max"}, c.Declared())
	err := c.CheckComparison()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares min and max")
}

func TestComparisons_TypeOfMustBeAType(t *testing.T) {
	assert.NoError(t, condition.Condition{Field: "spec.x", TypeOf: "slice"}.CheckComparison())
	err := condition.Condition{Field: "spec.x", TypeOf: "list"}.CheckComparison()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "map, slice, string, number, bool, null")
}

func TestComparisonNames_FlagsThenValues(t *testing.T) {
	names := condition.ComparisonNames()
	assert.Equal(t, "exists", names[0])
	assert.Contains(t, names, "typeOf")
	assert.NotContains(t, names, "greaterThanOrEqual")
}

func TestResolve_FirstDeclaredWins(t *testing.T) {
	yes := true
	name, op, v := condition.Resolve(map[string]*bool{"unique": &yes}, map[string]string{"equals": "a"})
	assert.Equal(t, "unique", name)
	assert.Equal(t, condition.OpUnique, op)
	assert.Empty(t, v)
}

func TestApply_TextAndNumbers(t *testing.T) {
	data := map[string]interface{}{"n": 5.0}
	assert.True(t, condition.Apply(condition.OpGte, "5", "5", data, "n"))
	assert.False(t, condition.Apply(condition.OpGt, "5", "x", data, "n"))
	assert.True(t, condition.Apply(condition.OpNotBetween, "11", "1,10", data, "n"))
	assert.True(t, condition.Apply(condition.OpTypeOf, "5", "number", data, "n"))
	assert.True(t, condition.Apply(condition.OpIn, "", ",Pending", data, "missing"))
}
