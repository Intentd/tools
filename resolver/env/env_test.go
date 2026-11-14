package env

import (
	"fmt"
	"reflect"
	"strings"
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
	assert.Equal(t, Vars{"SHARED": "declared", "EMPTY": ""}, r.Vars(), "only what was declared, never the process environment")
	assert.Empty(t, Resolver{}.Vars())
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

// A context resolves ${{ name.key }} wherever ${{ env.NAME }} does, commands
// included; an unknown key is reported, a context never added is left alone.
func TestContexts(t *testing.T) {
	base := New(map[string]string{"NS": "shop"})
	r := base.WithContext("ecosystem", map[string]string{"postgres.url": "postgresql://app:pw@postgres-rw.data:5432/app"})

	assert.Equal(t, "postgresql://app:pw@postgres-rw.data:5432/app in shop",
		r.Expand("${{ ecosystem.postgres.url }} in ${NS}"))
	assert.Equal(t, "${{ secrets.TOKEN }}", r.Expand("${{ secrets.TOKEN }}"), "a context never added stays as written")
	assert.Equal(t, "${{ ecosystem.postgres.uri }}", r.Expand("${{ ecosystem.postgres.uri }}"), "an unknown key stays as written")
	assert.Equal(t, "${{ ecosystem.postgres.url }}", base.Expand("${{ ecosystem.postgres.url }}"), "WithContext leaves the original alone")

	type cmd struct {
		Run string `env:"shell"`
		Out string
	}
	got := ExpandAll(r, cmd{Run: "psql ${{ ecosystem.postgres.url }} $HOME", Out: "${{ ecosystem.postgres.url }}"})
	assert.Equal(t, "psql postgresql://app:pw@postgres-rw.data:5432/app $HOME", got.Run, "a command gets contexts before its shell")
	assert.Equal(t, "postgresql://app:pw@postgres-rw.data:5432/app", got.Out)

	assert.Equal(t, []string{"ecosystem.postgres.uri", "ecosystem.redis.url"},
		UnknownContext(r, cmd{Run: "${{ ecosystem.postgres.uri }}", Out: "${{ ecosystem.redis.url }} ${{ secrets.X }} ${{ env.NS }}"}))
	assert.Equal(t, []string{"postgres.url"}, r.ContextKeys("ecosystem"))
	assert.Empty(t, UnknownContext(base, cmd{Run: "${{ ecosystem.postgres.url }}"}), "nothing added, nothing checked")
}

// A .env file as Compose reads it: comments, export, quotes; a line that
// isn't KEY=VALUE is an error naming it.
func TestParseDotEnv(t *testing.T) {
	got, err := ParseDotEnv([]byte("# db\nDB_URL=postgres://x\nexport TOKEN='a b'\n\nNAME=\"shop\"\nEMPTY=\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"DB_URL": "postgres://x", "TOKEN": "a b", "NAME": "shop", "EMPTY": ""}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ParseDotEnv = %v, want %v", got, want)
	}
	if _, err := ParseDotEnv([]byte("ok=1\nnot a pair\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("want an error on line 2, got %v", err)
	}
}

func TestVarsInherit(t *testing.T) {
	tests := []struct {
		name   string
		e      Vars
		parent Vars
		want   Vars
	}{
		{
			name:   "both empty",
			e:      Vars{},
			parent: Vars{},
			want:   Vars{},
		},
		{
			name:   "empty parent returns receiver",
			e:      Vars{"A": "1"},
			parent: Vars{},
			want:   Vars{"A": "1"},
		},
		{
			name:   "empty receiver inherits parent",
			e:      Vars{},
			parent: Vars{"A": "1"},
			want:   Vars{"A": "1"},
		},
		{
			name:   "merges both maps",
			e:      Vars{"A": "1"},
			parent: Vars{"B": "2"},
			want:   Vars{"A": "1", "B": "2"},
		},
		{
			name:   "receiver values override parent",
			e:      Vars{"A": "child"},
			parent: Vars{"A": "parent"},
			want:   Vars{"A": "child"},
		},
		{
			name:   "multiple overlapping and distinct keys",
			e:      Vars{"A": "child", "B": "child", "C": "3"},
			parent: Vars{"A": "parent", "B": "parent", "D": "4"},
			want:   Vars{"A": "child", "B": "child", "C": "3", "D": "4"},
		},
		{
			name:   "nil receiver",
			e:      nil,
			parent: Vars{"A": "1"},
			want:   Vars{"A": "1"},
		},
		{
			name:   "nil parent",
			e:      Vars{"A": "1"},
			parent: nil,
			want:   Vars{"A": "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.Inherit(tt.parent)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Inherit() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
