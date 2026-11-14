package access

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/intentd/tools/kubeclient/kubefake"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// cluster is a fake target that issues tokens "tok-1", "tok-2", … and fills
// token Secrets as the token controller would.
type cluster struct {
	cs     *kubefake.Clientset
	issued int
	asked  []time.Duration
}

func newCluster() *cluster {
	c := &cluster{cs: kubefake.NewClientset()}
	c.cs.PrependReactor("create", "serviceaccounts", func(a kubefake.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "token" {
			return false, nil, nil
		}
		tr := a.(kubefake.CreateAction).GetObject().(*authv1.TokenRequest)
		ttl := time.Duration(*tr.Spec.ExpirationSeconds) * time.Second
		c.issued++
		c.asked = append(c.asked, ttl)
		tr.Status = authv1.TokenRequestStatus{Token: fmt.Sprintf("tok-%d", c.issued), ExpirationTimestamp: metav1.NewTime(t0.Add(ttl))}
		return true, tr, nil
	})
	c.cs.PrependReactor("create", "secrets", func(a kubefake.Action) (bool, runtime.Object, error) {
		if s := a.(kubefake.CreateAction).GetObject().(*corev1.Secret); s.Type == corev1.SecretTypeServiceAccountToken {
			c.issued++
			s.Data = map[string][]byte{"token": []byte(fmt.Sprintf("long-%d", c.issued))}
		}
		return false, nil, nil
	})
	return c
}

func opts(target *cluster, hub kubernetes.Interface) Options {
	return Options{
		connect: func(string) (kubernetes.Interface, string, []byte, error) {
			return target.cs, "https://prod.internal:6443", []byte("PEM"), nil
		},
		hub: func() (kubernetes.Interface, error) { return hub, nil },
		now: func() time.Time { return t0 },
	}
}

func cfg(c Cluster) *Config { return &Config{Clusters: map[string]Cluster{"prod": c}} }

var readRules = []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get"}}}

func nolog(string) {}

func TestGrant_ShortByDefault(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := cfg(Cluster{Context: "kind-prod", Settings: Settings{Rules: readRules}})
	r, err := Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)

	assert.Equal(t, TokenShort, r.Settings.Token)
	assert.Equal(t, t0.Add(DefaultTTL), r.ExpiresAt)
	assert.Equal(t, []time.Duration{DefaultTTL}, target.asked)

	sa := DefaultNaming.ServiceAccount
	_, err = target.cs.CoreV1().ServiceAccounts(DefaultSANamespace).Get(ctx, sa, metav1.GetOptions{})
	require.NoError(t, err)
	cr, err := target.cs.RbacV1().ClusterRoles().Get(ctx, sa, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, readRules, cr.Rules)
	role, err := target.cs.RbacV1().Roles(DefaultSANamespace).Get(ctx, TokenRoleName(sa), metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{sa}, role.Rules[0].ResourceNames, "a short token may request only its own")

	cred, err := SecretFormat{}.Read(ctx, hub, "default", "cluster-access-prod", DefaultNaming)
	require.NoError(t, err)
	assert.Equal(t, "tok-1", cred.Token)
	assert.Equal(t, "https://prod.internal:6443", cred.Endpoint)
	assert.Equal(t, "PEM", string(cred.CA))
	assert.Equal(t, sa, cred.ServiceAccount)
	assert.Equal(t, t0.Add(DefaultTTL), cred.ExpiresAt)
}

func TestGrant_LongReusesUnlessRotated(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := cfg(Cluster{Context: "kind-prod", Settings: Settings{Token: Token{Type: TokenLong}}})
	_, err := Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)
	_, err = Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)
	cred, _ := SecretFormat{}.Read(ctx, hub, "default", "cluster-access-prod", DefaultNaming)
	assert.Equal(t, "long-1", cred.Token, "a rerun reuses the long token")
	assert.False(t, cred.Short())

	yes := true
	c.Clusters["prod"] = Cluster{Context: "kind-prod", Settings: Settings{Token: Token{Type: TokenLong, Rotate: &yes}}}
	_, err = Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)
	cred, _ = SecretFormat{}.Read(ctx, hub, "default", "cluster-access-prod", DefaultNaming)
	assert.Equal(t, "long-2", cred.Token, "rotate issues a new one")

	_, err = target.cs.RbacV1().Roles(DefaultSANamespace).Get(ctx, TokenRoleName("cluster-access"), metav1.GetOptions{})
	assert.Error(t, err, "a long token gets no self-request Role")
	_, err = target.cs.RbacV1().ClusterRoles().Get(ctx, "cluster-access", metav1.GetOptions{})
	assert.Error(t, err, "no rules, no ClusterRole")
}

func TestGrant_SwitchingToShortRemovesLongToken(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := cfg(Cluster{Context: "kind-prod", Settings: Settings{Token: Token{Type: TokenLong}}})
	_, err := Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)

	c.Clusters["prod"] = Cluster{Context: "kind-prod"}
	_, err = Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)
	_, err = target.cs.CoreV1().Secrets(DefaultSANamespace).Get(ctx, TokenSecretName("cluster-access"), metav1.GetOptions{})
	assert.Error(t, err, "the long token Secret is gone, so its token no longer works")
}

func TestGrant_DryRunChangesNothing(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	o := opts(target, hub)
	o.DryRun = true
	var logged []string
	_, err := Grant(ctx, cfg(Cluster{Context: "kind-prod", Settings: Settings{Rules: readRules}}), "prod", o, func(m string) { logged = append(logged, m) })
	require.NoError(t, err)
	assert.Empty(t, target.cs.Actions())
	assert.Empty(t, hub.Actions())
	assert.NotEmpty(t, logged)
}

func TestRevoke_RemovesEverything(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := cfg(Cluster{Context: "kind-prod", Settings: Settings{Rules: readRules}})
	_, err := Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)

	c.Clusters["prod"] = Cluster{Context: "kind-prod", Revoke: true, Settings: Settings{Rules: readRules}}
	results, err := Run(ctx, c, opts(target, hub), nolog)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Revoked)

	_, err = target.cs.CoreV1().ServiceAccounts(DefaultSANamespace).Get(ctx, "cluster-access", metav1.GetOptions{})
	assert.Error(t, err)
	_, err = target.cs.RbacV1().ClusterRoles().Get(ctx, "cluster-access", metav1.GetOptions{})
	assert.Error(t, err)
	_, err = target.cs.RbacV1().Roles(DefaultSANamespace).Get(ctx, TokenRoleName("cluster-access"), metav1.GetOptions{})
	assert.Error(t, err)
	_, err = hub.CoreV1().Secrets("default").Get(ctx, "cluster-access-prod", metav1.GetOptions{})
	assert.Error(t, err)

	_, err = Revoke(ctx, c, "prod", opts(target, hub), nolog)
	assert.NoError(t, err, "revoking what is gone is not an error")
}

func TestRefresh(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := cfg(Cluster{Context: "kind-prod", Settings: Settings{Token: Token{TTL: "2h"}}})
	_, err := Grant(ctx, c, "prod", opts(target, hub), nolog)
	require.NoError(t, err)

	var used string
	ro := func(at time.Time) RefreshOptions {
		return RefreshOptions{
			now: func() time.Time { return at },
			target: func(cfg *rest.Config) (kubernetes.Interface, error) {
				used = cfg.BearerToken
				return target.cs, nil
			},
		}
	}

	got, err := Refresh(ctx, hub, "default", "cluster-access-prod", ro(t0.Add(30*time.Minute)))
	require.NoError(t, err)
	assert.False(t, got.Renewed, "not yet half way")

	got, err = Refresh(ctx, hub, "default", "cluster-access-prod", ro(t0.Add(90*time.Minute)))
	require.NoError(t, err)
	assert.True(t, got.Renewed)
	assert.Equal(t, "tok-1", used, "the current token asks for the next")
	cred, _ := SecretFormat{}.Read(ctx, hub, "default", "cluster-access-prod", DefaultNaming)
	assert.Equal(t, "tok-2", cred.Token)
	assert.Equal(t, t0.Add(90*time.Minute), cred.IssuedAt)
	assert.Equal(t, []time.Duration{2 * time.Hour, 2 * time.Hour}, target.asked, "the same TTL is asked again")

	_, err = Refresh(ctx, hub, "default", "cluster-access-prod", ro(cred.ExpiresAt.Add(time.Minute)))
	assert.ErrorContains(t, err, "grant the access again")
}

func TestRefresh_LongIsLeftAlone(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	_, err := Grant(ctx, cfg(Cluster{Context: "kind-prod", Settings: Settings{Token: Token{Type: TokenLong}}}), "prod", opts(target, hub), nolog)
	require.NoError(t, err)
	got, err := Refresh(ctx, hub, "default", "cluster-access-prod", RefreshOptions{Force: true})
	require.NoError(t, err)
	assert.False(t, got.Renewed)
}

func TestFormats_RoundTrip(t *testing.T) {
	ctx := context.Background()
	c := Credential{
		Cluster: "prod", Endpoint: "https://p:6443", Token: "tok", CA: []byte("PEM"),
		ServiceAccount: "sa", Namespace: "kube-system", IssuedAt: t0, ExpiresAt: t0.Add(time.Hour),
	}
	for _, f := range Formats {
		hub := kubefake.NewClientset()
		require.NoError(t, f.Write(ctx, hub, "argocd", "inrun-prod", c, DefaultNaming, nolog))
		got, err := f.Read(ctx, hub, "argocd", "inrun-prod", DefaultNaming)
		require.NoError(t, err)
		assert.Equal(t, c, got, "%T", f)
	}

	hub := kubefake.NewClientset()
	require.NoError(t, ArgoCDFormat{}.Write(ctx, hub, "argocd", "inrun-prod", c, DefaultNaming, nolog))
	s, _ := hub.CoreV1().Secrets("argocd").Get(ctx, "inrun-prod", metav1.GetOptions{})
	assert.Equal(t, ArgoCDSecretType, s.Labels["argocd.argoproj.io/secret-type"])
	assert.NotContains(t, DefaultNaming.Labels, "argocd.argoproj.io/secret-type", "Naming's labels are not changed")
	assert.JSONEq(t, `{"bearerToken":"tok","tlsClientConfig":{"insecure":false,"caData":"UEVN"}}`, string(s.Data["config"]))

	hub = kubefake.NewClientset()
	require.NoError(t, Formats[FormatKubeconfig].Write(ctx, hub, "default", "inrun-prod", c, DefaultNaming, nolog))
	s, _ = hub.CoreV1().Secrets("default").Get(ctx, "inrun-prod", metav1.GetOptions{})
	assert.Contains(t, s.Data, KubeconfigKey)
	assert.NotContains(t, s.Data, FluxKey, "flux and kubeconfig differ only in the key")
}

func TestGrant_FormatPerCluster(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	c := &Config{
		Defaults: Settings{Format: FormatFlux},
		Clusters: map[string]Cluster{
			"prod":    {Context: "kind-prod", Settings: Settings{Format: FormatArgoCD}},
			"staging": {Context: "kind-staging"},
		},
	}
	o := opts(target, hub)
	o.HubNamespace = "argocd"
	_, err := Run(ctx, c, o, nolog)
	require.NoError(t, err)

	prod, err := hub.CoreV1().Secrets("argocd").Get(ctx, "cluster-access-prod", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, ArgoCDSecretType, prod.Labels["argocd.argoproj.io/secret-type"])
	staging, err := hub.CoreV1().Secrets("argocd").Get(ctx, "cluster-access-staging", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Contains(t, string(staging.Data[FluxKey]), "server: https://prod.internal:6443", "the default format is flux here")

	for name, want := range map[string]HubFormat{"cluster-access-prod": ArgoCDFormat{}, "cluster-access-staging": Formats[FormatFlux]} {
		got, err := FormatOf(ctx, hub, "argocd", name, DefaultNaming)
		require.NoError(t, err)
		assert.Equal(t, want, got, name)
	}

	// Refresh finds the format on the credential.
	got, err := Refresh(ctx, hub, "argocd", "cluster-access-prod", RefreshOptions{
		Force:  true,
		now:    func() time.Time { return t0.Add(time.Hour) },
		target: func(*rest.Config) (kubernetes.Interface, error) { return target.cs, nil },
	})
	require.NoError(t, err)
	assert.True(t, got.Renewed)
	cred, _ := ArgoCDFormat{}.Read(ctx, hub, "argocd", "cluster-access-prod", DefaultNaming)
	assert.Equal(t, "tok-3", cred.Token)
}

func TestGrant_OptionsFormatWins(t *testing.T) {
	ctx, target, hub := context.Background(), newCluster(), kubefake.NewClientset()
	o := opts(target, hub)
	o.Format = SecretFormat{}
	_, err := Grant(ctx, cfg(Cluster{Context: "kind-prod", Settings: Settings{Format: FormatArgoCD}}), "prod", o, nolog)
	require.NoError(t, err)
	s, _ := hub.CoreV1().Secrets("default").Get(ctx, "cluster-access-prod", metav1.GetOptions{})
	assert.NotContains(t, s.Labels, "argocd.argoproj.io/secret-type")
	assert.Equal(t, "tok-1", string(s.Data[TokenKey]))
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
defaults:
  token: {ttl: 12h}
clusters:
  staging:
    context: kind-staging
  prod:
    context: kind-prod
    namespace: restricted
    token: {type: long}
    rules:
      - apiGroups: [apps]
        resources: [deployments]
        verbs: [get, list]
`), 0o600))
	c, err := LoadConfig(path)
	require.NoError(t, err)
	require.NoError(t, c.Validate(DefaultNaming))
	assert.Equal(t, []string{"prod", "staging"}, c.Names())

	staging := c.Effective("staging", DefaultNaming)
	assert.Equal(t, TokenShort, staging.Token)
	assert.Equal(t, 12*time.Hour, staging.TTL)
	assert.Equal(t, DefaultSANamespace, staging.Namespace)

	prod := c.Effective("prod", DefaultNaming)
	assert.Equal(t, TokenLong, prod.Token)
	assert.Zero(t, prod.TTL, "an inherited ttl does not apply to a long token")
	assert.Equal(t, "restricted", prod.Namespace)
	assert.Equal(t, []string{"apps"}, prod.Rules[0].APIGroups, "rules decode by their json names")

	require.NoError(t, os.WriteFile(path, []byte("clusters:\n  prod:\n    context: x\n    revok: true\n"), 0o600))
	_, err = LoadConfig(path)
	assert.ErrorContains(t, err, "revok", "unknown keys are errors")
}

func TestValidate(t *testing.T) {
	yes := true
	c := &Config{
		Defaults: Settings{Token: Token{Type: "medium"}},
		Clusters: map[string]Cluster{
			"Bad_Name":  {Context: "x"},
			"nocontext": {},
			"shortttl":  {Context: "x", Settings: Settings{Token: Token{TTL: "5m"}}},
			"longttl":   {Context: "x", Settings: Settings{Token: Token{Type: TokenLong, TTL: "1h"}}},
			"shortrot":  {Context: "x", Settings: Settings{Token: Token{Type: TokenShort, Rotate: &yes}}},
			"badformat": {Context: "x", Settings: Settings{Format: "rancher"}},
			"rules": {Context: "x", Settings: Settings{Rules: []rbacv1.PolicyRule{
				{Verbs: []string{"get"}},
				{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"peek"}},
			}}},
		},
	}
	err := c.Validate(DefaultNaming)
	require.Error(t, err)
	for _, want := range []string{
		`defaults: token.type "medium"`,
		`clusters.Bad_Name: name "Bad_Name"`,
		"clusters.nocontext: context is required",
		"clusters.shortttl: token.ttl 5m is under the 10m0s",
		"clusters.longttl: token.ttl applies to short tokens",
		"clusters.shortrot: token.rotate applies to long tokens",
		`clusters.badformat: format "rancher": use secret, argocd, kubeconfig or flux`,
		"clusters.rules.rules[0]: apiGroups and resources are required",
		`clusters.rules.rules[1]: unknown verb "peek"`,
	} {
		assert.Contains(t, err.Error(), want)
	}
	assert.ErrorContains(t, (&Config{}).Validate(DefaultNaming), "no clusters")
}

func TestNaming(t *testing.T) {
	assert.Equal(t, "inrun-prod", Naming{HubSecretPrefix: "inrun"}.HubSecretName("prod"))
	assert.Equal(t, "inrun-prod", Naming{HubSecretPrefix: "inrun-"}.HubSecretName("prod"))
	assert.Equal(t, "prod", Naming{}.HubSecretName("prod"))
	assert.Equal(t, DefaultNaming, Naming{}.orDefault())
	assert.Equal(t, "intentd.io", Naming{ServiceAccount: "x"}.orDefault().AnnotationPrefix)
}
