package kube

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/intentd/tools/duration"
	"github.com/intentd/tools/logger"
)

// TLSSpec declares TLS certificate generation for a secret.
// When set, the secret is created as type kubernetes.io/tls with
// tls.crt, tls.key, and ca.crt fields.
type TLSSpec struct {
	// CommonName is the certificate's CN field.
	//   commonName: "{{ .metadata.name }}.{{ .metadata.namespace }}.svc"
	CommonName string `yaml:"commonName" json:"commonName"`

	// DNSNames are the Subject Alternative Names.
	// Template expressions supported per entry.
	DNSNames []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`

	// ValidFor is the certificate validity period.
	// Default: same as rotateAfter, or 1y if rotateAfter is not set.
	// Must be >= rotateAfter to avoid immediate expiry on rotation.
	ValidFor string `yaml:"validFor,omitempty" json:"validFor,omitempty"`

	// Organization is the cert's O field. Default: "inrun"
	Organization string `yaml:"organization,omitempty" json:"organization,omitempty"`
}

// SecretExists reports whether the Secret namespace/name exists: false when
// it is not found, an error when the API call fails. It reads from the API
// server's watch cache (ResourceVersion "0"), not etcd.
func SecretExists(ctx context.Context, cs kubernetes.Interface, namespace, name string) (bool, error) {
	_, err := cs.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{ResourceVersion: "0"})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking secret %s/%s: %w", namespace, name, err)
	}
	return true, nil
}

// ReadSecretKey returns the value at key in the Secret namespace/name, read
// from the API server's watch cache (ResourceVersion "0").
func ReadSecretKey(ctx context.Context, cs kubernetes.Interface, namespace, name, key string) (string, error) {
	secret, err := cs.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{ResourceVersion: "0"})
	if err != nil {
		return "", fmt.Errorf("reading secret %s/%s: %w", namespace, name, err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q", namespace, name, key)
	}
	return string(val), nil
}

// SecretOption sets how WriteSecretKeys creates a Secret.
type SecretOption func(*corev1.Secret)

// WithSecretType sets the type of a Secret WriteSecretKeys creates, such as
// kubernetes.io/basic-auth. Default: Opaque.
func WithSecretType(t corev1.SecretType) SecretOption {
	return func(s *corev1.Secret) { s.Type = t }
}

// WithSecretLabels adds labels to the Secret WriteSecretKeys writes.
func WithSecretLabels(labels map[string]string) SecretOption {
	return func(s *corev1.Secret) {
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		for k, v := range labels {
			s.Labels[k] = v
		}
	}
}

// WriteSecretKeys sets keys in the Secret namespace/name: it creates the
// Secret with them when it doesn't exist, and otherwise updates those keys,
// keeping the others. A write that conflicts with another is retried.
// Options apply to a Secret it creates; labels are added on update too.
func WriteSecretKeys(ctx context.Context, cs kubernetes.Interface, namespace, name string, keys map[string]string, opts ...SecretOption) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := cs.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Data: map[string][]byte{}}
			for _, o := range opts {
				o(s)
			}
			for k, v := range keys {
				s.Data[k] = []byte(v)
			}
			if _, err := cs.CoreV1().Secrets(namespace).Create(ctx, s, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("creating secret %s/%s: %w", namespace, name, err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("reading secret %s/%s: %w", namespace, name, err)
		}
		updated := existing.DeepCopy()
		labeled := &corev1.Secret{}
		for _, o := range opts {
			o(labeled)
		}
		for k, v := range labeled.Labels {
			if updated.Labels == nil {
				updated.Labels = map[string]string{}
			}
			updated.Labels[k] = v
		}
		if updated.Data == nil {
			updated.Data = map[string][]byte{}
		}
		for k, v := range keys {
			updated.Data[k] = []byte(v)
		}
		if _, err := cs.CoreV1().Secrets(namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating secret %s/%s: %w", namespace, name, err)
		}
		return nil
	})
}

// DeleteSecret deletes the Secret namespace/name; one already gone is not an error.
func DeleteSecret(ctx context.Context, cs kubernetes.Interface, namespace, name string) error {
	err := cs.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ── Rotation ───────────────────────────────────────────────────────────────

// RotationKeys are the annotations a generated Secret's rotation is kept in:
// when it was generated (RFC3339) and the rotateAfter it was generated with.
type RotationKeys struct {
	GeneratedAt string
	RotateAfter string
}

// DefaultRotationKeys are the rotation annotations used unless a caller passes its own.
var DefaultRotationKeys = RotationKeys{
	GeneratedAt: "intentd.io/generated-at",
	RotateAfter: "intentd.io/rotate-after",
}

// RotationOption changes how rotation reads and writes a Secret.
type RotationOption func(*RotationKeys)

// WithRotationKeys keeps rotation in the annotations generatedAt and
// rotateAfter instead of DefaultRotationKeys.
func WithRotationKeys(generatedAt, rotateAfter string) RotationOption {
	return func(k *RotationKeys) { k.GeneratedAt, k.RotateAfter = generatedAt, rotateAfter }
}

func rotationKeys(opts []RotationOption) RotationKeys {
	k := DefaultRotationKeys
	for _, o := range opts {
		o(&k)
	}
	return k
}

// GenerationAnnotations returns the annotations a Secret generated now
// carries: the time, and rotateAfter when set.
func GenerationAnnotations(rotateAfter string, opts ...RotationOption) map[string]string {
	k := rotationKeys(opts)
	annotations := map[string]string{k.GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	if rotateAfter != "" {
		annotations[k.RotateAfter] = rotateAfter
	}
	return annotations
}

// RotationDue reports whether a Secret generated at generatedAt (RFC3339)
// has passed rotateAfter. An empty or invalid rotateAfter never rotates; an
// unreadable generatedAt always does.
func RotationDue(generatedAt, rotateAfter string) bool {
	if rotateAfter == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, generatedAt)
	if err != nil {
		return true
	}
	threshold, err := duration.Parse(rotateAfter)
	if err != nil {
		return false
	}
	return time.Since(t) >= threshold
}

// SecretNeedsRotation reports whether the Secret namespace/name is due to be
// generated again (see RotationDue). It is false when rotateAfter is empty or
// the Secret doesn't exist yet. A Secret without the generated-at annotation,
// made elsewhere, is annotated now and its rotation clock starts from this
// call.
func SecretNeedsRotation(ctx context.Context, cs kubernetes.Interface, namespace, name, rotateAfter string, opts ...RotationOption) (bool, error) {
	if rotateAfter == "" {
		return false, nil
	}
	secret, err := cs.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{ResourceVersion: "0"})
	if apierrors.IsNotFound(err) {
		return false, nil // to be created, not rotated
	}
	if err != nil {
		return false, fmt.Errorf("checking secret %s/%s for rotation: %w", namespace, name, err)
	}

	k := rotationKeys(opts)
	generatedAt := secret.Annotations[k.GeneratedAt]
	if generatedAt == "" {
		logger.FromContext(ctx).Warn().
			Str("secret", name).
			Msg("secret missing generated-at annotation — annotating now, rotation clock starts")
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[k.GeneratedAt] = time.Now().UTC().Format(time.RFC3339)
		secret.Annotations[k.RotateAfter] = rotateAfter
		if _, err := cs.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("annotating secret %s/%s: %w", namespace, name, err)
		}
		return false, nil
	}
	return RotationDue(generatedAt, rotateAfter), nil
}
