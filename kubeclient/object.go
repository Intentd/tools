package kubeclient

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// Object is a Kubernetes API object: metadata and type information.
type Object interface {
	metav1.Object
	runtime.Object
}

// ObjectList is a Kubernetes API list: list metadata and type information.
type ObjectList interface {
	metav1.ListInterface
	runtime.Object
}

// ObjectKey identifies an object by namespace and name.
type ObjectKey = types.NamespacedName
