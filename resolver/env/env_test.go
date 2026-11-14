package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolver(t *testing.T) {
	t.Setenv("FROM_PROCESS", "proc")
	t.Setenv("SHARED", "proc")
	r := New(map[string]string{"SHARED": "declared", "EMPTY": ""})

	assert.Equal(t, "declared/proc/${MISSING}", r.Expand("${SHARED}/${FROM_PROCESS}/${MISSING}"), "declared wins; unknown stays as written")
	assert.Equal(t, "$patch and $x", r.Expand("$patch and $x"), "a bare $NAME does not expand")
	assert.Equal(t, "declared/proc/declared", r.Expand("${{ env.SHARED }}/${{env.FROM_PROCESS}}/${SHARED}"), "${{ env.NAME }} expands like ${NAME}")
	assert.Equal(t, "${{ env.MISSING }}", r.Expand("${{ env.MISSING }}"), "unknown stays as written")
	v, ok := r.Lookup("EMPTY")
	assert.True(t, ok)
	assert.Equal(t, "", v, "declared empty is set")
	assert.ElementsMatch(t, []string{"SHARED=declared", "EMPTY="}, r.Pairs())
	v, ok = Resolver{}.Lookup("FROM_PROCESS")
	assert.True(t, ok)
	assert.Equal(t, "proc", v, "the zero Resolver reads the process environment")
}

func TestExpandAllAndUnknown(t *testing.T) {
	type spec struct {
		Name string
		Run  string            `env:"shell"`
		Env  map[string]string `env:"-"`
		Tags []string
	}
	r := New(map[string]string{"NS": "team"})
	in := spec{
		Name: "${NS}-web",
		Run:  "echo ${NS} $NS ${{ env.NS }} ${SHELL_ONLY} ${{ env.RUN_NOPE }}",
		Env:  map[string]string{"X": "${{ env.NS }}"},
		Tags: []string{"${{ env.NS }}", "${NOPE}", "${{ env.ALSO_NOPE }}"},
	}

	out := ExpandAll(r, in)
	assert.Equal(t, spec{
		Name: "team-web",
		Run:  "echo ${NS} $NS team ${SHELL_ONLY} ${{ env.RUN_NOPE }}", // the shell expands the rest
		Env:  map[string]string{"X": "${{ env.NS }}"},                 // env:"-" keeps its text
		Tags: []string{"team", "${NOPE}", "${{ env.ALSO_NOPE }}"},
	}, out)
	assert.Equal(t, "${NS}-web", in.Name, "the input is not modified")
	assert.Equal(t, []string{"ALSO_NOPE", "NOPE", "RUN_NOPE"}, Unknown(r, in), "a command's ${NAME} is its shell's")
}
