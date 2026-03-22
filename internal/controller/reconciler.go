package controller

import (
	"context"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var sandboxClaimGVR = schema.GroupVersionResource{
	Group:    "agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxclaims",
}

// SandboxReconciler watches SandboxClaim resources and manages their lifecycle.
type SandboxReconciler struct {
	client    dynamic.Interface
	namespace string
	logger    *slog.Logger
	metrics   *ControllerMetrics
	gcTimeout time.Duration
}

// NewSandboxReconciler creates a reconciler for sandbox lifecycle management.
func NewSandboxReconciler(client dynamic.Interface, namespace string, logger *slog.Logger, opts ...ReconcilerOption) *SandboxReconciler {
	r := &SandboxReconciler{
		client:    client,
		namespace: namespace,
		logger:    logger,
		gcTimeout: 10 * time.Minute, // default
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ReconcilerOption configures the SandboxReconciler.
type ReconcilerOption func(*SandboxReconciler)

// WithMetrics sets the controller metrics instance.
func WithMetrics(m *ControllerMetrics) ReconcilerOption {
	return func(r *SandboxReconciler) { r.metrics = m }
}

// WithGCTimeout sets the GC cleanup timeout.
func WithGCTimeout(d time.Duration) ReconcilerOption {
	return func(r *SandboxReconciler) { r.gcTimeout = d }
}

// Start begins watching SandboxClaim events and runs periodic GC.
func (r *SandboxReconciler) Start(ctx context.Context) error {
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		r.client, 30*time.Second, r.namespace, func(opts *metav1.ListOptions) {
			opts.LabelSelector = "app.kubernetes.io/managed-by=opencode-scale"
		},
	)

	informer := factory.ForResource(sandboxClaimGVR).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return
			}
			phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
			r.logger.Info("sandbox claim created",
				"name", u.GetName(),
				"namespace", u.GetNamespace(),
			)
			if r.metrics != nil && phase != "" {
				r.metrics.RecordClaimEvent(ctx, phase)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			u, ok := newObj.(*unstructured.Unstructured)
			if !ok {
				return
			}
			phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")
			r.logger.Info("sandbox claim updated",
				"name", u.GetName(),
				"phase", phase,
			)
			if r.metrics != nil && phase != "" {
				r.metrics.RecordClaimEvent(ctx, phase)
			}
		},
		DeleteFunc: func(obj interface{}) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return
			}
			r.logger.Info("sandbox claim deleted", "name", u.GetName())
		},
	})

	// Start informer in background.
	go informer.Run(ctx.Done())

	// Wait for cache sync.
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		r.logger.Warn("failed to sync sandbox claim cache")
	}

	// Run GC loop.
	r.runGCLoop(ctx)

	return nil
}

// runGCLoop periodically scans for orphaned sandbox claims and cleans them up.
func (r *SandboxReconciler) runGCLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.cleanupOrphaned(ctx)
		}
	}
}

// cleanupOrphaned finds sandbox claims that have been in a terminal state
// for too long and deletes them.
func (r *SandboxReconciler) cleanupOrphaned(ctx context.Context) {
	list, err := r.client.Resource(sandboxClaimGVR).Namespace(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=opencode-scale",
	})
	if err != nil {
		r.logger.Error("failed to list sandbox claims for GC", "error", err)
		return
	}

	cutoff := time.Now().Add(-r.gcTimeout)

	for _, item := range list.Items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		if phase != "Failed" && phase != "Completed" {
			continue
		}

		created := item.GetCreationTimestamp().Time
		if created.Before(cutoff) {
			r.logger.Info("cleaning up orphaned sandbox claim",
				"name", item.GetName(),
				"phase", phase,
				"age", time.Since(created).Round(time.Second),
			)
			err := r.client.Resource(sandboxClaimGVR).Namespace(r.namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
			if err != nil {
				r.logger.Error("failed to delete orphaned sandbox claim",
					"name", item.GetName(),
					"error", err,
				)
			} else if r.metrics != nil {
				r.metrics.RecordGCDeletion(ctx)
			}
		}
	}
}
