package access

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/intentd/tools/duration"
	"github.com/intentd/tools/fileio"
)

// TokenType is how long the hub's token lives.
type TokenType string

const (
	// TokenShort is a TokenRequest token with a TTL, renewed by Refresh.
	TokenShort TokenType = "short"
	// TokenLong is a ServiceAccount token Secret that does not expire.
	TokenLong TokenType = "long"
)

// TokenTypes is every TokenType.
var TokenTypes = []TokenType{TokenShort, TokenLong}

// IsValid reports whether t is one of TokenTypes.
func (t TokenType) IsValid() bool { return slices.Contains(TokenTypes, t) }

const (
	// DefaultTTL is a short token's life when none is declared.
	DefaultTTL = 24 * time.Hour
	// MinTTL is the shortest life TokenRequest issues.
	MinTTL = 10 * time.Minute
	// DefaultSANamespace is where the ServiceAccount goes on the target.
	DefaultSANamespace = "kube-system"
)

// Token is the hub's token.
type Token struct {
	// Type is short (default) or long.
	Type TokenType `json:"type,omitempty"`
	// TTL is a short token's life, e.g. "24h" (see tools/duration). Default
	// DefaultTTL; at least MinTTL. Short tokens only.
	TTL string `json:"ttl,omitempty"`
	// Rotate issues a new long token instead of reusing the current one.
	// Long tokens only: a short one is new on every run.
	Rotate *bool `json:"rotate,omitempty"`
}

// Settings is what a cluster's access is made of. Unset fields take the
// defaults' value, then the built-in one.
type Settings struct {
	// ServiceAccount on the target; default Naming.ServiceAccount.
	ServiceAccount string `json:"serviceAccount,omitempty"`
	// Namespace of the ServiceAccount on the target; default DefaultSANamespace.
	Namespace string `json:"namespace,omitempty"`
	// Token is the hub's token.
	Token Token `json:"token,omitempty"`
	// Rules of the ClusterRole bound to the ServiceAccount. With none, no
	// ClusterRole is made.
	Rules []rbacv1.PolicyRule `json:"rules,omitempty"`
	// Format is how the credential is kept on the hub: secret (default),
	// argocd, kubeconfig or flux.
	Format FormatName `json:"format,omitempty"`
}

// Cluster is one target cluster.
type Cluster struct {
	// Context is the kubeconfig context that reaches the target (required).
	Context string `json:"context"`
	// Revoke removes the access instead of granting it: the ServiceAccount,
	// its roles and token on the target, and the credential on the hub.
	Revoke bool `json:"revoke,omitempty"`
	Settings
}

// Config is a file of clusters to give a hub access to:
//
//	defaults:
//	  token: {type: short, ttl: 24h}
//	clusters:
//	  staging:
//	    context: kind-staging
//	  prod:
//	    context: kind-prod
//	    namespace: restricted-ns
//	    rules:
//	      - apiGroups: [apps]
//	        resources: [deployments]
//	        verbs: [get, list, watch]
type Config struct {
	Defaults Settings           `json:"defaults,omitempty"`
	Clusters map[string]Cluster `json:"clusters"`
}

// LoadConfig reads a YAML or JSON Config. Unknown keys are errors.
func LoadConfig(path string) (*Config, error) {
	data, err := fileio.ReadLocal(path)
	if err != nil {
		return nil, fmt.Errorf("reading access config: %w", err)
	}
	var cfg Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing access config %s: %w", path, err)
	}
	return &cfg, nil
}

// Names returns the cluster names, sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Clusters))
	for name := range c.Clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Effective is a cluster's settings once resolved.
type Effective struct {
	ServiceAccount string
	Namespace      string
	Token          TokenType
	TTL            time.Duration // short tokens
	Rotate         bool          // long tokens
	Rules          []rbacv1.PolicyRule
	Format         FormatName
}

// Effective resolves cluster name's settings over the defaults and n. It
// assumes Validate passed.
func (c *Config) Effective(name string, n Naming) Effective {
	cl := c.Clusters[name]
	e := Effective{
		ServiceAccount: first(cl.ServiceAccount, c.Defaults.ServiceAccount, n.ServiceAccount),
		Namespace:      first(cl.Namespace, c.Defaults.Namespace, DefaultSANamespace),
		Token:          TokenType(first(string(cl.Token.Type), string(c.Defaults.Token.Type), string(TokenShort))),
		Format:         FormatName(first(string(cl.Format), string(c.Defaults.Format), string(FormatSecret))),
		Rules:          cl.Rules,
	}
	if e.Rules == nil {
		e.Rules = c.Defaults.Rules
	}
	switch e.Token {
	case TokenShort:
		e.TTL = DefaultTTL
		if ttl := first(cl.Token.TTL, c.Defaults.Token.TTL); ttl != "" {
			e.TTL, _ = duration.Parse(ttl)
		}
	case TokenLong:
		if r := cl.Token.Rotate; r != nil {
			e.Rotate = *r
		} else if r := c.Defaults.Token.Rotate; r != nil {
			e.Rotate = *r
		}
	}
	return e
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Validate reports every problem in c at once, each naming where it is.
func (c *Config) Validate(n Naming) error {
	var errs []error
	add := func(where, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, args...)))
	}
	if len(c.Clusters) == 0 {
		return errors.New("access config: no clusters")
	}
	checkSettings("defaults", c.Defaults, add)
	for _, name := range c.Names() {
		cl := c.Clusters[name]
		where := "clusters." + name
		if msgs := validation.IsDNS1123Label(name); len(msgs) > 0 {
			add(where, "name %q: %s", name, strings.Join(msgs, "; "))
		} else if msgs := validation.IsDNS1123Subdomain(n.HubSecretName(name)); len(msgs) > 0 {
			add(where, "hub Secret %q: %s", n.HubSecretName(name), strings.Join(msgs, "; "))
		}
		if strings.TrimSpace(cl.Context) == "" {
			add(where, "context is required")
		}
		checkSettings(where, cl.Settings, add)
		if sa := c.Effective(name, n).ServiceAccount; sa == "" {
			add(where, "no serviceAccount: set one here, in defaults, or in the Naming")
		}
	}
	return errors.Join(errs...)
}

// checkSettings checks the fields s sets. A token setting that does not fit
// a type set at the same level is an error; one inherited is ignored.
func checkSettings(where string, s Settings, add func(where, format string, args ...any)) {
	if s.ServiceAccount != "" {
		if msgs := validation.IsDNS1123Subdomain(s.ServiceAccount); len(msgs) > 0 {
			add(where, "serviceAccount %q: %s", s.ServiceAccount, strings.Join(msgs, "; "))
		}
	}
	if s.Namespace != "" {
		if msgs := validation.IsDNS1123Label(s.Namespace); len(msgs) > 0 {
			add(where, "namespace %q: %s", s.Namespace, strings.Join(msgs, "; "))
		}
	}
	if s.Format != "" && !s.Format.IsValid() {
		add(where, "format %q: use secret, argocd, kubeconfig or flux", s.Format)
	}
	t := s.Token
	if t.Type != "" && !t.Type.IsValid() {
		add(where, "token.type %q: use short or long", t.Type)
	}
	if t.TTL != "" {
		switch d, err := duration.Parse(t.TTL); {
		case err != nil:
			add(where, "token.ttl %q: %v", t.TTL, err)
		case d < MinTTL:
			add(where, "token.ttl %s is under the %s TokenRequest issues", t.TTL, MinTTL)
		case t.Type == TokenLong:
			add(where, "token.ttl applies to short tokens; a long token does not expire")
		}
	}
	if t.Rotate != nil && t.Type == TokenShort {
		add(where, "token.rotate applies to long tokens; a short token is new on every run")
	}
	for i, rule := range s.Rules {
		at := fmt.Sprintf("%s.rules[%d]", where, i)
		if len(rule.NonResourceURLs) == 0 && (len(rule.APIGroups) == 0 || len(rule.Resources) == 0) {
			add(at, `apiGroups and resources are required (use [""] for the core group)`)
		}
		if len(rule.Verbs) == 0 {
			add(at, "verbs are required")
		}
		for _, verb := range rule.Verbs {
			if !KnownVerbs[verb] {
				add(at, "unknown verb %q", verb)
			}
		}
	}
}
