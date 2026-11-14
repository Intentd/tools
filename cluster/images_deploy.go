package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/schemagen"
	"github.com/intentd/tools/structured"
	"github.com/intentd/tools/term"
	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// ImageDeploy configures a Kubernetes Deployment and Service for an image.
type ImageDeploy struct {
	// Name is the Deployment and Service name.
	// Defaults to the image name without its tag or digest.
	Name string `yaml:"name,omitempty"`

	// Namespace is the target namespace.
	// Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`

	// Ports are the container's ports, each with a Service port and a
	// forward to localhost (Port). At least one. TCP is used.
	Ports Ports `yaml:"ports"`

	// Healthcheck is the container's check, as Docker Compose writes it,
	// and the probes it becomes.
	Healthcheck *Healthchecks `yaml:"healthcheck,omitempty"`

	// Replicas is the desired number of Deployment replicas.
	// Defaults to 1.
	Replicas *int32 `yaml:"replicas,omitempty"`

	// Env are the container's environment variables, as Compose's
	// environment: the caller resolves their values before deploying.
	Env map[string]string `yaml:"env,omitempty"`

	// EnvFrom are Secrets the container loads whole, each key a variable,
	// in the Deployment's namespace.
	EnvFrom []string `yaml:"envFrom,omitempty"`

	// EnvFile are .env files, as Compose's env_file: their variables become
	// a Secret (<name>-env), loaded with envFrom; a later file wins.
	EnvFile EnvFiles `yaml:"envFile,omitempty"`

	// Init are containers run to completion, in order, before the app's
	// starts: Kubernetes' initContainers, in the same pod (its network, its
	// env), for what Compose does with a separate service and depends_on
	// (a migration, waiting for a dependency).
	Init []InitContainer `yaml:"init,omitempty"`

	// DeployManifests contains every Kubernetes resource produced by an image deployment.
	DeployManifests *ImageDeployManifests `yaml:"-" json:"-"`
}

// ImageDeployManifests contains every Kubernetes resource produced
// by an image deployment.
type ImageDeployManifests struct {
	Secrets    []*corev1.Secret
	Deployment *appsv1.Deployment
	Service    *corev1.Service
}

// Manifests builds all Kubernetes resources produced by this deployment.
// It does not interact with the Kubernetes cluster.
func (d *ImageDeploy) Manifests(image string) (*ImageDeployManifests, error) {
	if d == nil {
		return nil, nil
	}

	manifests := &ImageDeployManifests{
		Deployment: d.DeploymentSpec(image),
	}

	// No exposed port, no Service.
	service := d.ServiceSpec(image)
	if len(service.Spec.Ports) > 0 {
		manifests.Service = service
	}

	secrets, err := d.envFileSecrets(image)
	if err != nil {
		return nil, fmt.Errorf("build deployment Secrets: %w", err)
	}

	manifests.Secrets = secrets

	return manifests, nil
}

// InitContainer is one entry in deploy.init.
//
//	init:
//	  - command: ["./migrate", "up"]                 # the app's image
//	  - name: wait-db
//	    image: busybox:1.37
//	    command: ["sh", "-c", "until nc -z postgres-rw.data 5432; do sleep 1; done"]
type InitContainer struct {
	// Name is the container's name. Defaults to init-<index>.
	Name string `yaml:"name,omitempty"`
	// Image defaults to the app's image. It is a reference: an image built
	// by an images entry, or one the cluster pulls; never built here.
	Image string `yaml:"image,omitempty"`
	// Command must exit: an init that runs an app's entrypoint never
	// finishes, and the pod never starts.
	Command []string `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
	// Env, EnvFrom and EnvFile are as the app's container has them; an
	// init's EnvFile is a Secret of its own (<name>-<init>-env).
	Env     map[string]string `yaml:"env,omitempty"`
	EnvFrom []string          `yaml:"envFrom,omitempty"`
	EnvFile EnvFiles          `yaml:"envFile,omitempty"`
}

// EnvFiles are .env file paths: one, or a list.
type EnvFiles []string

// UnmarshalYAML accepts one path as well as a list.
func (e *EnvFiles) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*e = EnvFiles{value.Value}
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*e = list
	return nil
}

// JSONSchemaExtend accepts one path beside the list, as UnmarshalYAML does.
func (EnvFiles) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a .env file"))
}

// ResolvePaths returns e with relative paths resolved from dir.
func (e EnvFiles) ResolvePaths(dir string) EnvFiles {
	if len(e) == 0 {
		return e
	}
	out := make(EnvFiles, len(e))
	for i, p := range e {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		out[i] = p
	}
	return out
}

// Validate reports an init without a command.
func (c InitContainer) Validate() error {
	if len(c.Command) == 0 {
		return fmt.Errorf("init %s: set command: an init runs a command and exits", c.Name)
	}
	return nil
}

// Port is one entry in deploy.ports. It is written either as a scalar —
// the container port, forwarded to the same localhost port — or as a
// struct with the host and container ports and the forward/expose flags.
//
// Short forms, as in Docker Compose:
//
//	ports:
//	  - 8080            # container 8080, forward localhost:8080
//	  - 8081:8080       # container 8080, forward localhost:8081
//
// Long form:
//
//	ports:
//	  - port: 9090
//	    forward: false  # Service yes, localhost no
type Port struct {
	// Port is the container port. The Service targets it, and the
	// Deployment's containerPort is it.
	Port int32 `yaml:"port"`

	// Host is the localhost port the forward listens on. Defaults to Port
	// when omitted.
	Host int32 `yaml:"host,omitempty"`

	// Forward reports whether jig forwards this port to localhost. Default
	// true. Set false for a port reachable in-cluster but not needed on the
	// developer's machine.
	Forward *bool `yaml:"forward,omitempty"`

	// Expose reports whether the Service has this port. Default true; false
	// is a container port only: no Service port, no forward.
	Expose *bool `yaml:"expose,omitempty"`
}
type Ports []Port

// UnmarshalYAML accepts the scalar form (8080) and the host:container
// form (8081:8080) as well as the struct. Host defaults to Port.
func (p *Port) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		host, container, short := strings.Cut(value.Value, ":")
		if !short {
			container = host
		}
		c, err := portNumber(container)
		if err != nil {
			return fmt.Errorf("port %q: %w", value.Value, err)
		}
		h, err := portNumber(host)
		if err != nil {
			return fmt.Errorf("port %q: %w", value.Value, err)
		}
		*p = Port{Port: c, Host: h}
		return nil
	}
	type plain Port
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	if err := structured.StrictUnmarshal(raw, (*plain)(p)); err != nil {
		return err
	}
	if p.Host == 0 {
		p.Host = p.Port
	}
	return nil
}

// JSONSchemaExtend accepts the scalar and host:container forms beside the
// struct, as UnmarshalYAML does.
func (Port) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s,
		&jsonschema.Schema{Type: "integer", Description: "The container port, forwarded on the same localhost port"},
		&jsonschema.Schema{Type: "string", Pattern: `^\d+(:\d+)?$`, Description: "host:container, e.g. 8081:8080"},
	)
}

// portNumber parses a port: a number from 1 to 65535.
func portNumber(s string) (int32, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%d is not between 1 and 65535", n)
	}
	return int32(n), nil
}

// Exposed reports whether the Service has the port.
func (p Port) Exposed() bool { return p.Expose == nil || *p.Expose }

// Forwarded reports whether jig forwards the port to localhost: a port that
// isn't exposed isn't forwarded either.
func (p Port) Forwarded() bool { return p.Exposed() && (p.Forward == nil || *p.Forward) }

// Validate reports a port outside 1–65535.
func (p Port) Validate() error {
	for _, n := range []int32{p.Port, p.Host} {
		if n < 1 || n > 65535 {
			return fmt.Errorf("port %d is not between 1 and 65535", n)
		}
	}
	return nil
}

// name is the port's name on the container and the Service: unique per
// port, a valid port name.
func (p Port) name() string { return fmt.Sprintf("p%d", p.Port) }

const DefaultDeployFieldManager = "intend-tools"

// DeployOptions configures server-side apply for image deployments.
type DeployOptions struct {
	// FieldManager identifies the manager owning the applied fields.
	FieldManager string

	// Force allows taking ownership of fields managed by another manager.
	Force bool
}

// DeployOption configures a deployment operation.
type DeployOption func(*DeployOptions)

// WithFieldManager sets the server-side apply field manager.
func WithFieldManager(name string) DeployOption {
	return func(o *DeployOptions) {
		o.FieldManager = name
	}
}

// WithForceOwnership allows taking ownership of fields managed by
// another server-side apply field manager.
func WithForceOwnership() DeployOption {
	return func(o *DeployOptions) {
		o.Force = true
	}
}

func resolveDeployOptions(opts []DeployOption) DeployOptions {
	o := DeployOptions{
		FieldManager: DefaultDeployFieldManager,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	if o.FieldManager == "" {
		o.FieldManager = DefaultDeployFieldManager
	}

	return o
}

// deploymentName returns the configured deployment name or derives it
// from the image reference.
func (d *ImageDeploy) deploymentName(image string) string {
	if d.Name != "" {
		return d.Name
	}

	return (&Image{Image: image}).DefaultDeployName()
}

// namespace returns the configured namespace or "default".
func (d *ImageDeploy) namespace() string {
	if d.Namespace == "" {
		return "default"
	}

	return d.Namespace
}

// replicas returns the configured replica count or 1.
func (d *ImageDeploy) replicas() int32 {
	if d.Replicas == nil {
		return 1
	}

	return *d.Replicas
}

// DeploymentSpec builds the Kubernetes Deployment for this image.
func (d *ImageDeploy) DeploymentSpec(image string) *appsv1.Deployment {
	name := d.deploymentName(image)

	labels := map[string]string{
		"app.kubernetes.io/name": name,
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.namespace(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{d.replicas()}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					InitContainers: d.initContainers(image),
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             envVars(d.Env),
							EnvFrom:         envFromSecrets(withEnvFile(d.EnvFrom, d.EnvFile, envFileSecret(name))),
							Ports:           d.containerPorts(),
							StartupProbe:    d.probe(ProbeStartup),
							ReadinessProbe:  d.probe(ProbeReadiness),
							LivenessProbe:   d.probe(ProbeLiveness),
						},
					},
				},
			},
		},
	}
}

// initContainers are deploy.init's containers, in order: the app's image
// when an entry names none.
func (d *ImageDeploy) initContainers(image string) []corev1.Container {
	if len(d.Init) == 0 {
		return nil
	}
	out := make([]corev1.Container, len(d.Init))
	for i, c := range d.Init {
		name := initName(i, c)
		img := c.Image
		if img == "" {
			img = image
		}
		out[i] = corev1.Container{
			Name: name, Image: img, ImagePullPolicy: corev1.PullIfNotPresent,
			Command: c.Command, Args: c.Args,
			Env:     envVars(c.Env),
			EnvFrom: envFromSecrets(withEnvFile(c.EnvFrom, c.EnvFile, envFileSecret(d.deploymentName(image)+"-"+name))),
		}
	}
	return out
}

// initName is an init container's name: its own, or init-<index>.
func initName(i int, c InitContainer) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("init-%d", i)
}

// envFileSecret is the Secret a container's env files become.
func envFileSecret(owner string) string { return owner + "-env" }

// withEnvFile is envFrom with the env-file Secret added when there are
// env files.
func withEnvFile(envFrom []string, files EnvFiles, secret string) []string {
	if len(files) == 0 {
		return envFrom
	}
	return append(append([]string(nil), envFrom...), secret)
}

// envFileSecrets are the Secrets d's env files become: the app's and each
// init's, read and merged, a later file winning.
func (d *ImageDeploy) envFileSecrets(image string) ([]*corev1.Secret, error) {
	name := d.deploymentName(image)
	var out []*corev1.Secret
	add := func(secret string, files EnvFiles) error {
		if len(files) == 0 {
			return nil
		}
		data := map[string][]byte{}
		for _, f := range files {
			raw, err := fileio.ReadLocal(f)
			if err != nil {
				return fmt.Errorf("envFile %s: %w", f, err)
			}
			vars, err := env.ParseDotEnv(raw)
			if err != nil {
				return fmt.Errorf("envFile %s: %w", f, err)
			}
			for k, v := range vars {
				data[k] = []byte(v)
			}
		}
		out = append(out, &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: secret, Namespace: d.namespace()},
			Type:       corev1.SecretTypeOpaque,
			Data:       data,
		})
		return nil
	}
	if err := add(envFileSecret(name), d.EnvFile); err != nil {
		return nil, err
	}
	for i, c := range d.Init {
		if err := add(envFileSecret(name+"-"+initName(i, c)), c.EnvFile); err != nil {
			return nil, fmt.Errorf("init %s: %w", initName(i, c), err)
		}
	}
	return out, nil
}

// containerPorts are the container's ports, one per entry.
func (d *ImageDeploy) containerPorts() []corev1.ContainerPort {
	out := make([]corev1.ContainerPort, len(d.Ports))
	for i, p := range d.Ports {
		out[i] = corev1.ContainerPort{Name: p.name(), ContainerPort: p.Port, Protocol: corev1.ProtocolTCP}
	}
	return out
}

// servicePorts are the Service's ports, one per exposed entry, each
// targeting its container port.
func (d *ImageDeploy) servicePorts() []corev1.ServicePort {
	var out []corev1.ServicePort
	for _, p := range d.Ports {
		if p.Exposed() {
			out = append(out, corev1.ServicePort{Name: p.name(), Port: p.Port, TargetPort: intstr.FromString(p.name()), Protocol: corev1.ProtocolTCP})
		}
	}
	return out
}

// envVars are env as a container's variables, in name order so the same
// declaration applies the same Deployment every run.
func envVars(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]corev1.EnvVar, len(names))
	for i, k := range names {
		out[i] = corev1.EnvVar{Name: k, Value: env[k]}
	}
	return out
}

// envFromSecrets are Secret names as a container's env sources.
func envFromSecrets(names []string) []corev1.EnvFromSource {
	if len(names) == 0 {
		return nil
	}
	out := make([]corev1.EnvFromSource, len(names))
	for i, name := range names {
		out[i] = corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
		}}
	}
	return out
}

// ServiceSpec builds the ClusterIP Service for this image.
func (d *ImageDeploy) ServiceSpec(image string) *corev1.Service {
	name := d.deploymentName(image)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.namespace(),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app.kubernetes.io/name": name,
			},
			Ports: d.servicePorts(),
		},
	}
}

// DeployImages applies the Deployments and Services configured for the
// supplied images using Kubernetes server-side apply.
//
// Images without deploy configuration are ignored. It does not wait for
// Deployment rollouts; readiness is handled separately by the environment
// wait phase.
func DeployImages(
	ctx context.Context,
	client kubernetes.Interface,
	images Images,
	opts ...DeployOption,
) error {
	var deployable Images
	for _, img := range images {
		if img.Deploy != nil {
			deployable = append(deployable, img)
		}
	}

	count := len(deployable)
	if count == 0 {
		return nil
	}

	deploying := fmt.Sprintf("Deploying %s", plural(count, "image"))
	spin := term.StartSpinner(deploying)

	deployed := 0
	for _, img := range deployable {
		spin.Update(fmt.Sprintf(
			"%s: %s (%d/%d)",
			deploying,
			img.Image,
			deployed+1,
			count,
		))

		if err := img.Deploy.Apply(ctx, client, img.Image, opts...); err != nil {
			msg := fmt.Sprintf(
				"Deploying %s: failed on %s",
				plural(count, "image"),
				img.Image,
			)
			if deployed > 0 {
				msg += fmt.Sprintf(" (%d/%d deployed)", deployed, count)
			}
			spin.FailureWithUpdate(msg)

			return fmt.Errorf("deploying %q: %w", img.Image, err)
		}

		spin.Stop()
		fmt.Printf("  %s %s deployed\n", term.SuccessMark(), img.Image)
		deployed++

		if deployed < count {
			spin = term.StartSpinner(deploying)
		}
	}

	spin.SuccessWithUpdate(fmt.Sprintf("%s deployed", plural(deployed, "image")))
	return nil
}

// Apply creates or updates the Deployment and Service using
// Kubernetes server-side apply.
//
// It does not wait for the Deployment rollout. Readiness is handled
// separately by the environment wait phase. Assumes all validation
// (image/replicas/port) has been done. Call after validation.
func (d *ImageDeploy) Apply(
	ctx context.Context,
	client kubernetes.Interface,
	image string,
	opts ...DeployOption,
) error {
	if d == nil {
		return nil
	}

	if client == nil {
		return fmt.Errorf("deploy image: Kubernetes client is nil")
	}

	cfg := *d
	manifests, err := d.Manifests(image)
	if err != nil {
		return fmt.Errorf("deploy %s: %w", cfg.Name, err)
	}

	options := resolveDeployOptions(opts)
	// The env files' Secrets first: the pod reads them when it starts.

	for _, sec := range manifests.Secrets {
		if err := applySecret(ctx, client, sec, options); err != nil {
			return fmt.Errorf("deploy %s: apply Secret: %w", cfg.Name, err)
		}
	}
	if err := applyDeployment(ctx, client, manifests.Deployment, options); err != nil {
		return fmt.Errorf("deploy %s: apply Deployment: %w", cfg.Name, err)
	}

	if manifests.Service == nil {
		return nil
	}

	if err := applyService(ctx, client, manifests.Service, options); err != nil {
		return fmt.Errorf("deploy %s: apply Service: %w", cfg.Name, err)
	}

	return nil
}

// applyDeployment applies a Deployment using server-side apply.
func applyDeployment(
	ctx context.Context,
	client kubernetes.Interface,
	obj *appsv1.Deployment,
	opts DeployOptions,
) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal Deployment: %w", err)
	}

	patchOpts := metav1.PatchOptions{
		FieldManager: opts.FieldManager,
	}

	if opts.Force {
		force := true
		patchOpts.Force = &force
	}

	_, err = client.AppsV1().
		Deployments(obj.Namespace).
		Patch(
			ctx,
			obj.Name,
			types.ApplyPatchType,
			data,
			patchOpts,
		)

	if err != nil {
		return fmt.Errorf("apply Deployment %s/%s: %w",
			obj.Namespace, obj.Name, err)
	}

	return nil
}

// applySecret applies a Secret using server-side apply.
func applySecret(ctx context.Context, client kubernetes.Interface, obj *corev1.Secret, opts DeployOptions) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal Secret: %w", err)
	}
	patchOpts := metav1.PatchOptions{FieldManager: opts.FieldManager}
	if opts.Force {
		force := true
		patchOpts.Force = &force
	}
	if _, err := client.CoreV1().Secrets(obj.Namespace).Patch(ctx, obj.Name, types.ApplyPatchType, data, patchOpts); err != nil {
		return fmt.Errorf("apply Secret %s/%s: %w", obj.Namespace, obj.Name, err)
	}
	return nil
}

// applyService applies a Service using server-side apply.
func applyService(
	ctx context.Context,
	client kubernetes.Interface,
	obj *corev1.Service,
	opts DeployOptions,
) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal Service: %w", err)
	}

	patchOpts := metav1.PatchOptions{
		FieldManager: opts.FieldManager,
	}

	if opts.Force {
		force := true
		patchOpts.Force = &force
	}

	_, err = client.CoreV1().
		Services(obj.Namespace).
		Patch(
			ctx,
			obj.Name,
			types.ApplyPatchType,
			data,
			patchOpts,
		)

	if err != nil {
		return fmt.Errorf("apply Service %s/%s: %w",
			obj.Namespace, obj.Name, err)
	}

	return nil
}
