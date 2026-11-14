package kube

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/intentd/tools/duration"
	"github.com/intentd/tools/logger"
)

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
