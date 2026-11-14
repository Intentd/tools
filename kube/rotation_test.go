// Tests for RotationDue (secret.go). Duration parsing is tested in
// tools/duration.
package kube_test

import (
	"testing"
	"time"

	"github.com/intentd/tools/kube"
	"github.com/stretchr/testify/assert"
)

// ── RotationDue ─────────────────────────────────────────────────────────────

func TestRotationDue_NoRotateAfter(t *testing.T) {
	past := time.Now().Add(-400 * 24 * time.Hour).Format(time.RFC3339)
	assert.False(t, kube.RotationDue(past, ""))
}

func TestRotationDue_NotYetExpired(t *testing.T) {
	recent := time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	assert.False(t, kube.RotationDue(recent, "90d"))
}

func TestRotationDue_Expired(t *testing.T) {
	old := time.Now().Add(-91 * 24 * time.Hour).Format(time.RFC3339)
	assert.True(t, kube.RotationDue(old, "90d"))
}

func TestRotationDue_ExactThreshold(t *testing.T) {
	// Just at the boundary: age == threshold means rotate (>=)
	exact := time.Now().Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	assert.True(t, kube.RotationDue(exact, "1y"))
}

func TestRotationDue_InvalidTimestamp(t *testing.T) {
	// Unparseable annotation → rotate to be safe
	assert.True(t, kube.RotationDue("not-a-timestamp", "30d"))
}

func TestRotationDue_InvalidDuration(t *testing.T) {
	// Invalid duration → do not rotate unexpectedly
	recent := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	assert.False(t, kube.RotationDue(recent, "invalid"))
}

func TestRotationDue_WeekRotation(t *testing.T) {
	old := time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	assert.True(t, kube.RotationDue(old, "1w"))
}

func TestRotationDue_MonthRotation_NotYet(t *testing.T) {
	recent := time.Now().Add(-15 * 24 * time.Hour).Format(time.RFC3339)
	assert.False(t, kube.RotationDue(recent, "1mo"))
}

func TestGenerationAnnotations_Keys(t *testing.T) {
	def := kube.GenerationAnnotations("90d")
	assert.Contains(t, def, kube.DefaultRotationKeys.GeneratedAt)
	assert.Equal(t, "90d", def[kube.DefaultRotationKeys.RotateAfter])

	own := kube.GenerationAnnotations("", kube.WithRotationKeys("x.io/at", "x.io/after"))
	assert.Contains(t, own, "x.io/at")
	assert.NotContains(t, own, "x.io/after")
}
