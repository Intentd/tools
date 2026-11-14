package kube_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/intentd/tools/kube"
	"github.com/intentd/tools/kubeclient/kubefake"
)

// WriteSecretKeys creates the Secret with its keys, type and labels; a later
// write updates the keys it names and keeps the others.
func TestWriteSecretKeys(t *testing.T) {
	ctx := context.Background()
	cs := kubefake.NewClientset()

	require.NoError(t, kube.WriteSecretKeys(ctx, cs, "data", "postgres-shop",
		map[string]string{"username": "shop", "password": "one"},
		kube.WithSecretType(corev1.SecretTypeBasicAuth), kube.WithSecretLabels(map[string]string{"cnpg.io/reload": "true"})))
	s, err := cs.CoreV1().Secrets("data").Get(ctx, "postgres-shop", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, corev1.SecretTypeBasicAuth, s.Type)
	assert.Equal(t, "true", s.Labels["cnpg.io/reload"])
	assert.Equal(t, "one", string(s.Data["password"]))

	require.NoError(t, kube.WriteSecretKeys(ctx, cs, "data", "postgres-shop", map[string]string{"password": "two", "extra": "x"}))
	s, err = cs.CoreV1().Secrets("data").Get(ctx, "postgres-shop", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"username": "shop", "password": "two", "extra": "x"}, map[string]string{
		"username": string(s.Data["username"]), "password": string(s.Data["password"]), "extra": string(s.Data["extra"]),
	})
	assert.Equal(t, corev1.SecretTypeBasicAuth, s.Type, "an update keeps the type")

	exists, err := kube.SecretExists(ctx, cs, "data", "postgres-shop")
	require.NoError(t, err)
	assert.True(t, exists)
}
