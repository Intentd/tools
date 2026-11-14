package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/intentd/tools/version"
)

func TestDefaults(t *testing.T) {
	assert.Equal(t, "dev", version.Short())
	assert.Equal(t, "dev (commit: none, built: unknown)", version.String())
}
