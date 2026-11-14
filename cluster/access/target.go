package access

import (
	"context"
	"fmt"
	"strings"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// target creates and removes the access objects on one target cluster.
type target struct {
	cs     kubernetes.Interface
	e      Effective
	labels map[string]string
	dryRun bool
	log    func(string)
}

func (t target) meta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: t.labels}
}

// do logs what is about to be done, or would be under DryRun; it reports whether to
// go ahead.
func (t target) do(what string) bool {
	if t.dryRun {
		t.log("dry-run: " + what)
		return false
	}
	return true
}

func (t target) ensureServiceAccount(ctx context.Context) error {
	what := fmt.Sprintf("ServiceAccount %s/%s", t.e.Namespace, t.e.ServiceAccount)
	if !t.do(what) {
		return nil
	}
	sa := &corev1.ServiceAccount{ObjectMeta: t.meta(t.e.ServiceAccount, t.e.Namespace)}
	_, err := t.cs.CoreV1().ServiceAccounts(t.e.Namespace).Create(ctx, sa, metav1.CreateOptions{})
	switch {
	case k8serrors.IsAlreadyExists(err):
		t.log(what + ": already exists")
	case err != nil:
		return fmt.Errorf("creating ServiceAccount: %w", err)
	default:
		t.log(what + ": created")
	}
	return nil
}

// ensureClusterRole makes the ClusterRole with the rules, and its binding to
// the ServiceAccount; with no rules it removes both, so dropping the rules
// takes the access away.
func (t target) ensureClusterRole(ctx context.Context) error {
	name := t.e.ServiceAccount
	if len(t.e.Rules) == 0 {
		return t.removeClusterRole(ctx)
	}
	if !t.do("ClusterRole and ClusterRoleBinding " + name) {
		return nil
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: t.meta(name, ""), Rules: t.e.Rules}
	if _, err := t.cs.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{}); k8serrors.IsAlreadyExists(err) {
		existing, err := t.cs.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("fetching ClusterRole: %w", err)
		}
		existing.Rules = t.e.Rules
		if _, err := t.cs.RbacV1().ClusterRoles().Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating ClusterRole: %w", err)
		}
		t.log("ClusterRole " + name + ": rules updated")
	} else if err != nil {
		return fmt.Errorf("creating ClusterRole: %w", err)
	} else {
		t.log("ClusterRole " + name + ": created")
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: t.meta(name, ""),
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: t.e.ServiceAccount, Namespace: t.e.Namespace}},
	}
	switch _, err := t.cs.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); {
	case k8serrors.IsAlreadyExists(err):
		t.log("ClusterRoleBinding " + name + ": already exists")
	case err != nil:
		return fmt.Errorf("creating ClusterRoleBinding: %w", err)
	default:
		t.log("ClusterRoleBinding " + name + ": created")
	}
	return nil
}

func (t target) removeClusterRole(ctx context.Context) error {
	name := t.e.ServiceAccount
	if !t.do("remove ClusterRole and ClusterRoleBinding " + name) {
		return nil
	}
	if err := ignoreNotFound(t.cs.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("deleting ClusterRoleBinding: %w", err)
	}
	if err := ignoreNotFound(t.cs.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("deleting ClusterRole: %w", err)
	}
	return nil
}

// ensureTokenRole lets the ServiceAccount request tokens for itself only,
// so a short token can renew itself.
func (t target) ensureTokenRole(ctx context.Context) error {
	name := TokenRoleName(t.e.ServiceAccount)
	if !t.do(fmt.Sprintf("Role and RoleBinding %s/%s (request its own token)", t.e.Namespace, name)) {
		return nil
	}
	rules := []rbacv1.PolicyRule{{
		APIGroups: []string{""}, Resources: []string{"serviceaccounts/token"},
		ResourceNames: []string{t.e.ServiceAccount}, Verbs: []string{"create"},
	}}
	roles := t.cs.RbacV1().Roles(t.e.Namespace)
	if _, err := roles.Create(ctx, &rbacv1.Role{ObjectMeta: t.meta(name, t.e.Namespace), Rules: rules}, metav1.CreateOptions{}); k8serrors.IsAlreadyExists(err) {
		existing, err := roles.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("fetching token Role: %w", err)
		}
		existing.Rules = rules
		if _, err := roles.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating token Role: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("creating token Role: %w", err)
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: t.meta(name, t.e.Namespace),
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: t.e.ServiceAccount, Namespace: t.e.Namespace}},
	}
	if _, err := t.cs.RbacV1().RoleBindings(t.e.Namespace).Create(ctx, rb, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating token RoleBinding: %w", err)
	}
	t.log(fmt.Sprintf("Role %s/%s: ready", t.e.Namespace, name))
	return nil
}

func (t target) removeTokenRole(ctx context.Context) error {
	name := TokenRoleName(t.e.ServiceAccount)
	if err := ignoreNotFound(t.cs.RbacV1().RoleBindings(t.e.Namespace).Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("deleting token RoleBinding: %w", err)
	}
	if err := ignoreNotFound(t.cs.RbacV1().Roles(t.e.Namespace).Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("deleting token Role: %w", err)
	}
	return nil
}

// requestToken issues a short token through TokenRequest. The cluster may
// shorten the TTL, so the expiry is the one it returns.
func requestToken(ctx context.Context, cs kubernetes.Interface, namespace, sa string, ttl time.Duration, now time.Time) (string, time.Time, error) {
	secs := int64(ttl.Seconds())
	tr, err := cs.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, sa,
		&authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &secs}}, metav1.CreateOptions{})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("requesting a token for %s/%s: %w", namespace, sa, err)
	}
	expires := tr.Status.ExpirationTimestamp.Time
	if expires.IsZero() {
		expires = now.Add(ttl)
	}
	return tr.Status.Token, expires, nil
}

// longToken returns the token of the ServiceAccount's token Secret, making
// the Secret first if needed. With rotate, the Secret is replaced, which
// invalidates the old token.
func (t target) longToken(ctx context.Context, rotate bool) (string, error) {
	name, ns := TokenSecretName(t.e.ServiceAccount), t.e.Namespace
	secrets := t.cs.CoreV1().Secrets(ns)
	if rotate {
		if !t.do(fmt.Sprintf("rotate Secret %s/%s (token)", ns, name)) {
			return "", nil
		}
		if err := ignoreNotFound(secrets.Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
			return "", fmt.Errorf("deleting token Secret to rotate: %w", err)
		}
	} else {
		if !t.do(fmt.Sprintf("Secret %s/%s (token)", ns, name)) {
			return "", nil
		}
		if existing, err := secrets.Get(ctx, name, metav1.GetOptions{}); err == nil {
			if token := strings.TrimSpace(string(existing.Data["token"])); token != "" {
				t.log(fmt.Sprintf("Secret %s/%s: already exists — reusing token", ns, name))
				return token, nil
			}
		}
	}
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: ns, Labels: t.labels,
			Annotations: map[string]string{corev1.ServiceAccountNameKey: t.e.ServiceAccount},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	if _, err := secrets.Create(ctx, s, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("creating token Secret: %w", err)
	}
	token, err := waitForToken(ctx, t.cs, ns, name, t.log)
	if err != nil {
		return "", err
	}
	t.log(fmt.Sprintf("Secret %s/%s: token ready", ns, name))
	return token, nil
}

// removeLongToken deletes the token Secret, which invalidates its token.
func (t target) removeLongToken(ctx context.Context) error {
	name := TokenSecretName(t.e.ServiceAccount)
	return ignoreNotFound(t.cs.CoreV1().Secrets(t.e.Namespace).Delete(ctx, name, metav1.DeleteOptions{}))
}

// revoke removes everything Grant made on the target. Deleting the
// ServiceAccount invalidates every token issued to it.
func (t target) revoke(ctx context.Context) error {
	if !t.do(fmt.Sprintf("revoke ServiceAccount %s/%s, its roles and tokens", t.e.Namespace, t.e.ServiceAccount)) {
		return nil
	}
	if err := t.removeClusterRole(ctx); err != nil {
		return err
	}
	if err := t.removeTokenRole(ctx); err != nil {
		return err
	}
	if err := t.removeLongToken(ctx); err != nil {
		return fmt.Errorf("deleting token Secret: %w", err)
	}
	if err := ignoreNotFound(t.cs.CoreV1().ServiceAccounts(t.e.Namespace).Delete(ctx, t.e.ServiceAccount, metav1.DeleteOptions{})); err != nil {
		return fmt.Errorf("deleting ServiceAccount: %w", err)
	}
	t.log(fmt.Sprintf("ServiceAccount %s/%s: revoked", t.e.Namespace, t.e.ServiceAccount))
	return nil
}

func waitForToken(ctx context.Context, cs kubernetes.Interface, ns, name string, log func(string)) (string, error) {
	for range 10 {
		s, err := cs.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("reading token Secret: %w", err)
		}
		if token := strings.TrimSpace(string(s.Data["token"])); token != "" {
			return token, nil
		}
		log("waiting for the token controller to fill the token...")
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return "", fmt.Errorf("token not filled after 30s — check the token controller is running")
}

func ignoreNotFound(err error) error {
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}
