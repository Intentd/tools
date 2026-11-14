// Package builtins is the table of Kubernetes built-in resource kinds: each
// kind's API identity (group, version, kind, plural, scope, API path), its
// shorthands, and how its status behaves.
package builtins

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Def is one built-in kind.
type Def struct {
	Kind    string // PascalCase Kind name (e.g. "Deployment")
	Group   string // API group; empty string for core
	Version string // API version (e.g. "v1", "v2")
	Plural  string // plural resource name (e.g. "deployments")

	Namespaced bool   // true if resource is namespaced
	APIPath    string // "/api" for core, "/apis" otherwise

	// Shorthands are case-insensitive aliases, e.g. "hpa" for
	// horizontalpodautoscaler.
	Shorthands []string

	Statusless             bool // No meaningful status; ready once it exists
	SkipStatusSubresource  bool // No /status subresource; never PATCH status
	SkipObservedGeneration bool // Has status but no observedGeneration
}

// All returns every built-in kind, keyed by its lowercase singular kind name.
func All() map[string]Def {
	out := make(map[string]Def, len(table))
	for k, d := range table {
		out[k] = d
	}
	return out
}

// Lookup returns the built-in kind named kind, by its singular name or a
// shorthand, case-insensitive.
func Lookup(kind string) (Def, bool) {
	key := strings.ToLower(strings.TrimSpace(kind))
	if expanded, ok := shorthands[key]; ok {
		key = expanded
	}
	d, ok := table[key]
	return d, ok
}

// ByGVK returns the built-in kind with this kind and group. The version is
// not compared: a built-in kind is the same kind at every version it serves.
func ByGVK(group, version, kind string) (Def, bool) {
	d, ok := Lookup(kind)
	if !ok || !strings.EqualFold(d.Group, group) {
		return Def{}, false
	}
	return d, true
}

// ListKinds returns each built-in kind's GVR and list kind, as a fake dynamic
// client needs them registered before it can List a resource.
func ListKinds() map[schema.GroupVersionResource]string {
	out := make(map[schema.GroupVersionResource]string, len(table))
	for _, d := range table {
		out[schema.GroupVersionResource{Group: d.Group, Version: d.Version, Resource: d.Plural}] = d.Kind + "List"
	}
	return out
}

// shorthands maps each shorthand to its kind's key in table.
var shorthands = func() map[string]string {
	out := map[string]string{}
	for key, d := range table {
		for _, sh := range d.Shorthands {
			out[strings.ToLower(sh)] = key
		}
	}
	return out
}()

// table is every built-in kind, keyed by its lowercase singular kind name.
var table = map[string]Def{

	// ── Core group (group: "", apiVersion: v1) ────────────────────────────────

	"pod": {
		Kind:                   "Pod",
		Group:                  "",
		Version:                "v1",
		Plural:                 "pods",
		Namespaced:             true,
		APIPath:                "/api",
		SkipObservedGeneration: true,
		Shorthands:             []string{"po"},
	},

	"service": {
		Kind:                   "Service",
		Group:                  "",
		Version:                "v1",
		Plural:                 "services",
		Namespaced:             true,
		APIPath:                "/api",
		Shorthands:             []string{"svc"},
		SkipObservedGeneration: true,
	},

	"configmap": {
		Kind:                  "ConfigMap",
		Group:                 "",
		Version:               "v1",
		Plural:                "configmaps",
		Namespaced:            true,
		APIPath:               "/api",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"cm"},
	},

	"secret": {
		Kind:                  "Secret",
		Group:                 "",
		Version:               "v1",
		Plural:                "secrets",
		Namespaced:            true,
		APIPath:               "/api",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	"namespace": {
		Kind:                   "Namespace",
		Group:                  "",
		Version:                "v1",
		Plural:                 "namespaces",
		Namespaced:             false,
		APIPath:                "/api",
		SkipObservedGeneration: true,
		Shorthands:             []string{"ns"},
	},

	"serviceaccount": {
		Kind:                  "ServiceAccount",
		Group:                 "",
		Version:               "v1",
		Plural:                "serviceaccounts",
		Namespaced:            true,
		APIPath:               "/api",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"sa"},
	},

	"persistentvolumeclaim": {
		Kind:                   "PersistentVolumeClaim",
		Group:                  "",
		Version:                "v1",
		Plural:                 "persistentvolumeclaims",
		Namespaced:             true,
		APIPath:                "/api",
		SkipObservedGeneration: true,
		Shorthands:             []string{"pvc", "pvcs", "pvclaim"},
	},

	"persistentvolume": {
		Kind:                   "PersistentVolume",
		Group:                  "",
		Version:                "v1",
		Plural:                 "persistentvolumes",
		Namespaced:             false,
		APIPath:                "/api",
		SkipObservedGeneration: true,
		Shorthands:             []string{"pv", "pvs"},
	},

	"event": {
		Kind:                  "Event",
		Group:                 "",
		Version:               "v1",
		Plural:                "events",
		Namespaced:            true,
		APIPath:               "/api",
		Shorthands:            []string{"ev"},
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	"node": {
		Kind:                   "Node",
		Group:                  "",
		Version:                "v1",
		Plural:                 "nodes",
		Namespaced:             false,
		APIPath:                "/api",
		SkipObservedGeneration: true,
	},

	"resourcequota": {
		Kind:                   "ResourceQuota",
		Group:                  "",
		Version:                "v1",
		Plural:                 "resourcequotas",
		Namespaced:             true,
		APIPath:                "/api",
		SkipObservedGeneration: true,
	},

	"limitrange": {
		Kind:                   "LimitRange",
		Group:                  "",
		Version:                "v1",
		Plural:                 "limitranges",
		Namespaced:             true,
		APIPath:                "/api",
		SkipObservedGeneration: true,
	},

	"componentstatus": {
		Kind:                  "ComponentStatus",
		Group:                 "",
		Version:               "v1",
		Plural:                "componentstatuses",
		Namespaced:            false,
		APIPath:               "/api",
		SkipStatusSubresource: true,
	},

	"podtemplate": {
		Kind:                  "PodTemplate",
		Group:                 "",
		Version:               "v1",
		Plural:                "podtemplates",
		Namespaced:            true,
		APIPath:               "/api",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	// ── apps/v1 ───────────────────────────────────────────────────────────────

	"deployment": {
		Kind:       "Deployment",
		Group:      "apps",
		Version:    "v1",
		Plural:     "deployments",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"deploy", "dep"},
	},

	"statefulset": {
		Kind:       "StatefulSet",
		Group:      "apps",
		Version:    "v1",
		Plural:     "statefulsets",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"sts"},
	},

	"daemonset": {
		Kind:       "DaemonSet",
		Group:      "apps",
		Version:    "v1",
		Plural:     "daemonsets",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"ds"},
	},

	"replicaset": {
		Kind:       "ReplicaSet",
		Group:      "apps",
		Version:    "v1",
		Plural:     "replicasets",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"rs"},
	},

	// ── batch/v1 ──────────────────────────────────────────────────────────────

	"job": {
		Kind:                  "Job",
		Group:                 "batch",
		Version:               "v1",
		Plural:                "jobs",
		Namespaced:            true,
		APIPath:               "/apis",
		SkipStatusSubresource: true,
	},

	"cronjob": {
		Kind:                  "CronJob",
		Group:                 "batch",
		Version:               "v1",
		Plural:                "cronjobs",
		Namespaced:            true,
		APIPath:               "/apis",
		SkipStatusSubresource: true,
		Shorthands:            []string{"cj"},
	},

	// ── networking.k8s.io/v1 ─────────────────────────────────────────────────

	"ingress": {
		Kind:       "Ingress",
		Group:      "networking.k8s.io",
		Version:    "v1",
		Plural:     "ingresses",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"ing"},
	},

	"networkpolicy": {
		Kind:                  "NetworkPolicy",
		Group:                 "networking.k8s.io",
		Version:               "v1",
		Plural:                "networkpolicies",
		Namespaced:            true,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"np"},
	},

	"ingressclass": {
		Kind:       "IngressClass",
		Group:      "networking.k8s.io",
		Version:    "v1",
		Plural:     "ingressclasses",
		Namespaced: false,
		APIPath:    "/apis",
	},

	// ── autoscaling/v2 ────────────────────────────────────────────────────────

	"horizontalpodautoscaler": {
		Kind:       "HorizontalPodAutoscaler",
		Group:      "autoscaling",
		Version:    "v2",
		Plural:     "horizontalpodautoscalers",
		Namespaced: true,
		APIPath:    "/apis",
		Shorthands: []string{"hpa"},
	},

	// ── rbac.authorization.k8s.io/v1 ─────────────────────────────────────────

	"role": {
		Kind:                  "Role",
		Group:                 "rbac.authorization.k8s.io",
		Version:               "v1",
		Plural:                "roles",
		Namespaced:            true,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	"rolebinding": {
		Kind:                  "RoleBinding",
		Group:                 "rbac.authorization.k8s.io",
		Version:               "v1",
		Plural:                "rolebindings",
		Namespaced:            true,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"rb"},
	},

	"clusterrole": {
		Kind:                  "ClusterRole",
		Group:                 "rbac.authorization.k8s.io",
		Version:               "v1",
		Plural:                "clusterroles",
		Namespaced:            false,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"cr"},
	},

	"clusterrolebinding": {
		Kind:                  "ClusterRoleBinding",
		Group:                 "rbac.authorization.k8s.io",
		Version:               "v1",
		Plural:                "clusterrolebindings",
		Namespaced:            false,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
		Shorthands:            []string{"crb"},
	},

	// ── policy/v1 ─────────────────────────────────────────────────────────────

	"poddisruptionbudget": {
		Kind:                   "PodDisruptionBudget",
		Group:                  "policy",
		Version:                "v1",
		Plural:                 "poddisruptionbudgets",
		Namespaced:             true,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
		Shorthands:             []string{"pdb"},
	},

	// ── storage.k8s.io/v1 ────────────────────────────────────────────────────

	"storageclass": {
		Kind:       "StorageClass",
		Group:      "storage.k8s.io",
		Version:    "v1",
		Plural:     "storageclasses",
		Namespaced: false,
		APIPath:    "/apis",
		Shorthands: []string{"sc"},
	},

	"volumeattachment": {
		Kind:                   "VolumeAttachment",
		Group:                  "storage.k8s.io",
		Version:                "v1",
		Plural:                 "volumeattachments",
		Namespaced:             false,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
	},

	// ── apiextensions.k8s.io/v1 ──────────────────────────────────────────────

	"customresourcedefinition": {
		Kind:                   "CustomResourceDefinition",
		Group:                  "apiextensions.k8s.io",
		Version:                "v1",
		Plural:                 "customresourcedefinitions",
		Namespaced:             false,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
		Shorthands:             []string{"crd"},
	},

	// ── apiregistration.k8s.io/v1 ────────────────────────────────────────────

	"apiservice": {
		Kind:                   "APIService",
		Group:                  "apiregistration.k8s.io",
		Version:                "v1",
		Plural:                 "apiservices",
		Namespaced:             false,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
	},

	// ── admissionregistration.k8s.io/v1 ──────────────────────────────────────

	"mutatingwebhookconfiguration": {
		Kind:                  "MutatingWebhookConfiguration",
		Group:                 "admissionregistration.k8s.io",
		Version:               "v1",
		Plural:                "mutatingwebhookconfigurations",
		Namespaced:            false,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	"validatingwebhookconfiguration": {
		Kind:                  "ValidatingWebhookConfiguration",
		Group:                 "admissionregistration.k8s.io",
		Version:               "v1",
		Plural:                "validatingwebhookconfigurations",
		Namespaced:            false,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	// ── scheduling.k8s.io/v1 ─────────────────────────────────────────────────

	"priorityclass": {
		Kind:                  "PriorityClass",
		Group:                 "scheduling.k8s.io",
		Version:               "v1",
		Plural:                "priorityclasses",
		Namespaced:            false,
		APIPath:               "/apis",
		Statusless:            true,
		SkipStatusSubresource: true,
	},

	// ── events.k8s.io/v1 ─────────────────────────────────────────────────────
	// Internal key avoids clashing with core/v1 Event.

	"event_events": {
		Kind: "Event", Group: "events.k8s.io", Version: "v1", Plural: "events",
		Namespaced: true, APIPath: "/apis",
		Statusless: true, SkipStatusSubresource: true,
	},

	// ── discovery.k8s.io/v1 ──────────────────────────────────────────────────

	"endpointslice": {
		Kind:                   "EndpointSlice",
		Group:                  "discovery.k8s.io",
		Version:                "v1",
		Plural:                 "endpointslices",
		Namespaced:             true,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
		Shorthands:             []string{"ep"},
	},

	// ── coordination.k8s.io/v1 ───────────────────────────────────────────────

	"lease": {
		Kind:                   "Lease",
		Group:                  "coordination.k8s.io",
		Version:                "v1",
		Plural:                 "leases",
		Namespaced:             true,
		APIPath:                "/apis",
		SkipObservedGeneration: true,
	},
}
