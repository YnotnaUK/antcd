package k8s

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var cmGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

func configMap(ns, name string, labels map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("ConfigMap")
	obj.SetNamespace(ns)
	obj.SetName(name)
	obj.SetLabels(labels)
	return obj
}

func scopeLabels(s Scope) map[string]string {
	return map[string]string{ManagedByLabel: ManagedByValue, RepoLabel: s.Repo, TargetLabel: s.Target}
}

func TestApplyLabelsPreservesExisting(t *testing.T) {
	obj := configMap("ns", "x", map[string]string{"keep": "me", ManagedByLabel: "other"})
	Scope{Repo: "r", Target: "t"}.applyLabels(obj)

	got := obj.GetLabels()
	want := map[string]string{"keep": "me", ManagedByLabel: ManagedByValue, RepoLabel: "r", TargetLabel: "t"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("label %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestPruneScopedToRepoAndTarget(t *testing.T) {
	mine := Scope{Repo: "r1", Target: "app"}
	otherTarget := Scope{Repo: "r1", Target: "infra"}
	otherRepo := Scope{Repo: "r2", Target: "app"}

	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{cmGVR: "ConfigMapList"},
		configMap("a", "keep", scopeLabels(mine)),
		configMap("a", "stale", scopeLabels(mine)),
		configMap("b", "stale-other-ns", scopeLabels(mine)),
		configMap("a", "other-target", scopeLabels(otherTarget)),
		configMap("a", "other-repo", scopeLabels(otherRepo)),
		configMap("a", "unmanaged", nil),
	)
	applier := &Applier{dynamicClient: client}

	applied := []ResourceID{{GVR: cmGVR, Namespace: "a", Name: "keep"}}
	if err := applier.Prune(context.Background(), mine, applied); err != nil {
		t.Fatal(err)
	}

	list, err := client.Resource(cmGVR).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range list.Items {
		got[it.GetNamespace()+"/"+it.GetName()] = true
	}
	for _, want := range []string{"a/keep", "a/other-target", "a/other-repo", "a/unmanaged"} {
		if !got[want] {
			t.Errorf("%s was deleted but should remain", want)
		}
	}
	for _, gone := range []string{"a/stale", "b/stale-other-ns"} {
		if got[gone] {
			t.Errorf("%s should have been pruned", gone)
		}
	}
}
