package term

import (
	"reflect"
	"testing"
)

func TestWarningMessagesHasWarnings(t *testing.T) {
	tests := []struct {
		name string
		w    *WarningMessages
		want bool
	}{
		{
			name: "nil",
			w:    nil,
			want: false,
		},
		{
			name: "empty",
			w:    &WarningMessages{},
			want: false,
		},
		{
			name: "with warnings",
			w:    &WarningMessages{"warning one"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.HasWarnings(); got != tt.want {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWarningMessagesAddWarning(t *testing.T) {
	var w WarningMessages

	w.AddWarning("warning one")
	w.AddWarning("warning two")

	want := WarningMessages{"warning one", "warning two"}

	if !reflect.DeepEqual(w, want) {
		t.Errorf("AddWarning() = %#v, want %#v", w, want)
	}
}

func TestWarningMessagesMergeWarnings(t *testing.T) {
	tests := []struct {
		name  string
		w     WarningMessages
		other WarningMessages
		want  WarningMessages
	}{
		{
			name:  "merge into empty",
			w:     WarningMessages{},
			other: WarningMessages{"one", "two"},
			want:  WarningMessages{"one", "two"},
		},
		{
			name:  "merge into existing",
			w:     WarningMessages{"one"},
			other: WarningMessages{"two", "three"},
			want:  WarningMessages{"one", "two", "three"},
		},
		{
			name:  "merge empty",
			w:     WarningMessages{"one"},
			other: WarningMessages{},
			want:  WarningMessages{"one"},
		},
		{
			name:  "merge nil",
			w:     WarningMessages{"one"},
			other: nil,
			want:  WarningMessages{"one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.w.MergeWarnings(tt.other)

			if !reflect.DeepEqual(tt.w, tt.want) {
				t.Errorf("MergeWarnings() = %#v, want %#v", tt.w, tt.want)
			}
		})
	}
}

func TestWarningMessagesContains(t *testing.T) {
	tests := []struct {
		name string
		w    *WarningMessages
		text string
		want bool
	}{
		{
			name: "nil receiver",
			w:    nil,
			text: "warning",
			want: false,
		},
		{
			name: "empty",
			w:    &WarningMessages{},
			text: "warning",
			want: false,
		},
		{
			name: "matching substring",
			w:    &WarningMessages{"something went wrong"},
			text: "went wrong",
			want: true,
		},
		{
			name: "no match",
			w:    &WarningMessages{"something went wrong"},
			text: "not found",
			want: false,
		},
		{
			name: "empty substring",
			w:    &WarningMessages{"warning"},
			text: "",
			want: true,
		},
		{
			name: "empty message",
			w:    &WarningMessages{""},
			text: "",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.Contains(tt.text); got != tt.want {
				t.Errorf("Contains(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestWarningMessagesString(t *testing.T) {
	tests := []struct {
		name string
		w    *WarningMessages
		want string
	}{
		{
			name: "nil",
			w:    nil,
			want: "",
		},
		{
			name: "empty",
			w:    &WarningMessages{},
			want: "",
		},
		{
			name: "single warning",
			w:    &WarningMessages{"warning one"},
			want: "warning one",
		},
		{
			name: "multiple warnings",
			w:    &WarningMessages{"warning one", "warning two"},
			want: "warning one,warning two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoMessagesHasInfo(t *testing.T) {
	tests := []struct {
		name string
		w    *InfoMessages
		want bool
	}{
		{
			name: "nil",
			w:    nil,
			want: false,
		},
		{
			name: "empty",
			w:    &InfoMessages{},
			want: false,
		},
		{
			name: "with info",
			w:    &InfoMessages{"information"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.HasInfo(); got != tt.want {
				t.Errorf("HasInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInfoMessagesAddInfo(t *testing.T) {
	var w InfoMessages

	w.AddInfo("information one")
	w.AddInfo("information two")

	want := InfoMessages{"information one", "information two"}

	if !reflect.DeepEqual(w, want) {
		t.Errorf("AddInfo() = %#v, want %#v", w, want)
	}
}

func TestInfoMessagesMergeInfo(t *testing.T) {
	tests := []struct {
		name  string
		w     InfoMessages
		other InfoMessages
		want  InfoMessages
	}{
		{
			name:  "merge into empty",
			w:     InfoMessages{},
			other: InfoMessages{"one", "two"},
			want:  InfoMessages{"one", "two"},
		},
		{
			name:  "merge into existing",
			w:     InfoMessages{"one"},
			other: InfoMessages{"two", "three"},
			want:  InfoMessages{"one", "two", "three"},
		},
		{
			name:  "merge empty",
			w:     InfoMessages{"one"},
			other: InfoMessages{},
			want:  InfoMessages{"one"},
		},
		{
			name:  "merge nil",
			w:     InfoMessages{"one"},
			other: nil,
			want:  InfoMessages{"one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.w.MergeInfo(tt.other)

			if !reflect.DeepEqual(tt.w, tt.want) {
				t.Errorf("MergeInfo() = %#v, want %#v", tt.w, tt.want)
			}
		})
	}
}

func TestInfoMessagesContains(t *testing.T) {
	tests := []struct {
		name string
		w    *InfoMessages
		text string
		want bool
	}{
		{
			name: "nil receiver",
			w:    nil,
			text: "information",
			want: false,
		},
		{
			name: "empty",
			w:    &InfoMessages{},
			text: "information",
			want: false,
		},
		{
			name: "matching substring",
			w:    &InfoMessages{"operation completed successfully"},
			text: "completed",
			want: true,
		},
		{
			name: "no match",
			w:    &InfoMessages{"operation completed successfully"},
			text: "failed",
			want: false,
		},
		{
			name: "empty substring",
			w:    &InfoMessages{"information"},
			text: "",
			want: true,
		},
		{
			name: "empty message",
			w:    &InfoMessages{""},
			text: "",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.Contains(tt.text); got != tt.want {
				t.Errorf("Contains(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestInfoMessagesString(t *testing.T) {
	tests := []struct {
		name string
		w    *InfoMessages
		want string
	}{
		{
			name: "nil",
			w:    nil,
			want: "",
		},
		{
			name: "empty",
			w:    &InfoMessages{},
			want: "",
		},
		{
			name: "single info",
			w:    &InfoMessages{"information one"},
			want: "information one",
		},
		{
			name: "multiple info",
			w:    &InfoMessages{"information one", "information two"},
			want: "information one,information two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.w.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
