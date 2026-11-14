package access

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Credential is what the hub needs to reach a target, and what Refresh needs
// to renew it.
type Credential struct {
	// Cluster is the target's name in the Config.
	Cluster string
	// Endpoint is the target's API server URL.
	Endpoint string
	// Token is the ServiceAccount's bearer token.
	Token string
	// CA is the target's CA bundle (PEM); empty when the kubeconfig had none.
	CA []byte
	// ServiceAccount and Namespace name the token's ServiceAccount on the
	// target.
	ServiceAccount string
	Namespace      string
	// IssuedAt and ExpiresAt bound a short token; zero for a long one.
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Short reports whether c holds a token that expires.
func (c Credential) Short() bool { return !c.ExpiresAt.IsZero() }

// FormatName names a HubFormat in a Config.
type FormatName string

const (
	// FormatSecret is SecretFormat, the default.
	FormatSecret FormatName = "secret"
	// FormatArgoCD is ArgoCDFormat.
	FormatArgoCD FormatName = "argocd"
	// FormatKubeconfig is a kubeconfig under KubeconfigKey.
	FormatKubeconfig FormatName = "kubeconfig"
	// FormatFlux is a kubeconfig under FluxKey, where Flux reads it.
	FormatFlux FormatName = "flux"
)

// Formats is every HubFormat by name.
var Formats = map[FormatName]HubFormat{
	FormatSecret:     SecretFormat{},
	FormatArgoCD:     ArgoCDFormat{},
	FormatKubeconfig: KubeconfigFormat{Name: FormatKubeconfig, Key: KubeconfigKey},
	FormatFlux:       KubeconfigFormat{Name: FormatFlux, Key: FluxKey},
}

// IsValid reports whether f names one of Formats.
func (f FormatName) IsValid() bool { _, ok := Formats[f]; return ok }

// Credential Secret keys of SecretFormat.
const (
	TokenKey  = "token"
	CAKey     = "ca.crt"
	ServerKey = "server"
)

// HubFormat is how a credential is kept on the hub, in the form the hub's
// consumer reads. Both formats keep it in one Secret, so Revoke deletes it
// the same way.
type HubFormat interface {
	Write(ctx context.Context, hub kubernetes.Interface, namespace, name string, c Credential, n Naming, log func(string)) error
	Read(ctx context.Context, hub kubernetes.Interface, namespace, name string, n Naming) (Credential, error)
}

// SecretFormat keeps the token under TokenKey, the CA under CAKey and the
// endpoint under ServerKey in an Opaque Secret. It is the default format.
type SecretFormat struct{}

func (SecretFormat) Write(ctx context.Context, hub kubernetes.Interface, namespace, name string, c Credential, n Naming, log func(string)) error {
	data := map[string][]byte{TokenKey: []byte(c.Token), ServerKey: []byte(c.Endpoint)}
	if len(c.CA) > 0 {
		data[CAKey] = c.CA
	}
	return upsertSecret(ctx, hub, secret(FormatSecret, namespace, name, c, n, nil, data), log)
}

func (SecretFormat) Read(ctx context.Context, hub kubernetes.Interface, namespace, name string, n Naming) (Credential, error) {
	s, err := hub.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Credential{}, fmt.Errorf("reading credential %s/%s: %w", namespace, name, err)
	}
	c := fromAnnotations(s, n)
	c.Token, c.Endpoint, c.CA = string(s.Data[TokenKey]), string(s.Data[ServerKey]), s.Data[CAKey]
	return c, nil
}

// ArgoCDSecretType is the label value that makes Argo CD read a Secret as a
// cluster.
const ArgoCDSecretType = "cluster"

// ArgoCDFormat keeps the credential as an Argo CD declarative cluster
// Secret: labelled argocd.argoproj.io/secret-type: cluster, with the
// cluster's name, server and a config holding the bearer token and CA. The
// namespace must be Argo CD's.
type ArgoCDFormat struct{}

type argoConfig struct {
	BearerToken     string `json:"bearerToken"`
	TLSClientConfig struct {
		Insecure bool   `json:"insecure"`
		CAData   string `json:"caData,omitempty"`
	} `json:"tlsClientConfig"`
}

func (ArgoCDFormat) Write(ctx context.Context, hub kubernetes.Interface, namespace, name string, c Credential, n Naming, log func(string)) error {
	var cfg argoConfig
	cfg.BearerToken = c.Token
	if len(c.CA) > 0 {
		cfg.TLSClientConfig.CAData = base64.StdEncoding.EncodeToString(c.CA)
	}
	config, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding Argo CD cluster config: %w", err)
	}
	return upsertSecret(ctx, hub, secret(FormatArgoCD, namespace, name, c, n,
		map[string]string{"argocd.argoproj.io/secret-type": ArgoCDSecretType},
		map[string][]byte{"name": []byte(c.Cluster), "server": []byte(c.Endpoint), "config": config},
	), log)
}

func (ArgoCDFormat) Read(ctx context.Context, hub kubernetes.Interface, namespace, name string, n Naming) (Credential, error) {
	s, err := hub.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Credential{}, fmt.Errorf("reading credential %s/%s: %w", namespace, name, err)
	}
	var cfg argoConfig
	if err := json.Unmarshal(s.Data["config"], &cfg); err != nil {
		return Credential{}, fmt.Errorf("credential %s/%s: config: %w", namespace, name, err)
	}
	c := fromAnnotations(s, n)
	c.Token, c.Endpoint = cfg.BearerToken, string(s.Data["server"])
	if cfg.TLSClientConfig.CAData != "" {
		if c.CA, err = base64.StdEncoding.DecodeString(cfg.TLSClientConfig.CAData); err != nil {
			return Credential{}, fmt.Errorf("credential %s/%s: caData: %w", namespace, name, err)
		}
	}
	return c, nil
}

// KubeconfigFormat keeps the credential as a kubeconfig under Key that
// reaches the target as the ServiceAccount: the form Flux
// (spec.kubeConfig.secretRef), Cluster API and Inrun's gateway (secretRef)
// read. Name is what it is recorded as, so Refresh reads it back alike.
type KubeconfigFormat struct {
	Name FormatName
	Key  string
}

// Kubeconfig keys: the general one, and the one Flux reads by default.
const (
	KubeconfigKey = "kubeconfig"
	FluxKey       = "value"
)

func (f KubeconfigFormat) Write(ctx context.Context, hub kubernetes.Interface, namespace, name string, c Credential, n Naming, log func(string)) error {
	kc := clientcmdapi.NewConfig()
	kc.Clusters[c.Cluster] = &clientcmdapi.Cluster{Server: c.Endpoint, CertificateAuthorityData: c.CA}
	kc.AuthInfos[c.Cluster] = &clientcmdapi.AuthInfo{Token: c.Token}
	kc.Contexts[c.Cluster] = &clientcmdapi.Context{Cluster: c.Cluster, AuthInfo: c.Cluster}
	kc.CurrentContext = c.Cluster
	value, err := clientcmd.Write(*kc)
	if err != nil {
		return fmt.Errorf("encoding the kubeconfig: %w", err)
	}
	return upsertSecret(ctx, hub, secret(f.name(), namespace, name, c, n, nil, map[string][]byte{f.key(): value}), log)
}

func (f KubeconfigFormat) Read(ctx context.Context, hub kubernetes.Interface, namespace, name string, n Naming) (Credential, error) {
	s, err := hub.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Credential{}, fmt.Errorf("reading credential %s/%s: %w", namespace, name, err)
	}
	kc, err := clientcmd.Load(s.Data[f.key()])
	if err != nil {
		return Credential{}, fmt.Errorf("credential %s/%s: kubeconfig: %w", namespace, name, err)
	}
	c := fromAnnotations(s, n)
	if kctx := kc.Contexts[kc.CurrentContext]; kctx != nil {
		if cl := kc.Clusters[kctx.Cluster]; cl != nil {
			c.Endpoint, c.CA = cl.Server, cl.CertificateAuthorityData
		}
		if ai := kc.AuthInfos[kctx.AuthInfo]; ai != nil {
			c.Token = ai.Token
		}
	}
	return c, nil
}

func (f KubeconfigFormat) key() string {
	if f.Key == "" {
		return KubeconfigKey
	}
	return f.Key
}

func (f KubeconfigFormat) name() FormatName {
	if f.Name == "" {
		return FormatKubeconfig
	}
	return f.Name
}

// FormatOf returns the format the credential Secret namespace/name was
// written in, as recorded on it.
func FormatOf(ctx context.Context, hub kubernetes.Interface, namespace, name string, n Naming) (HubFormat, error) {
	s, err := hub.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading credential %s/%s: %w", namespace, name, err)
	}
	f := FormatName(s.Annotations[n.orDefault().annotation("format")])
	if f == "" {
		f = FormatSecret
	}
	format, ok := Formats[f]
	if !ok {
		return nil, fmt.Errorf("credential %s/%s: unknown format %q", namespace, name, f)
	}
	return format, nil
}

// secret is a credential Secret with n's labels, extra labels, and the
// annotations Refresh reads.
func secret(format FormatName, namespace, name string, c Credential, n Naming, extra map[string]string, data map[string][]byte) *corev1.Secret {
	labels := maps.Clone(n.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, extra)
	ann := map[string]string{
		n.annotation("format"):          string(format),
		n.annotation("cluster"):         c.Cluster,
		n.annotation("service-account"): c.Namespace + "/" + c.ServiceAccount,
	}
	if c.Short() {
		ann[n.annotation("issued-at")] = c.IssuedAt.UTC().Format(time.RFC3339)
		ann[n.annotation("expires-at")] = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels, Annotations: ann},
		Type:       corev1.SecretTypeOpaque,
		Data:       data,
	}
}

func fromAnnotations(s *corev1.Secret, n Naming) Credential {
	ann := s.Annotations
	c := Credential{Cluster: ann[n.annotation("cluster")]}
	c.Namespace, c.ServiceAccount, _ = strings.Cut(ann[n.annotation("service-account")], "/")
	c.IssuedAt, _ = time.Parse(time.RFC3339, ann[n.annotation("issued-at")])
	c.ExpiresAt, _ = time.Parse(time.RFC3339, ann[n.annotation("expires-at")])
	return c
}

// upsertSecret replaces the data, labels and annotations of the Secret
// there, or creates it, so a rerun refreshes the credential. It reads first,
// so a caller allowed only get and update (a refresher) can rewrite it.
func upsertSecret(ctx context.Context, cs kubernetes.Interface, s *corev1.Secret, log func(string)) error {
	existing, err := cs.CoreV1().Secrets(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	switch {
	case err == nil:
		existing.Data, existing.Labels, existing.Annotations = s.Data, s.Labels, s.Annotations
		if _, err := cs.CoreV1().Secrets(s.Namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating credential Secret: %w", err)
		}
		log(fmt.Sprintf("Secret %s/%s: updated", s.Namespace, s.Name))
		return nil
	case !k8serrors.IsNotFound(err):
		return fmt.Errorf("reading credential Secret: %w", err)
	}
	if _, err := cs.CoreV1().Secrets(s.Namespace).Create(ctx, s, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating credential Secret: %w", err)
	}
	log(fmt.Sprintf("Secret %s/%s: created", s.Namespace, s.Name))
	return nil
}
