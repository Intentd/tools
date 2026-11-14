package kube

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type refAndManagedFields struct {
	name       string
	apiversion string
	kind       string
	controller *bool
	manager    string
	operation  string
	fieldType  string
}

func sentinelObj(gen int64, labels, annotations map[string]string) metav1.Object {
	o := &metav1.ObjectMeta{
		Generation:  gen,
		Labels:      labels,
		Annotations: annotations,
	}
	return o
}

func collectOwnerRefs(ownRefs []refAndManagedFields) (refs []metav1.OwnerReference) {
	for _, ref := range ownRefs {
		newRef := metav1.OwnerReference{
			Name:       ref.name,
			Kind:       ref.kind,
			APIVersion: ref.apiversion,
			Controller: boolPtr(*ref.controller),
		}
		refs = append(refs, newRef)
	}
	return refs
}

func collectManagedFields(mgdFields []refAndManagedFields) (fields []metav1.ManagedFieldsEntry) {
	for _, field := range mgdFields {
		newEntry := metav1.ManagedFieldsEntry{
			Manager:    field.manager,
			Operation:  metav1.ManagedFieldsOperationType(field.operation),
			APIVersion: field.apiversion,
			FieldsType: field.fieldType,
		}
		fields = append(fields, newEntry)
	}
	return fields
}

func TestComputeSentinels_Empty(t *testing.T) {
	result := ComputeSentinels(nil, sentinelObj(1, nil, nil), sentinelObj(2, nil, nil))
	assert.Nil(t, result)
}

func TestComputeSentinels_GenerationChanged(t *testing.T) {
	result := ComputeSentinels([]string{GenerationChanged.String()},
		sentinelObj(1, nil, nil), sentinelObj(2, nil, nil))
	assert.Equal(t, "true", result[GenerationChanged.String()])
}

func TestComputeSentinels_GenerationUnchanged(t *testing.T) {
	result := ComputeSentinels([]string{GenerationChanged.String()},
		sentinelObj(5, nil, nil), sentinelObj(5, nil, nil))
	assert.Equal(t, "false", result[GenerationChanged.String()])
}

func TestComputeSentinels_LabelsChanged(t *testing.T) {
	result := ComputeSentinels([]string{LabelsChanged.String()},
		sentinelObj(1, map[string]string{"a": "1"}, nil),
		sentinelObj(1, map[string]string{"a": "2"}, nil))
	assert.Equal(t, "true", result[LabelsChanged.String()])
}

func TestComputeSentinels_LabelsUnchanged(t *testing.T) {
	result := ComputeSentinels([]string{LabelsChanged.String()},
		sentinelObj(1, map[string]string{"a": "1"}, nil),
		sentinelObj(1, map[string]string{"a": "1"}, nil))
	assert.Equal(t, "false", result[LabelsChanged.String()])
}

func TestComputeSentinels_AnnotationsChanged(t *testing.T) {
	result := ComputeSentinels([]string{AnnotationsChanged.String()},
		sentinelObj(1, nil, map[string]string{"x": "old"}),
		sentinelObj(1, nil, map[string]string{"x": "new"}))
	assert.Equal(t, "true", result[AnnotationsChanged.String()])
}

func TestComputeSentinels_OnlyDeclaredAreComputed(t *testing.T) {
	result := ComputeSentinels([]string{GenerationChanged.String()},
		sentinelObj(1, map[string]string{"a": "1"}, nil),
		sentinelObj(2, map[string]string{"b": "2"}, nil))
	assert.Equal(t, "true", result[GenerationChanged.String()])
	_, hasLabels := result[LabelsChanged.String()]
	assert.False(t, hasLabels)
}

func TestComputeSentinels_DeletionStarted(t *testing.T) {
	now := metav1.Now()
	old := &metav1.ObjectMeta{}
	new := &metav1.ObjectMeta{DeletionTimestamp: &now}
	result := ComputeSentinels([]string{DeletionStarted.String()}, old, new)
	assert.Equal(t, "true", result[DeletionStarted.String()])
}

func TestComputeSentinels_DeletionStarted_AlreadyDeleting(t *testing.T) {
	now := metav1.Now()
	old := &metav1.ObjectMeta{DeletionTimestamp: &now}
	new := &metav1.ObjectMeta{DeletionTimestamp: &now}
	result := ComputeSentinels([]string{DeletionStarted.String()}, old, new)
	assert.Equal(t, "false", result[DeletionStarted.String()])
}

func TestComputeSentinels_FinalizersChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Finalizers: []string{"inrun.dev/protect"}}
	new := &metav1.ObjectMeta{}
	result := ComputeSentinels([]string{FinalizersChanged.String()}, old, new)
	assert.Equal(t, "true", result[FinalizersChanged.String()])
}

func TestComputeSentinels_FinalizersUnchanged(t *testing.T) {
	old := &metav1.ObjectMeta{Finalizers: []string{"inrun.dev/protect"}}
	new := &metav1.ObjectMeta{Finalizers: []string{"inrun.dev/protect"}}
	result := ComputeSentinels([]string{FinalizersChanged.String()}, old, new)
	assert.Equal(t, "false", result[FinalizersChanged.String()])
}

func TestComputeSentinels_NameChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Name: "inrun"}
	new := &metav1.ObjectMeta{Name: "inrun-v2"}
	result := ComputeSentinels([]string{NameChanged.String()}, old, new)
	assert.Equal(t, "true", result[NameChanged.String()])
}

func TestComputeSentinels_NameUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Name: "inrun"}
	new := &metav1.ObjectMeta{Name: "inrun"}
	result := ComputeSentinels([]string{NameChanged.String()}, old, new)
	assert.Equal(t, "false", result[NameChanged.String()])
}

func TestComputeSentinels_NamespaceChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Namespace: "inrun-system"}
	new := &metav1.ObjectMeta{Namespace: "inrun-v2-system"}
	result := ComputeSentinels([]string{NamespaceChanged.String()}, old, new)
	assert.Equal(t, "true", result[NamespaceChanged.String()])
}

func TestComputeSentinels_NamespaceUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{Namespace: "inrun-system"}
	new := &metav1.ObjectMeta{Namespace: "inrun-system"}
	result := ComputeSentinels([]string{NamespaceChanged.String()}, old, new)
	assert.Equal(t, "false", result[NamespaceChanged.String()])
}

func TestComputeSentinels_GenerateNameChanged(t *testing.T) {
	old := &metav1.ObjectMeta{GenerateName: "inrun-generate"}
	new := &metav1.ObjectMeta{GenerateName: "inrun-v2-generate"}
	result := ComputeSentinels([]string{GenerateNameChanged.String()}, old, new)
	assert.Equal(t, "true", result[GenerateNameChanged.String()])
}

func TestComputeSentinels_GenerateNameUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{GenerateName: "inrun-generate"}
	new := &metav1.ObjectMeta{GenerateName: "inrun-generate"}
	result := ComputeSentinels([]string{GenerateNameChanged.String()}, old, new)
	assert.Equal(t, "false", result[GenerateNameChanged.String()])
}

func TestComputeSentinels_UIDChanged(t *testing.T) {
	old := &metav1.ObjectMeta{UID: "UID-XX-12-Y-TX"}
	new := &metav1.ObjectMeta{UID: "UID-XX-12-Y-TY"}
	result := ComputeSentinels([]string{UIDChanged.String()}, old, new)
	assert.Equal(t, "true", result[UIDChanged.String()])
}

func TestComputeSentinels_UIDUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{UID: "UID-XX-12-Y-TX"}
	new := &metav1.ObjectMeta{UID: "UID-XX-12-Y-TX"}
	result := ComputeSentinels([]string{UIDChanged.String()}, old, new)
	assert.Equal(t, "false", result[UIDChanged.String()])
}

func TestComputeSentinels_ResourceVersionChanged(t *testing.T) {
	old := &metav1.ObjectMeta{ResourceVersion: "inrun-v1.0"}
	new := &metav1.ObjectMeta{ResourceVersion: "inrun-v2-v1.1"}
	result := ComputeSentinels([]string{ResourceVersionChanged.String()}, old, new)
	assert.Equal(t, "true", result[ResourceVersionChanged.String()])
}

func TestComputeSentinels_ResourceVersionUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{ResourceVersion: "inrun-v1.0"}
	new := &metav1.ObjectMeta{ResourceVersion: "inrun-v1.0"}
	result := ComputeSentinels([]string{ResourceVersionChanged.String()}, old, new)
	assert.Equal(t, "false", result[ResourceVersionChanged.String()])
}

func TestComputeSentinels_CreationTimestampChanged(t *testing.T) {
	now := metav1.Now()
	newTime := metav1.NewTime(now.Add(5))

	old := &metav1.ObjectMeta{CreationTimestamp: now}
	new := &metav1.ObjectMeta{CreationTimestamp: newTime}
	result := ComputeSentinels([]string{CreationTimestampChanged.String()}, old, new)
	assert.Equal(t, "true", result[CreationTimestampChanged.String()])
}

func TestComputeSentinels_CreationTimestamUnpChanged(t *testing.T) {
	now := metav1.Now()

	old := &metav1.ObjectMeta{CreationTimestamp: now}
	new := &metav1.ObjectMeta{CreationTimestamp: now}
	result := ComputeSentinels([]string{CreationTimestampChanged.String()}, old, new)
	assert.Equal(t, "false", result[CreationTimestampChanged.String()])
}

func TestComputeSentinels_DeletionGracePeriodSecondsChanged(t *testing.T) {
	old := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	new := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(6)}
	result := ComputeSentinels([]string{DeletionGracePeriodSecondsChanged.String()}, old, new)
	assert.Equal(t, "true", result[DeletionGracePeriodSecondsChanged.String()])
}

func TestComputeSentinels_DeletionGracePeriodSecondsUnChanged(t *testing.T) {
	old := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	new := &metav1.ObjectMeta{DeletionGracePeriodSeconds: int64Ptr(5)}
	result := ComputeSentinels([]string{DeletionGracePeriodSecondsChanged.String()}, old, new)
	assert.Equal(t, "false", result[DeletionGracePeriodSecondsChanged.String()])
}

func TestComputeSentinels_OwnerReferenceChangedNewControllerPtr(t *testing.T) {
	ref := func(ctrl bool) refAndManagedFields {
		return refAndManagedFields{
			name:       "inrun-v2",
			kind:       "Blueprint",
			apiversion: "test.inrun.dev",
			controller: boolPtr(ctrl),
		}
	}

	oldRef := []refAndManagedFields{ref(false)}
	newRef := []refAndManagedFields{ref(true)}

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(newRef)}
	result := ComputeSentinels([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "true", result[OwnerReferenceChanged.String()])
}

func TestComputeSentinels_OwnerReferenceChangedAddNewRef(t *testing.T) {
	oldRef := []refAndManagedFields{
		{
			name:       "inrun-v2",
			kind:       "Blueprint",
			apiversion: "test.inrun.dev",
			controller: boolPtr(false),
		},
	}
	newRef := append(oldRef, refAndManagedFields{
		name:       "inrun-v2-gateway",
		kind:       "Stack",
		apiversion: "test.inrun.dev",
		controller: boolPtr(true),
	})

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(newRef)}
	result := ComputeSentinels([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "true", result[OwnerReferenceChanged.String()])
}

func TestComputeSentinels_OwnerReferenceUnChanged(t *testing.T) {
	ref := func(ctrl bool) refAndManagedFields {
		return refAndManagedFields{
			name:       "inrun-v2",
			kind:       "Blueprint",
			apiversion: "test.inrun.dev",
			controller: boolPtr(ctrl),
		}
	}

	oldRef := []refAndManagedFields{ref(false)}

	old := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	new := &metav1.ObjectMeta{OwnerReferences: collectOwnerRefs(oldRef)}
	result := ComputeSentinels([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "false", result[OwnerReferenceChanged.String()])
}

func TestComputeSentinels_ManagedFieldsChanged(t *testing.T) {
	entry := func(manager, operation string) refAndManagedFields {
		return refAndManagedFields{
			apiversion: "test.inrun.dev",
			fieldType:  "test",
			manager:    manager,
			operation:  operation,
		}
	}

	oldEntry := []refAndManagedFields{entry("inrun", "apply")}
	newEntry := []refAndManagedFields{entry("inrun-v2", "apply")}

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(newEntry)}
	result := ComputeSentinels([]string{ManagedFieldsChanged.String()}, old, new)
	assert.Equal(t, "true", result[ManagedFieldsChanged.String()])
}

func TestComputeSentinels_OwnerReferenceChangedAddNewField(t *testing.T) {
	oldEntry := []refAndManagedFields{
		{
			apiversion: "test.inrun.dev",
			fieldType:  "test",
			manager:    "inrun-v2-manager",
			operation:  "apply",
		},
	}
	newEntry := append(oldEntry, refAndManagedFields{
		apiversion: "test.inrun.dev",
		fieldType:  "test",
		manager:    "inrun-v2-manager",
		operation:  "update",
	})

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(newEntry)}
	result := ComputeSentinels([]string{ManagedFieldsChanged.String()}, old, new)
	assert.Equal(t, "true", result[ManagedFieldsChanged.String()])
}

func TestComputeSentinels_ManagedFieldsUnChanged(t *testing.T) {
	entry := func(manager, operation string) refAndManagedFields {
		return refAndManagedFields{
			apiversion: "test.inrun.dev",
			fieldType:  "test",
			manager:    manager,
			operation:  operation,
		}
	}

	oldEntry := []refAndManagedFields{entry("inrun", "exists")}

	old := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	new := &metav1.ObjectMeta{ManagedFields: collectManagedFields(oldEntry)}
	result := ComputeSentinels([]string{OwnerReferenceChanged.String()}, old, new)
	assert.Equal(t, "false", result[OwnerReferenceChanged.String()])
}

func TestComputeSentinels_UnknownSentinelReturnsEmpty(t *testing.T) {
	result := ComputeSentinels([]string{"specChanged"},
		sentinelObj(1, nil, nil), sentinelObj(2, nil, nil))
	assert.Equal(t, "", result["specChanged"])
}

func TestIsValidSentinel_ValidSentinel(t *testing.T) {
	known := ManagedFieldsChanged.String()
	assert.Equal(t, true, IsValidSentinel(known))
}

func TestIsValidSentinel_InalidSentinel(t *testing.T) {
	assert.Equal(t, false, IsValidSentinel("unknown"))
}

func TestAllValidSentinels_MultipleValidSentinels(t *testing.T) {
	known := []string{ManagedFieldsChanged.String(), NameChanged.String(), NamespaceChanged.String()}
	allValid, unknown := AllValidSentinels(known)
	assert.Equal(t, []string([]string(nil)), unknown)
	assert.Equal(t, true, allValid)
}

func TestAllValidSentinels_MultipleInvalidSentinels(t *testing.T) {
	known := []string{
		ManagedFieldsChanged.String(), "invalid-1",
		NameChanged.String(), "invalid-2",
		NamespaceChanged.String(), "invalid-3",
	}
	allValid, unknown := AllValidSentinels(known)

	assert.Equal(t, []string{"invalid-1", "invalid-2", "invalid-3"}, unknown)
	assert.Equal(t, 3, len(unknown))
	assert.Equal(t, false, allValid)
}

func int64Ptr(v int64) *int64 { return &v }
func boolPtr(v bool) *bool    { return &v }
