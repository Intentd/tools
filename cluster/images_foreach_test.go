package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/intentd/tools/resolver"
)

func decodeImages(t *testing.T, doc string) Images {
	t.Helper()
	var imgs Images
	require.NoError(t, yaml.Unmarshal([]byte(doc), &imgs))
	return imgs
}

// Each row is the entry with its templates resolved: a templated port
// decodes as a number, a templated name is slugified, ${{ … }} is left for
// the run, and the entries around it pass through.
func TestImagesExpandForEach(t *testing.T) {
	imgs := decodeImages(t, `
- worker:dev
- name: "api-{{ .item.ref }}"
  forEach:
    items:
      - { ref: main, port: 8080 }
      - { ref: release/1.3, port: 8081 }
  image: "api:{{ .item.port }}"
  git: { url: https://github.com/acme/api.git, ref: "{{ .item.ref }}" }
  build: { context: ., args: { VERSION: "{{ .item.ref }}", TOKEN: "${{ env.TOKEN }}" } }
  deploy: { ports: ["{{ .item.port }}"], env: { DATABASE_URL: "${{ ecosystem.postgres.url }}" } }
`)
	got, err := imgs.ExpandForEach(resolver.NewWithFunctions(nil))
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "worker:dev", got[0].Image)
	assert.Nil(t, got[0].Row)

	main, rel := got[1], got[2]
	assert.Equal(t, "api-main", main.Name)
	assert.Equal(t, "api-release-1-3", rel.Name, "slugified")
	assert.Equal(t, "api:8081", rel.Image)
	assert.Equal(t, "release/1.3", rel.Git.Ref)
	assert.Equal(t, int32(8081), rel.Deploy.Ports[0].Port, "a templated port decodes as one")
	assert.Equal(t, "release/1.3", rel.Build.Args["VERSION"])
	assert.Equal(t, "${{ env.TOKEN }}", rel.Build.Args["TOKEN"], "contexts are the run's")
	assert.Equal(t, "${{ ecosystem.postgres.url }}", rel.Deploy.Env["DATABASE_URL"])
	assert.Equal(t, &ImageRow{Entry: 1, Index: 1, Base: "api"}, rel.Row)
	assert.Nil(t, main.ForEach)
}

// Without a templated name, rows are <base>-<entry>-<row>: the repository's
// name for an image from git; a matrix row is a map like an item.
func TestImagesExpandForEach_DefaultNames(t *testing.T) {
	imgs := decodeImages(t, `
- forEach:
    matrix: { dockerfile: [Dockerfile, Dockerfile.prod] }
  git: { url: "git@github.com:acme/payments-api.git" }
  build: { context: ., dockerfile: "{{ .item.dockerfile }}" }
`)
	got, err := imgs.ExpandForEach(resolver.NewWithFunctions(nil))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "payments-api-0-0", got[0].Name)
	assert.Equal(t, "payments-api-0-1", got[1].Name)
	assert.Equal(t, "Dockerfile.prod", got[1].Build.Dockerfile)
	assert.Equal(t, "payments-api", got[1].Row.Base)
}

// A template reading what a row doesn't have fails, naming the row; so does
// a forEach shape that makes no rows.
func TestImagesExpandForEach_Errors(t *testing.T) {
	_, err := decodeImages(t, `
- forEach: { items: [a] }
  image: "x:{{ .spec.version }}"
`).ExpandForEach(resolver.NewWithFunctions(nil))
	assert.ErrorContains(t, err, "images[0] forEach row 0")
	assert.ErrorContains(t, err, "a row doesn't have")

	_, err = decodeImages(t, "- forEach: {}\n  image: x\n").ExpandForEach(resolver.NewWithFunctions(nil))
	assert.ErrorContains(t, err, "set items or matrix")
}
