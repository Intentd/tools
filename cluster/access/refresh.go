package access

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// RefreshOptions says how to find and renew a credential.
type RefreshOptions struct {
	// Naming is what the credential was granted with; the zero value is
	// DefaultNaming.
	Naming Naming
	// Format is how the credential is kept on the hub; nil is the format
	// recorded on it.
	Format HubFormat
	// Force renews even when the token is not yet half way through its life.
	Force bool

	// target and now reach the target and tell the time; tests replace them.
	target func(*rest.Config) (kubernetes.Interface, error)
	now    func() time.Time
}

// Refreshed is what Refresh did.
type Refreshed struct {
	// Renewed is true when a new token was written.
	Renewed bool
	// ExpiresAt is when the token on the hub now expires; zero for a long one.
	ExpiresAt time.Time
}

// Refresh renews the short token of the credential namespace/name on hub
// once it is half way through its life: it asks the target, with the token
// itself, for the next one (Grant allowed the ServiceAccount to), and writes
// it back. A long token is left as it is. If the token has already expired,
// only a new Grant can restore the access.
func Refresh(ctx context.Context, hub kubernetes.Interface, namespace, name string, opts RefreshOptions) (Refreshed, error) {
	opts.Naming = opts.Naming.orDefault()
	if opts.Format == nil {
		f, err := FormatOf(ctx, hub, namespace, name, opts.Naming)
		if err != nil {
			return Refreshed{}, err
		}
		opts.Format = f
	}
	if opts.target == nil {
		opts.target = func(cfg *rest.Config) (kubernetes.Interface, error) { return kubernetes.NewForConfig(cfg) }
	}
	if opts.now == nil {
		opts.now = time.Now
	}

	c, err := opts.Format.Read(ctx, hub, namespace, name, opts.Naming)
	if err != nil {
		return Refreshed{}, err
	}
	if !c.Short() {
		return Refreshed{}, nil
	}
	now := opts.now()
	if !now.Before(c.ExpiresAt) {
		return Refreshed{ExpiresAt: c.ExpiresAt}, fmt.Errorf(
			"credential %s/%s: token expired at %s; grant the access again", namespace, name, c.ExpiresAt.UTC().Format(time.RFC3339))
	}
	ttl := c.ExpiresAt.Sub(c.IssuedAt)
	if !opts.Force && now.Before(c.IssuedAt.Add(ttl/2)) {
		return Refreshed{ExpiresAt: c.ExpiresAt}, nil
	}
	if c.ServiceAccount == "" || c.Namespace == "" || c.Endpoint == "" {
		return Refreshed{}, fmt.Errorf("credential %s/%s: no service account or endpoint recorded; grant the access again", namespace, name)
	}

	cs, err := opts.target(&rest.Config{
		Host: c.Endpoint, BearerToken: c.Token,
		TLSClientConfig: rest.TLSClientConfig{CAData: c.CA},
	})
	if err != nil {
		return Refreshed{}, fmt.Errorf("connecting to %s: %w", c.Endpoint, err)
	}
	token, expires, err := requestToken(ctx, cs, c.Namespace, c.ServiceAccount, ttl, now)
	if err != nil {
		return Refreshed{}, err
	}
	c.Token, c.IssuedAt, c.ExpiresAt = token, now, expires
	if err := opts.Format.Write(ctx, hub, namespace, name, c, opts.Naming, func(string) {}); err != nil {
		return Refreshed{}, err
	}
	return Refreshed{Renewed: true, ExpiresAt: expires}, nil
}
