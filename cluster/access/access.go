package access

import (
	"context"
	"fmt"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/intentd/tools/fileio"
)

// Options holds what a run is given beside the Config. They come from the
// caller (CLI flags, a product's defaults), not the file.
type Options struct {
	// HubNamespace is where the credential is written on the hub; default
	// "default".
	HubNamespace string
	// DryRun says what would be done without changing anything.
	DryRun bool
	// EmitRBACOnly returns the rules without connecting to any cluster.
	EmitRBACOnly bool
	// Rules, when not nil, replace every cluster's rules.
	Rules []rbacv1.PolicyRule
	// Naming names what is created; the zero value is DefaultNaming.
	Naming Naming
	// Format, when set, keeps every credential this way, whatever the
	// Config says; nil follows each cluster's format setting.
	Format HubFormat

	// connect and hub reach the clusters; tests replace them.
	connect func(kubectx string) (cs kubernetes.Interface, endpoint string, ca []byte, err error)
	hub     func() (kubernetes.Interface, error)
	now     func() time.Time
}

func (o Options) withDefaults() Options {
	if o.HubNamespace == "" {
		o.HubNamespace = "default"
	}
	o.Naming = o.Naming.orDefault()
	if o.connect == nil {
		o.connect = connectContext
	}
	if o.hub == nil {
		o.hub = func() (kubernetes.Interface, error) {
			cs, _, _, err := connectContext("")
			return cs, err
		}
	}
	if o.now == nil {
		o.now = time.Now
	}
	return o
}

// Result is what a run did for one cluster. The credential is the Secret
// SecretNamespace/SecretName on the hub.
type Result struct {
	Name            string
	Settings        Effective
	Revoked         bool
	Endpoint        string
	SecretName      string
	SecretNamespace string
	HasCA           bool
	// ExpiresAt is when a short token expires; zero for a long one.
	ExpiresAt time.Time
}

// Run grants or revokes (per cluster's Revoke) every cluster in cfg, in name
// order, stopping at the first failure. cfg must have passed Validate.
func Run(ctx context.Context, cfg *Config, opts Options, log func(string)) ([]*Result, error) {
	var results []*Result
	for _, name := range cfg.Names() {
		var r *Result
		var err error
		if cfg.Clusters[name].Revoke {
			r, err = Revoke(ctx, cfg, name, opts, log)
		} else {
			r, err = Grant(ctx, cfg, name, opts, log)
		}
		if err != nil {
			return results, fmt.Errorf("cluster %s: %w", name, err)
		}
		results = append(results, r)
	}
	return results, nil
}

// Grant gives the hub (the current kubeconfig context) access to cluster
// name: on the target, a ServiceAccount, a ClusterRole with the rules and its
// binding, and a token; on the hub, the credential, through opts.Format.
//
// A short token comes from TokenRequest, and the ServiceAccount may request
// its own next one, so Refresh renews it. A long token is a token Secret,
// reused unless rotated. A rerun reconciles the roles and rewrites the
// credential.
func Grant(ctx context.Context, cfg *Config, name string, opts Options, log func(string)) (*Result, error) {
	opts = opts.withDefaults()
	e := cfg.Effective(name, opts.Naming)
	if opts.Rules != nil {
		e.Rules = opts.Rules
	}
	result := &Result{
		Name: name, Settings: e,
		SecretName: opts.Naming.HubSecretName(name), SecretNamespace: opts.HubNamespace,
	}
	if opts.EmitRBACOnly {
		return result, nil
	}

	cs, endpoint, ca, err := opts.connect(cfg.Clusters[name].Context)
	if err != nil {
		return nil, err
	}
	result.Endpoint, result.HasCA = endpoint, len(ca) > 0
	t := target{cs: cs, e: e, labels: opts.Naming.Labels, dryRun: opts.DryRun, log: log}

	if err := t.ensureServiceAccount(ctx); err != nil {
		return nil, err
	}
	if err := t.ensureClusterRole(ctx); err != nil {
		return nil, err
	}

	c := Credential{
		Cluster: name, Endpoint: endpoint, CA: ca,
		ServiceAccount: e.ServiceAccount, Namespace: e.Namespace,
	}
	switch e.Token {
	case TokenShort:
		if err := t.ensureTokenRole(ctx); err != nil {
			return nil, err
		}
		if opts.DryRun {
			log(fmt.Sprintf("dry-run: token for %s/%s (%s)", e.Namespace, e.ServiceAccount, e.TTL))
			return result, nil
		}
		// A long token left from an earlier grant would outlive this one.
		if err := t.removeLongToken(ctx); err != nil {
			return nil, fmt.Errorf("removing the earlier long token: %w", err)
		}
		now := opts.now()
		token, expires, err := requestToken(ctx, cs, e.Namespace, e.ServiceAccount, e.TTL, now)
		if err != nil {
			return nil, err
		}
		c.Token, c.IssuedAt, c.ExpiresAt = token, now, expires
		result.ExpiresAt = expires
		log(fmt.Sprintf("token for %s/%s: issued, expires %s", e.Namespace, e.ServiceAccount, expires.UTC().Format(time.RFC3339)))
	case TokenLong:
		token, err := t.longToken(ctx, e.Rotate)
		if err != nil {
			return nil, err
		}
		if opts.DryRun {
			return result, nil
		}
		if err := t.removeTokenRole(ctx); err != nil {
			return nil, err
		}
		c.Token = token
	}

	hub, err := opts.hub()
	if err != nil {
		return nil, fmt.Errorf("connecting to the hub: %w", err)
	}
	format := opts.Format
	if format == nil {
		format = Formats[e.Format]
	}
	if err := format.Write(ctx, hub, opts.HubNamespace, result.SecretName, c, opts.Naming, log); err != nil {
		return nil, err
	}
	return result, nil
}

// Revoke removes cluster name's access: the ServiceAccount, its roles and
// token on the target, then the credential on the hub. It is destructive;
// a CLI should confirm first.
func Revoke(ctx context.Context, cfg *Config, name string, opts Options, log func(string)) (*Result, error) {
	opts = opts.withDefaults()
	e := cfg.Effective(name, opts.Naming)
	result := &Result{
		Name: name, Settings: e, Revoked: true,
		SecretName: opts.Naming.HubSecretName(name), SecretNamespace: opts.HubNamespace,
	}
	cs, endpoint, _, err := opts.connect(cfg.Clusters[name].Context)
	if err != nil {
		return nil, err
	}
	result.Endpoint = endpoint
	t := target{cs: cs, e: e, labels: opts.Naming.Labels, dryRun: opts.DryRun, log: log}
	if err := t.revoke(ctx); err != nil {
		return nil, err
	}
	if opts.DryRun {
		log(fmt.Sprintf("dry-run: remove Secret %s/%s on the hub", opts.HubNamespace, result.SecretName))
		return result, nil
	}
	hub, err := opts.hub()
	if err != nil {
		return nil, fmt.Errorf("connecting to the hub: %w", err)
	}
	if err := ignoreNotFound(hub.CoreV1().Secrets(opts.HubNamespace).Delete(ctx, result.SecretName, metav1.DeleteOptions{})); err != nil {
		return nil, fmt.Errorf("deleting the hub credential: %w", err)
	}
	log(fmt.Sprintf("Secret %s/%s: removed", opts.HubNamespace, result.SecretName))
	return result, nil
}

// connectContext connects to kubeconfig context kubectx ("" is the current
// one) and returns its endpoint and CA.
func connectContext(kubectx string) (kubernetes.Interface, string, []byte, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubectx},
	).ClientConfig()
	if err != nil {
		return nil, "", nil, fmt.Errorf("cannot load context %q: %w", kubectx, err)
	}
	ca := cfg.TLSClientConfig.CAData
	if len(ca) == 0 && cfg.TLSClientConfig.CAFile != "" {
		if ca, err = fileio.ReadLocal(cfg.TLSClientConfig.CAFile); err != nil {
			return nil, "", nil, fmt.Errorf("reading CA file: %w", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, "", nil, fmt.Errorf("connecting to context %q: %w", kubectx, err)
	}
	return cs, cfg.Host, ca, nil
}
