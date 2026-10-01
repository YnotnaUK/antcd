package k8s

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "antcd"
	RepoLabel      = "antcd.io/repo"
	TargetLabel    = "antcd.io/target"

	// DefaultNamespace is used for namespaced resources that do not declare one.
	DefaultNamespace = "default"
)

// Scope identifies the repo and target that own a set of applied resources.
type Scope struct {
	Repo   string
	Target string
}

func (s Scope) selector() string {
	return fmt.Sprintf("%s=%s,%s=%s,%s=%s", ManagedByLabel, ManagedByValue, RepoLabel, s.Repo, TargetLabel, s.Target)
}

func (s Scope) applyLabels(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[ManagedByLabel] = ManagedByValue
	labels[RepoLabel] = s.Repo
	labels[TargetLabel] = s.Target
	obj.SetLabels(labels)
}

type ResourceID struct {
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

type Applier struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
	// prunableGVRs lists every resource type that may hold owned resources.
	// It is a field so tests can replace cluster discovery.
	prunableGVRs func() ([]schema.GroupVersionResource, error)
}

func NewApplier() (*Applier, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("could not load cluster config: %w", err)
		}
	}

	// Pruning lists every resource type, which the default client limits (5 QPS) make very slow.
	config.QPS = 50
	config.Burst = 100

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	discClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}

	cachedDiscovery := memory.NewMemCacheClient(discClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscovery)

	return &Applier{
		dynamicClient: dynClient,
		mapper:        restMapper,
		prunableGVRs: cachedFor(discoveryCacheTTL, func() ([]schema.GroupVersionResource, error) {
			return discoverPrunable(discClient)
		}),
	}, nil
}

const (
	discoveryCacheTTL = time.Minute
	pruneConcurrency  = 16
)

// cachedFor memoises successful results of fn for ttl. It is safe for concurrent use,
// as repos reconcile in parallel and share one Applier.
func cachedFor(ttl time.Duration, fn func() ([]schema.GroupVersionResource, error)) func() ([]schema.GroupVersionResource, error) {
	var (
		mu      sync.Mutex
		cached  []schema.GroupVersionResource
		fetched time.Time
	)
	return func() ([]schema.GroupVersionResource, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached != nil && time.Since(fetched) < ttl {
			return cached, nil
		}
		res, err := fn()
		if err != nil {
			return nil, err
		}
		cached, fetched = res, time.Now()
		return cached, nil
	}
}

// discoverPrunable returns every preferred resource type that supports list and delete.
func discoverPrunable(disc discovery.DiscoveryInterface) ([]schema.GroupVersionResource, error) {
	lists, err := disc.ServerPreferredResources()
	if len(lists) == 0 && err != nil {
		return nil, err
	}

	// A partial discovery failure (e.g. an unavailable aggregated API) still yields usable results.
	var gvrs []schema.GroupVersionResource
	for _, list := range lists {
		gv, perr := schema.ParseGroupVersion(list.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") {
				continue
			}
			verbs := sets.New(r.Verbs...)
			if verbs.Has("list") && verbs.Has("delete") {
				gvrs = append(gvrs, gv.WithResource(r.Name))
			}
		}
	}
	return gvrs, nil
}

// ApplyManifest injects the antcd labels for scope and applies using Server-Side Apply.
// Namespaced resources without a namespace are applied to DefaultNamespace.
func (a *Applier) ApplyManifest(ctx context.Context, yamlData []byte, scope Scope) (*ResourceID, error) {
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}

	_, gvk, err := dec.Decode(yamlData, nil, obj)
	if err != nil {
		return nil, fmt.Errorf("decoding yaml: %w", err)
	}

	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("mapping GVK to resource: %w", err)
	}

	scope.applyLabels(obj)

	var dr dynamic.ResourceInterface
	ns := obj.GetNamespace()
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns == "" {
			ns = DefaultNamespace
			obj.SetNamespace(ns)
		}
		dr = a.dynamicClient.Resource(mapping.Resource).Namespace(ns)
	} else {
		dr = a.dynamicClient.Resource(mapping.Resource)
		ns = ""
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshaling json: %w", err)
	}

	force := true
	_, err = dr.Patch(ctx, obj.GetName(), "application/apply-patch+yaml", data, metav1.PatchOptions{
		FieldManager: "antcd",
		Force:        &force,
	})
	if err != nil {
		return nil, fmt.Errorf("applying %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return &ResourceID{
		GVR:       mapping.Resource,
		Namespace: ns,
		Name:      obj.GetName(),
	}, nil
}

// Prune deletes resources owned by scope that are no longer in Git.
// Resources belonging to other repos or targets are never touched.
func (a *Applier) Prune(ctx context.Context, scope Scope, applied []ResourceID) error {
	appliedMap := make(map[string]bool)
	uniqueGVRs := make(map[schema.GroupVersionResource]bool)

	for _, res := range applied {
		key := fmt.Sprintf("%s/%s/%s", res.GVR.String(), res.Namespace, res.Name)
		appliedMap[key] = true
		uniqueGVRs[res.GVR] = true
	}

	// Scan every resource type, not just those applied now, so that removing the
	// last resource of a kind (or all of a target's resources) is still pruned.
	if a.prunableGVRs != nil {
		discovered, err := a.prunableGVRs()
		if err != nil {
			log.Printf("[WARN] Prune: resource discovery failed, only scanning applied kinds: %v", err)
		}
		for _, gvr := range discovered {
			uniqueGVRs[gvr] = true
		}
	}

	// Resource types are scanned concurrently; there can be well over a hundred.
	sem := make(chan struct{}, pruneConcurrency)
	var wg sync.WaitGroup
	for gvr := range uniqueGVRs {
		wg.Add(1)
		sem <- struct{}{}
		go func(gvr schema.GroupVersionResource) {
			defer wg.Done()
			defer func() { <-sem }()
			a.pruneGVR(ctx, gvr, scope, appliedMap)
		}(gvr)
	}
	wg.Wait()
	return nil
}

func (a *Applier) pruneGVR(ctx context.Context, gvr schema.GroupVersionResource, scope Scope, applied map[string]bool) {
	list, err := a.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{
		LabelSelector: scope.selector(),
	})
	if err != nil {
		log.Printf("[WARN] Prune: Failed to list %s: %v", gvr.Resource, err)
		return
	}

	for _, item := range list.Items {
		key := fmt.Sprintf("%s/%s/%s", gvr.String(), item.GetNamespace(), item.GetName())
		if applied[key] {
			continue
		}

		log.Printf("[PRUNE] Resource no longer in Git: deleting %s/%s", gvr.Resource, item.GetName())
		var dr dynamic.ResourceInterface
		if item.GetNamespace() != "" {
			dr = a.dynamicClient.Resource(gvr).Namespace(item.GetNamespace())
		} else {
			dr = a.dynamicClient.Resource(gvr)
		}
		if err := dr.Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			log.Printf("[ERROR] Pruning %s/%s failed: %v", gvr.Resource, item.GetName(), err)
		}
	}
}
