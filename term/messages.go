package term

import "strings"

// WarningMessages holds a collection of warning messages.
type WarningMessages []string

// HasWarnings reports whether any warnings exist.
func (w *WarningMessages) HasWarnings() bool {
	return w != nil && len(*w) > 0
}

// AddWarning appends a warning message.
func (w *WarningMessages) AddWarning(msg string) {
	*w = append(*w, msg)
}

// MergeWarnings appends warnings from another collection.
func (w *WarningMessages) MergeWarnings(other WarningMessages) {
	*w = append(*w, other...)
}

// Contains reports whether any warning contains the given substring.
func (w *WarningMessages) Contains(text string) bool {
	if w == nil {
		return false
	}

	for _, msg := range *w {
		if strings.Contains(msg, text) {
			return true
		}
	}
	return false
}

// String returns all warning messages joined by commas.
func (w *WarningMessages) String() string {
	if w == nil {
		return ""
	}
	return strings.Join(*w, ",")
}

// Info holds a collection of informational messages.
type InfoMessages []string

// HasInfo reports whether any informational messages exist.
func (w *InfoMessages) HasInfo() bool {
	return w != nil && len(*w) > 0
}

// AddInfo appends an informational message.
func (w *InfoMessages) AddInfo(msg string) {
	*w = append(*w, msg)
}

// MergeInfo appends informational messages from another collection.
func (w *InfoMessages) MergeInfo(other InfoMessages) {
	*w = append(*w, other...)
}

// Contains reports whether any informational message contains the given substring.
func (w *InfoMessages) Contains(text string) bool {
	if w == nil {
		return false
	}

	for _, msg := range *w {
		if strings.Contains(msg, text) {
			return true
		}
	}
	return false
}

// String returns all informational messages joined by commas.
func (w *InfoMessages) String() string {
	if w == nil {
		return ""
	}
	return strings.Join(*w, ",")
}
