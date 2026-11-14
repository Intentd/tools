package cluster

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/intentd/tools/structured"
)

// Healthchecks is an image's health check: one check for the probes its as
// names (the flat form), or one check per probe (the named form), not both.
//
//	healthcheck:                      # flat: readiness by default
//	  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
//	  as: [startup, readiness]
//
//	healthcheck:                      # named
//	  readiness: { test: ["CMD", "curl", "-f", "http://localhost:8080/ready"], interval: 10s }
//	  liveness:  { test: ["CMD", "curl", "-f", "http://localhost:8080/live"], retries: 3 }
type Healthchecks struct {
	// Flat form: one check applied to the probes in As.
	*Healthcheck `yaml:",inline"`

	// Named form: one check per probe.
	Readiness *Healthcheck `yaml:"readiness,omitempty"`
	Liveness  *Healthcheck `yaml:"liveness,omitempty"`
	Startup   *Healthcheck `yaml:"startup,omitempty"`
}

// Healthcheck is one check, written as Docker Compose writes it, and the
// Kubernetes probes it becomes.
//
//	healthcheck:
//	  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
//	  interval: 30s
//	  timeout: 10s
//	  retries: 3
//	  as: [readiness]        # startup | readiness | liveness
type Healthcheck struct {
	// Test is the check, as Compose writes it: ["CMD", ...] or
	// ["CMD-SHELL", "..."]. A CMD form that is a single HTTP GET is
	// translated to httpGet; anything else is translated to exec.
	Test []string `yaml:"test"`

	// Interval, Timeout, Retries and StartPeriod are Compose's fields.
	Interval    string `yaml:"interval,omitempty"`     // → periodSeconds
	Timeout     string `yaml:"timeout,omitempty"`      // → timeoutSeconds
	Retries     int    `yaml:"retries,omitempty"`      // → failureThreshold
	StartPeriod string `yaml:"start_period,omitempty"` // → initialDelaySeconds

	// As names the probes this check becomes. Default: [readiness].
	As Probes `yaml:"as,omitempty"`
}

// Probes is the set of Kubernetes probes a healthcheck becomes.
type Probes []Probe

// Probe is one of startup, readiness or liveness.
type Probe string

const (
	ProbeStartup   Probe = "startup"
	ProbeReadiness Probe = "readiness"
	ProbeLiveness  Probe = "liveness"
)

// UnmarshalYAML reads the flat form (test, as) or the named form
// (readiness, liveness, startup), not both.
func (h *Healthchecks) UnmarshalYAML(value *yaml.Node) error {
	named := false
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			switch Probe(value.Content[i].Value) {
			case ProbeReadiness, ProbeLiveness, ProbeStartup:
				named = true
			}
		}
	}
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	if named {
		var n struct {
			Readiness *Healthcheck `yaml:"readiness,omitempty"`
			Liveness  *Healthcheck `yaml:"liveness,omitempty"`
			Startup   *Healthcheck `yaml:"startup,omitempty"`
		}
		if err := structured.StrictUnmarshal(raw, &n); err != nil {
			return fmt.Errorf("healthcheck: a check is flat (test, as) or named per probe (readiness, liveness, startup), not both: %w", err)
		}
		*h = Healthchecks{Readiness: n.Readiness, Liveness: n.Liveness, Startup: n.Startup}
		return nil
	}
	var flat Healthcheck
	if err := structured.StrictUnmarshal(raw, &flat); err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	*h = Healthchecks{Healthcheck: &flat}
	return nil
}

// For returns the check for probe kind: the named one, or the flat one when
// its as names kind (readiness by default); nil when there is none.
func (h *Healthchecks) For(kind Probe) *Healthcheck {
	if h == nil {
		return nil
	}
	if c := h.Healthcheck; c != nil {
		as := c.As
		if len(as) == 0 {
			as = Probes{ProbeReadiness}
		}
		if slices.Contains(as, kind) {
			return c
		}
		return nil
	}
	switch kind {
	case ProbeReadiness:
		return h.Readiness
	case ProbeLiveness:
		return h.Liveness
	case ProbeStartup:
		return h.Startup
	}
	return nil
}

// Validate reports a check without test, a test that isn't CMD, CMD-SHELL
// or NONE, a duration that doesn't parse, and a probe name that isn't one.
func (h *Healthchecks) Validate() error {
	if h == nil {
		return nil
	}
	for _, c := range []struct {
		name  string
		check *Healthcheck
	}{{"healthcheck", h.Healthcheck}, {"readiness", h.Readiness}, {"liveness", h.Liveness}, {"startup", h.Startup}} {
		if c.check == nil {
			continue
		}
		if err := c.check.validate(); err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
	}
	return nil
}

func (c *Healthcheck) validate() error {
	if len(c.Test) == 0 {
		return fmt.Errorf(`test is required: ["CMD", …], ["CMD-SHELL", "…"] or ["NONE"]`)
	}
	switch c.Test[0] {
	case "CMD":
		if len(c.Test) < 2 {
			return fmt.Errorf("test: CMD needs a command")
		}
	case "CMD-SHELL":
		if len(c.Test) != 2 {
			return fmt.Errorf("test: CMD-SHELL takes one command line")
		}
	case "NONE":
	default:
		return fmt.Errorf("test: start with CMD, CMD-SHELL or NONE, as Compose does (got %q)", c.Test[0])
	}
	for _, d := range []struct{ field, value string }{{"interval", c.Interval}, {"timeout", c.Timeout}, {"start_period", c.StartPeriod}} {
		if d.value == "" {
			continue
		}
		if _, err := time.ParseDuration(d.value); err != nil {
			return fmt.Errorf("%s %q: %w", d.field, d.value, err)
		}
	}
	for _, p := range c.As {
		if p != ProbeReadiness && p != ProbeLiveness && p != ProbeStartup {
			return fmt.Errorf("as: %q is not a probe (startup, readiness, liveness)", p)
		}
	}
	return nil
}

// probe is the Kubernetes probe of kind for d's container; nil without a
// check for it, or with test NONE.
func (d *ImageDeploy) probe(kind Probe) *corev1.Probe {
	c := d.Healthcheck.For(kind)
	if c == nil || len(c.Test) == 0 || c.Test[0] == "NONE" {
		return nil
	}
	p := &corev1.Probe{ProbeHandler: handler(c.Test)}
	if v := seconds(c.Interval); v > 0 {
		p.PeriodSeconds = v
	}
	if v := seconds(c.Timeout); v > 0 {
		p.TimeoutSeconds = v
	}
	if v := seconds(c.StartPeriod); v > 0 {
		p.InitialDelaySeconds = v
	}
	if c.Retries > 0 {
		p.FailureThreshold = int32(c.Retries)
	}
	return p
}

// handler is test as a probe handler: a CMD that is one HTTP GET to the
// container (curl or wget on localhost) becomes httpGet, so the check works
// on an image without curl; CMD-SHELL runs through sh -c; any other CMD
// runs as it is.
func handler(test []string) corev1.ProbeHandler {
	if test[0] == "CMD-SHELL" {
		return corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"sh", "-c", test[1]}}}
	}
	args := test[1:]
	if get := httpGet(args); get != nil {
		return corev1.ProbeHandler{HTTPGet: get}
	}
	return corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: args}}
}

// httpGet is args as an HTTP GET when they are curl or wget asking one
// localhost URL; nil otherwise.
func httpGet(args []string) *corev1.HTTPGetAction {
	if len(args) == 0 || (args[0] != "curl" && args[0] != "wget") {
		return nil
	}
	var target string
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "http://") {
			if target != "" {
				return nil
			}
			target = a
		}
	}
	if target == "" {
		return nil
	}
	u, err := url.Parse(target)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return nil
	}
	port := 80
	if u.Port() != "" {
		if port, err = strconv.Atoi(u.Port()); err != nil {
			return nil
		}
	}
	return &corev1.HTTPGetAction{Path: u.RequestURI(), Port: intstr.FromInt32(int32(port)), Scheme: corev1.URISchemeHTTP}
}

// seconds is a duration as whole seconds, rounded up; 0 when unset or not a
// duration (validation reports that).
func seconds(d string) int32 {
	if d == "" {
		return 0
	}
	v, err := time.ParseDuration(d)
	if err != nil {
		return 0
	}
	return int32(math.Ceil(v.Seconds()))
}
