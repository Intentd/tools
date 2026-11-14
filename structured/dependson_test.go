package structured

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependsOnMapUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    DependsOnMap
		wantErr bool
	}{
		{
			name: "format 1: list of names defaults to started",
			yaml: "dependsOn:\n  - database\n  - queue\n",
			want: DependsOnMap{
				"database": {Condition: "started"},
				"queue":    {Condition: "started"},
			},
		},
		{
			name: "format 2: map of name to condition",
			yaml: "dependsOn:\n  database: healthy\n  queue: started\n",
			want: DependsOnMap{
				"database": {Condition: "healthy"},
				"queue":    {Condition: "started"},
			},
		},
		{
			name: "format 3: map of name to struct",
			yaml: "dependsOn:\n  database:\n    condition: healthy\n  queue:\n    condition: started\n",
			want: DependsOnMap{
				"database": {Condition: "healthy"},
				"queue":    {Condition: "started"},
			},
		},
		{
			name: "format 3 with empty condition defaults to started",
			yaml: "dependsOn:\n  database:\n    condition: \"\"\n",
			want: DependsOnMap{
				"database": {Condition: "started"},
			},
		},
		{
			name: "mixed formats",
			yaml: "dependsOn:\n  database: healthy\n  queue:\n    condition: started\n",
			want: DependsOnMap{
				"database": {Condition: "healthy"},
				"queue":    {Condition: "started"},
			},
		},
		{
			name: "empty list",
			yaml: "dependsOn: []\n",
			want: DependsOnMap{},
		},
		{
			name: "empty map",
			yaml: "dependsOn: {}\n",
			want: DependsOnMap{},
		},
		{
			name:    "invalid: scalar",
			yaml:    "dependsOn: database\n",
			wantErr: true,
		},
		{
			name:    "invalid: number",
			yaml:    "dependsOn: 42\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var holder struct {
				DependsOn DependsOnMap `yaml:"dependsOn"`
			}
			err := yaml.Unmarshal([]byte(tc.yaml), &holder)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !EqualMaps(holder.DependsOn, tc.want) {
				t.Errorf("DependsOn = %v, want %v", holder.DependsOn, tc.want)
			}
		})
	}
}

func TestDependsOnConditionUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    DependsOnCondition
		wantErr bool
	}{
		{
			name: "scalar",
			yaml: "condition: healthy\n",
			want: DependsOnCondition{Condition: "healthy"},
		},
		{
			name: "map",
			yaml: "condition: started\n",
			want: DependsOnCondition{Condition: "started"},
		},
		{
			name:    "invalid: sequence",
			yaml:    "condition: [a, b]\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c DependsOnCondition
			err := yaml.Unmarshal([]byte(tc.yaml), &c)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.Condition != tc.want.Condition {
				t.Errorf("Condition = %q, want %q", c.Condition, tc.want.Condition)
			}
		})
	}
}

func TestDependsOnMapNames(t *testing.T) {
	m := DependsOnMap{
		"queue":    {Condition: "started"},
		"database": {Condition: "healthy"},
		"cache":    {Condition: "started"},
	}
	want := []string{"cache", "database", "queue"}
	got := m.Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDependsOnMapConditions(t *testing.T) {
	m := DependsOnMap{
		"database": {Condition: "healthy"},
		"queue":    {Condition: "started"},
	}

	t.Run("ConditionHealthy", func(t *testing.T) {
		if !m.ConditionHealthy("database") {
			t.Error("database should be healthy")
		}
		if m.ConditionHealthy("queue") {
			t.Error("queue is not healthy")
		}
		if m.ConditionHealthy("missing") {
			t.Error("missing dependency should not be healthy")
		}
	})

	t.Run("ConditionStarted", func(t *testing.T) {
		if !m.ConditionStarted("queue") {
			t.Error("queue should be started")
		}
		if m.ConditionStarted("database") {
			t.Error("database is not started")
		}
		if m.ConditionStarted("missing") {
			t.Error("missing dependency should not be started")
		}
	})
}

func TestEqualMaps(t *testing.T) {
	a := DependsOnMap{
		"database": {Condition: "healthy"},
		"queue":    {Condition: "started"},
	}

	tests := []struct {
		name string
		b    DependsOnMap
		want bool
	}{
		{
			name: "same",
			b: DependsOnMap{
				"database": {Condition: "healthy"},
				"queue":    {Condition: "started"},
			},
			want: true,
		},
		{
			name: "different condition",
			b: DependsOnMap{
				"database": {Condition: "started"},
				"queue":    {Condition: "started"},
			},
			want: false,
		},
		{
			name: "missing key",
			b: DependsOnMap{
				"database": {Condition: "healthy"},
			},
			want: false,
		},
		{
			name: "extra key",
			b: DependsOnMap{
				"database": {Condition: "healthy"},
				"queue":    {Condition: "started"},
				"cache":    {Condition: "started"},
			},
			want: false,
		},
		{
			name: "both empty",
			b:    DependsOnMap{},
			want: false, // a has two entries
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := EqualMaps(a, tc.b); got != tc.want {
				t.Errorf("EqualMaps = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("both empty", func(t *testing.T) {
		if !EqualMaps(DependsOnMap{}, DependsOnMap{}) {
			t.Error("two empty maps should be equal")
		}
	})
}

func TestDependsOnMapUnmarshalErrorNamesKey(t *testing.T) {
	var holder struct {
		DependsOn DependsOnMap `yaml:"dependsOn"`
	}
	err := yaml.Unmarshal([]byte("dependsOn:\n  database: [not, a, condition]\n"), &holder)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("error %q should name the key", err)
	}
}
