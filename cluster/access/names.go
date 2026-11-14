package access

import "strings"

// KnownVerbs is the set of valid Kubernetes RBAC verbs.
var KnownVerbs = map[string]bool{
	"get": true, "list": true, "watch": true, "create": true, "update": true,
	"patch": true, "delete": true, "deletecollection": true, "use": true,
	"bind": true, "escalate": true, "impersonate": true, "approve": true,
	"sign": true, "*": true,
}

// Naming is what access calls and marks what it creates, so a product can
// put its own names on it.
type Naming struct {
	// ServiceAccount is the ServiceAccount (and ClusterRole and binding) on
	// a target whose settings name none.
	ServiceAccount string
	// HubSecretPrefix prefixes the cluster's name for the credential on the
	// hub, joined with "-" ("inrun" → "inrun-prod").
	HubSecretPrefix string
	// Labels go on everything access creates.
	Labels map[string]string
	// AnnotationPrefix is the domain of the annotations on the hub credential
	// (issued-at, expires-at, service-account).
	AnnotationPrefix string
}

// DefaultNaming is the Naming used when Options sets none.
var DefaultNaming = Naming{
	ServiceAccount:  "cluster-access",
	HubSecretPrefix: "cluster-access",
	Labels: map[string]string{
		"app.kubernetes.io/managed-by": "intentd-access",
		"intentd.io/role":              "cluster-access",
	},
	AnnotationPrefix: "intentd.io",
}

func (n Naming) orDefault() Naming {
	if n.ServiceAccount == "" && n.HubSecretPrefix == "" && n.Labels == nil && n.AnnotationPrefix == "" {
		return DefaultNaming
	}
	if n.AnnotationPrefix == "" {
		n.AnnotationPrefix = DefaultNaming.AnnotationPrefix
	}
	return n
}

// HubSecretName returns the credential Secret on the hub for cluster name.
func (n Naming) HubSecretName(name string) string { return withDash(n.HubSecretPrefix) + name }

// TokenSecretName returns a long token's Secret on the target.
func TokenSecretName(serviceAccount string) string { return serviceAccount + "-token" }

// TokenRoleName returns the Role (and binding) that lets a short token's
// ServiceAccount request its own next token.
func TokenRoleName(serviceAccount string) string { return serviceAccount + "-token-request" }

func (n Naming) annotation(key string) string { return n.AnnotationPrefix + "/" + key }

// withDash ends a non-empty prefix with "-".
func withDash(prefix string) string {
	if prefix == "" || strings.HasSuffix(prefix, "-") {
		return prefix
	}
	return prefix + "-"
}
