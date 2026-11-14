package cluster

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	"github.com/intentd/tools/structured/structuredtest"
)

// deploy.env lands in the container in name order, the same Deployment
// every run; envFrom loads each Secret whole.
func TestDeploymentSpec_EnvAndEnvFrom(t *testing.T) {
	d := &ImageDeploy{Name: "api", Ports: Ports{{Port: 8090, Host: 8090}},
		Env:     map[string]string{"B_URL": "b", "A_URL": "a"},
		EnvFrom: []string{"shop-deps"},
	}
	c := d.DeploymentSpec("api:dev").Spec.Template.Spec.Containers[0]
	assert.Equal(t, []corev1.EnvVar{{Name: "A_URL", Value: "a"}, {Name: "B_URL", Value: "b"}}, c.Env)
	if assert.Len(t, c.EnvFrom, 1) {
		assert.Equal(t, "shop-deps", c.EnvFrom[0].SecretRef.Name)
	}
	plain := (&ImageDeploy{Name: "w", Ports: Ports{{Port: 80, Host: 80}}}).DeploymentSpec("w:dev").Spec.Template.Spec.Containers[0]
	assert.Nil(t, plain.Env)
	assert.Nil(t, plain.EnvFrom)
}

// Ports are written as Compose writes them: 8080, "8081:8080" (host on the
// left), or the struct; Host defaults to the container port.
func TestPortUnmarshal(t *testing.T) {
	var ps Ports
	require.NoError(t, yaml.Unmarshal([]byte("[8080, \"8081:8080\", {port: 9090, forward: false}]"), &ps))
	no := false
	assert.Equal(t, Ports{{Port: 8080, Host: 8080}, {Port: 8080, Host: 8081}, {Port: 9090, Host: 9090, Forward: &no}}, ps)
	assert.False(t, ps[2].Forwarded())
	assert.True(t, ps[0].Forwarded())

	for _, bad := range []string{"[http]", "[0]", "[\"70000:80\"]"} {
		var p Ports
		assert.Error(t, yaml.Unmarshal([]byte(bad), &p), bad)
	}
	internal := Port{Port: 80, Host: 80, Expose: &no}
	assert.NoError(t, internal.Validate())
	assert.False(t, internal.Exposed())
	assert.False(t, internal.Forwarded(), "not exposed, not forwarded")
}

// Every port is a container port and a Service port of its own name.
func TestDeploymentSpec_Ports(t *testing.T) {
	d := &ImageDeploy{Name: "api", Ports: Ports{{Port: 8080, Host: 8081}, {Port: 9090, Host: 9090}}}
	c := d.DeploymentSpec("api:dev").Spec.Template.Spec.Containers[0]
	assert.Equal(t, []corev1.ContainerPort{{Name: "p8080", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}, {Name: "p9090", ContainerPort: 9090, Protocol: corev1.ProtocolTCP}}, c.Ports)
	svc := d.ServiceSpec("api:dev").Spec.Ports
	assert.Len(t, svc, 2)
	assert.Equal(t, int32(9090), svc[1].Port)
	assert.Equal(t, "p9090", svc[1].TargetPort.String())

	no := false
	d.Ports[1].Expose = &no
	assert.Len(t, d.ServiceSpec("api:dev").Spec.Ports, 1, "a container port only")
	assert.Len(t, d.DeploymentSpec("api:dev").Spec.Template.Spec.Containers[0].Ports, 2)
}

// A health check is written as Compose writes it: a localhost HTTP GET
// becomes httpGet (an image without curl still passes), CMD-SHELL runs
// through sh -c, the flat form is readiness unless as says more, and
// flat and named together are refused.
func TestHealthcheck(t *testing.T) {
	var flat Healthchecks
	require.NoError(t, yaml.Unmarshal([]byte(`{test: ["CMD", "curl", "-f", "http://localhost:8080/health?x=1"], interval: 10s, retries: 3, as: [startup, readiness]}`), &flat))
	d := &ImageDeploy{Name: "api", Ports: Ports{{Port: 8080, Host: 8080}}, Healthcheck: &flat}
	c := d.DeploymentSpec("api:dev").Spec.Template.Spec.Containers[0]
	require.NotNil(t, c.ReadinessProbe)
	require.NotNil(t, c.StartupProbe)
	assert.Nil(t, c.LivenessProbe)
	assert.Equal(t, "/health?x=1", c.ReadinessProbe.HTTPGet.Path)
	assert.Equal(t, int32(8080), c.ReadinessProbe.HTTPGet.Port.IntVal)
	assert.Equal(t, int32(10), c.ReadinessProbe.PeriodSeconds)
	assert.Equal(t, int32(3), c.ReadinessProbe.FailureThreshold)

	var named Healthchecks
	require.NoError(t, yaml.Unmarshal([]byte(`{liveness: {test: ["CMD-SHELL", "pg_isready -U app"], start_period: 1500ms}}`), &named))
	d.Healthcheck = &named
	c = d.DeploymentSpec("api:dev").Spec.Template.Spec.Containers[0]
	assert.Nil(t, c.ReadinessProbe)
	require.NotNil(t, c.LivenessProbe)
	assert.Equal(t, []string{"sh", "-c", "pg_isready -U app"}, c.LivenessProbe.Exec.Command)
	assert.Equal(t, int32(2), c.LivenessProbe.InitialDelaySeconds, "rounded up")

	var mixed Healthchecks
	assert.ErrorContains(t, yaml.Unmarshal([]byte(`{test: ["CMD", "true"], readiness: {test: ["CMD", "true"]}}`), &mixed), "not both")
	assert.ErrorContains(t, (&Healthchecks{Healthcheck: &Healthcheck{Test: []string{"curl"}}}).Validate(), "start with CMD, CMD-SHELL or NONE")
}

// init runs before the app's container, in order: the app's image when an
// entry names none; a command is required.
func TestDeploymentSpec_Init(t *testing.T) {
	d := &ImageDeploy{Name: "api", Ports: Ports{{Port: 8080, Host: 8080}}, Init: []InitContainer{
		{Name: "wait-db", Image: "busybox:1.37", Command: []string{"sh", "-c", "until nc -z db 5432; do sleep 1; done"}},
		{Command: []string{"./migrate", "up"}, Env: map[string]string{"DB": "x"}},
	}}
	inits := d.DeploymentSpec("api:dev").Spec.Template.Spec.InitContainers
	require.Len(t, inits, 2)
	assert.Equal(t, "wait-db", inits[0].Name)
	assert.Equal(t, "busybox:1.37", inits[0].Image)
	assert.Equal(t, "init-1", inits[1].Name)
	assert.Equal(t, "api:dev", inits[1].Image, "the app's image")
	assert.Equal(t, []corev1.EnvVar{{Name: "DB", Value: "x"}}, inits[1].Env)
	assert.ErrorContains(t, InitContainer{Name: "x"}.Validate(), "set command")
}

// envFile: the files' variables become a Secret per container, a later file
// winning, loaded with envFrom; an init's is its own.
func TestEnvFile(t *testing.T) {
	dir := t.TempDir()
	base, local := filepath.Join(dir, ".env"), filepath.Join(dir, ".env.local")
	structuredtest.WriteFile(t, base, "DB=one\nNAME=shop\n")
	structuredtest.WriteFile(t, local, "DB=two\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "init.env"), "STEP=migrate\n")
	d := &ImageDeploy{Name: "api", Ports: Ports{{Port: 8080, Host: 8080}}, EnvFile: EnvFiles{base, local},
		Init: []InitContainer{{Name: "migrate", Command: []string{"./migrate"}, EnvFile: EnvFiles{filepath.Join(dir, "init.env")}}}}

	secrets, err := d.envFileSecrets("api:dev")
	require.NoError(t, err)
	require.Len(t, secrets, 2)
	assert.Equal(t, "api-env", secrets[0].Name)
	assert.Equal(t, map[string][]byte{"DB": []byte("two"), "NAME": []byte("shop")}, secrets[0].Data)
	assert.Equal(t, "api-migrate-env", secrets[1].Name)

	spec := d.DeploymentSpec("api:dev").Spec.Template.Spec
	assert.Equal(t, "api-env", spec.Containers[0].EnvFrom[0].SecretRef.Name)
	assert.Equal(t, "api-migrate-env", spec.InitContainers[0].EnvFrom[0].SecretRef.Name)

	var one EnvFiles
	require.NoError(t, yaml.Unmarshal([]byte(".env"), &one))
	assert.Equal(t, EnvFiles{".env"}, one)
	assert.Equal(t, EnvFiles{filepath.Join("/w", ".env"), "/abs/.env"}, EnvFiles{".env", "/abs/.env"}.ResolvePaths("/w"))
}
