package kube

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Sentinel names one observable metadata change of an object between two
// versions of it (an informer update's old and new), computed as "true" or
// "false" by ComputeSentinels.
type Sentinel string

const (
	GenerationChanged  Sentinel = "generationChanged"
	LabelsChanged      Sentinel = "labelsChanged"
	AnnotationsChanged Sentinel = "annotationsChanged"
	// DeletionStarted is true when the object's DeletionTimestamp transitions
	// from nil to non-nil — i.e. the moment a delete is issued. Only computable
	// at event time (old vs new); by reconcile time the timestamp is already set.
	DeletionStarted Sentinel = "deletionStarted"
	// FinalizersChanged is true when the finalizer list differs between old and new.
	FinalizersChanged Sentinel = "finalizersChanged"

	// The rest cover every other field of metav1.Object.
	NameChanged                       Sentinel = "nameChanged"
	NamespaceChanged                  Sentinel = "namespaceChanged"
	GenerateNameChanged               Sentinel = "generateNameChanged"
	UIDChanged                        Sentinel = "uidChanged"
	ResourceVersionChanged            Sentinel = "resourceVersionChanged"
	CreationTimestampChanged          Sentinel = "creationTimestampChanged"
	DeletionGracePeriodSecondsChanged Sentinel = "deletionGracePeriodSecondsChanged"
	OwnerReferenceChanged             Sentinel = "ownerReferenceChanged"
	ManagedFieldsChanged              Sentinel = "managedFieldsChanged"
)

// String stringifies a sentinel
func (s Sentinel) String() string {
	return string(s)
}

// ValidSentinels returns all known sentinel names in declaration order.
func ValidSentinels() []string {
	return []string{
		string(GenerationChanged),
		string(LabelsChanged),
		string(AnnotationsChanged),
		string(DeletionStarted),
		string(FinalizersChanged),
		string(NameChanged),
		string(NamespaceChanged),
		string(GenerateNameChanged),
		string(UIDChanged),
		string(ResourceVersionChanged),
		string(CreationTimestampChanged),
		string(DeletionGracePeriodSecondsChanged),
		string(OwnerReferenceChanged),
		string(ManagedFieldsChanged),
	}
}

// IsValidSentinel reports whether s is a known sentinel name.
func IsValidSentinel(s string) bool {
	for _, sent := range ValidSentinels() {
		if sent == s {
			return true
		}
	}
	return false
}

// AllValidSentinels reports whether every name in s is a known sentinel,
// with the unknown ones.
func AllValidSentinels(s []string) (bool, []string) {
	var unknown []string
	for _, sent := range s {
		if !IsValidSentinel(sent) {
			unknown = append(unknown, sent)
		}
	}
	if len(unknown) > 0 {
		return false, unknown
	}
	return true, nil
}

// ComputeSentinels returns the value of each declared sentinel by comparing
// oldObj and newObj.
//
// Returns nil when declared is empty (common path — no allocation).
func ComputeSentinels(declared []string, oldObj, newObj metav1.Object) map[string]string {
	if len(declared) == 0 {
		return nil
	}
	result := make(map[string]string, len(declared))
	for _, name := range declared {
		result[name] = computeSentinel(name, oldObj, newObj)
	}
	return result
}

func computeSentinel(name string, old, new metav1.Object) string {
	switch Sentinel(name) {
	case GenerationChanged:
		return boolStr(old.GetGeneration() != new.GetGeneration())
	case LabelsChanged:
		return boolStr(!reflect.DeepEqual(old.GetLabels(), new.GetLabels()))
	case AnnotationsChanged:
		return boolStr(!reflect.DeepEqual(old.GetAnnotations(), new.GetAnnotations()))
	case DeletionStarted:
		return boolStr(old.GetDeletionTimestamp() == nil && new.GetDeletionTimestamp() != nil)
	case FinalizersChanged:
		return boolStr(!reflect.DeepEqual(old.GetFinalizers(), new.GetFinalizers()))
	case NameChanged:
		return boolStr(old.GetName() != new.GetName())
	case NamespaceChanged:
		return boolStr(old.GetNamespace() != new.GetNamespace())
	case GenerateNameChanged:
		return boolStr(old.GetGenerateName() != new.GetGenerateName())
	case UIDChanged:
		return boolStr(old.GetUID() != new.GetUID())
	case ResourceVersionChanged:
		return boolStr(old.GetResourceVersion() != new.GetResourceVersion())
	case CreationTimestampChanged:
		return boolStr(old.GetCreationTimestamp() != new.GetCreationTimestamp())
	case DeletionGracePeriodSecondsChanged:
		return boolStr(!reflect.DeepEqual(old.GetDeletionGracePeriodSeconds(), new.GetDeletionGracePeriodSeconds()))
	case OwnerReferenceChanged:
		return boolStr(!reflect.DeepEqual(old.GetOwnerReferences(), new.GetOwnerReferences()))
	case ManagedFieldsChanged:
		return boolStr(!reflect.DeepEqual(old.GetManagedFields(), new.GetManagedFields()))
	default:
		return ""
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
