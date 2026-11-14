package cluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/intentd/tools/term"
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

	// Port is both the container port and Service port.
	// Required. TCP is used.
	Port int32 `yaml:"port"`

	// Replicas is the desired number of Deployment replicas.
	// Defaults to 1.
	Replicas *int32 `yaml:"replicas,omitempty"`
}

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
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: d.Port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}
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
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       d.Port,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
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
	deployment := cfg.DeploymentSpec(image)
	service := cfg.ServiceSpec(image)

	options := resolveDeployOptions(opts)
	if err := applyDeployment(ctx, client, deployment, options); err != nil {
		return fmt.Errorf("deploy %s: apply Deployment: %w", cfg.Name, err)
	}

	if err := applyService(ctx, client, service, options); err != nil {
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
