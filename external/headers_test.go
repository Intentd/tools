package external

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func upper(v string) (string, error) { return strings.ToUpper(v), nil }

func TestIdempotencyKey_RepeatsForTheSameRequestOnly(t *testing.T) {
	base := IdempotencyKey("uid-1", "POST", "https://api/buckets", `{"q":1}`)
	assert.Equal(t, base, IdempotencyKey("uid-1", "post", "https://api/buckets", `{"q":1}`), "a retry repeats the key")
	assert.NotEqual(t, base, IdempotencyKey("uid-1", "POST", "https://api/buckets", `{"q":2}`), "a changed request gets a new key")
	assert.NotEqual(t, base, IdempotencyKey("uid-2", "POST", "https://api/buckets", `{"q":1}`), "two objects never share a key")
	assert.NotEqual(t, base, IdempotencyKey("uid-1", "POST", "https://api/other", `{"q":1}`))
}

func TestRequestHeaders(t *testing.T) {
	h, err := RequestHeaders(upper, map[string]string{"X-Tenant": "acme"}, nil, "uid-1", "GET", "u", "")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"X-Tenant": "ACME"}, h, "no key on GET")

	h, err = RequestHeaders(upper, nil, nil, "uid-1", "POST", "u", "b")
	require.NoError(t, err)
	assert.Len(t, h[DefaultIdempotencyHeader], 32)

	h, err = RequestHeaders(upper, nil, &Idempotency{Header: "PayPal-Request-Id", Value: "k-{{x}}"}, "uid", "POST", "u", "b")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PayPal-Request-Id": "K-{{X}}"}, h)

	h, err = RequestHeaders(upper, nil, &Idempotency{Disabled: true}, "uid", "POST", "u", "b")
	require.NoError(t, err)
	assert.Empty(t, h)
}

func TestObjectIdentity(t *testing.T) {
	assert.Equal(t, "uid-1", ObjectIdentity(map[string]any{"metadata": map[string]any{"uid": "uid-1", "name": "logs"}}))
	assert.Equal(t, "default/logs", ObjectIdentity(map[string]any{"metadata": map[string]any{"namespace": "default", "name": "logs"}}))
}
