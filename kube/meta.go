package kube

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/validate/content"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ── Labels and annotations ─────────────────────────────────────────────────

// EnsureLabel sets label key to value on obj, or removes it when value is
// empty. It reports whether the labels changed.
func EnsureLabel(obj metav1.Object, key, value string) bool {
	labels := obj.GetLabels()
	cur, present := labels[key]
	switch {
	case value == "" && !present, value != "" && present && cur == value:
		return false
	case value == "":
		delete(labels, key)
	default:
		if labels == nil {
			labels = map[string]string{}
		}
		labels[key] = value
	}
	obj.SetLabels(labels)
	return true
}

// EnsureAnnotationUnset sets annotation key to value on obj unless it already
// has a non-empty value, which is kept. It reports whether it was set.
func EnsureAnnotationUnset(obj metav1.Object, key, value string) bool {
	ann := obj.GetAnnotations()
	if ann[key] != "" {
		return false
	}
	if ann == nil {
		ann = map[string]string{}
	}
	ann[key] = value
	obj.SetAnnotations(ann)
	return true
}

// MergeLabels sets every label in extra on obj, overwriting values that
// differ. It reports whether any label changed.
func MergeLabels(obj metav1.Object, extra map[string]string) bool {
	merged, changed := merge(obj.GetLabels(), extra)
	if changed {
		obj.SetLabels(merged)
	}
	return changed
}

// MergeAnnotations sets every annotation in extra on obj, overwriting values
// that differ. It reports whether any annotation changed.
func MergeAnnotations(obj metav1.Object, extra map[string]string) bool {
	merged, changed := merge(obj.GetAnnotations(), extra)
	if changed {
		obj.SetAnnotations(merged)
	}
	return changed
}

func merge(m, extra map[string]string) (map[string]string, bool) {
	changed := false
	for k, v := range extra {
		if cur, ok := m[k]; ok && cur == v {
			continue
		}
		if m == nil {
			m = make(map[string]string, len(extra))
		}
		m[k] = v
		changed = true
	}
	return m, changed
}

// SetJSONAnnotation stores v as JSON in annotation key on obj. A value that
// does not marshal is not stored.
func SetJSONAnnotation(obj *unstructured.Unstructured, key string, v map[string]interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 1)
	}
	ann[key] = string(b)
	obj.SetAnnotations(ann)
}

// JSONAnnotation decodes the JSON object in annotation key of an object
// map; nil when it is absent or not a JSON object.
func JSONAnnotation(obj map[string]interface{}, key string) map[string]interface{} {
	meta, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	annotations, ok := meta["annotations"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := annotations[key].(string)
	if !ok || raw == "" {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// CheckReservedDomain rejects keys in m under domain: domain/… and
// <sub>.domain/…. field names the map in the error.
func CheckReservedDomain(field string, m map[string]string, domain string) error {
	var bad []string
	for k := range m {
		prefix, _, ok := strings.Cut(k, "/")
		if !ok {
			continue
		}
		if prefix == domain || strings.HasSuffix(prefix, "."+domain) {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("%s %s: the %s domain is reserved for the runtime", field, strings.Join(bad, ", "), domain)
}

// ── Validation ─────────────────────────────────────────────────────────────

// ValidateLabelMap checks every entry in labels as a Kubernetes label:
// both the key and the value must be valid.
func ValidateLabelMap(labels map[string]string) error {
	for k, v := range labels {
		if errs := content.IsLabelKey(k); len(errs) > 0 {
			return fmt.Errorf("invalid label key %q: %s", k, strings.Join(errs, "; "))
		}
		if errs := content.IsLabelValue(v); len(errs) > 0 {
			return fmt.Errorf("invalid label value for key %q: %s", k, strings.Join(errs, "; "))
		}
	}
	return nil
}

// ValidateAnnotationKeys checks annotation keys against the label-key rules,
// which Kubernetes also applies to annotation keys. Values are not checked:
// annotation values are unconstrained strings.
func ValidateAnnotationKeys(annotations map[string]string) error {
	for k := range annotations {
		if errs := content.IsLabelKey(k); len(errs) > 0 {
			return fmt.Errorf("invalid annotation key %q: %s", k, strings.Join(errs, "; "))
		}
	}
	return nil
}

// ValidateLabelSelector reports whether sel is a valid Kubernetes label
// selector (e.g. "app=nginx", "tier in (gold,silver)"), with the parse error
// when it is not.
func ValidateLabelSelector(sel string) (bool, error) {
	if _, err := metav1.ParseToLabelSelector(sel); err != nil {
		return false, err
	}
	return true, nil
}

// IsValidLabelSelector reports whether sel is a valid Kubernetes label
// selector.
func IsValidLabelSelector(sel string) bool {
	_, err := metav1.ParseToLabelSelector(sel)
	return err == nil
}

// ── Finalizers ─────────────────────────────────────────────────────────────

// ContainsFinalizer reports whether o has finalizer.
func ContainsFinalizer(o metav1.Object, finalizer string) bool {
	return slices.Contains(o.GetFinalizers(), finalizer)
}

// AddFinalizer adds finalizer to o; it reports whether o changed.
func AddFinalizer(o metav1.Object, finalizer string) (updated bool) {
	if ContainsFinalizer(o, finalizer) {
		return false
	}
	o.SetFinalizers(append(o.GetFinalizers(), finalizer))
	return true
}

// RemoveFinalizer removes finalizer from o; it reports whether o changed.
func RemoveFinalizer(o metav1.Object, finalizer string) (updated bool) {
	cur := o.GetFinalizers()
	n := 0
	for i := range cur {
		if cur[i] != finalizer {
			cur[n] = cur[i]
			n++
		}
	}
	o.SetFinalizers(cur[:n])
	return n != len(cur)
}

// EnsureFinalizers adds the finalizers in want that o lacks; it reports
// whether o changed.
func EnsureFinalizers(o metav1.Object, want []string) (updated bool) {
	for _, f := range want {
		if AddFinalizer(o, f) {
			updated = true
		}
	}
	return updated
}

// StripFinalizers removes the finalizers in strip from o; it reports whether
// o changed.
func StripFinalizers(o metav1.Object, strip []string) (updated bool) {
	for _, f := range strip {
		if RemoveFinalizer(o, f) {
			updated = true
		}
	}
	return updated
}
