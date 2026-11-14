package external

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver"
)

func TestDo_HTTP(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Token")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var recorded []string
	tmpl := resolver.New(map[string]any{"base": srv.URL}, nil)
	got, err := Do(context.Background(), tmpl, Call{
		Name: "health", URL: "{{ .base }}/health", Credential: "s3cr3t", AuthHeader: "X-Token",
	}, Options{Where: "external[0]", Record: func(name, url string, _ float64, callErr string) {
		recorded = append(recorded, name+" "+url+" "+callErr)
	}})
	require.NoError(t, err)
	assert.Equal(t, "200", got["status"])
	assert.Equal(t, map[string]any{"ok": true}, got["json"])
	assert.Equal(t, "Bearer s3cr3t", gotAuth)
	assert.Equal(t, []string{"health " + srv.URL + "/health "}, recorded)
}

func TestDo_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	tmpl := resolver.New(nil, nil)

	got, err := Do(context.Background(), tmpl, Call{Name: "down", URL: srv.URL}, Options{})
	require.Error(t, err)
	assert.Equal(t, "HTTP 503", got["error"], "the failed result comes back with the error")

	got, err = Do(context.Background(), tmpl, Call{Name: "down", URL: srv.URL, ContinueOnError: true}, Options{})
	require.NoError(t, err)
	assert.Equal(t, "HTTP 503", got["error"])

	_, err = Do(context.Background(), tmpl, Call{Name: "bad", URL: "{{ .x"}, Options{Where: "external[1]"})
	assert.ErrorContains(t, err, "external[1].url")
}

func TestDo_SkippedWhenConditionsFail(t *testing.T) {
	tmpl := resolver.New(map[string]any{"enabled": "no"}, nil)
	got, err := Do(context.Background(), tmpl, Call{
		Name: "never", URL: "http://unreachable.invalid",
		When: condition.Conditions{{Field: "enabled", Equals: "yes"}},
	}, Options{})
	require.NoError(t, err)
	assert.Equal(t, "false", got["called"])
}
