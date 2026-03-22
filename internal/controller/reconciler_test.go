package controller

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newFakeClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			sandboxClaimGVR: "SandboxClaimList",
		},
		objects...,
	)
}

func makeSandboxClaim(name, phase string, created time.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "agents.x-k8s.io/v1alpha1",
			"kind":       "SandboxClaim",
			"metadata": map[string]interface{}{
				"name":              name,
				"namespace":         "test-ns",
				"creationTimestamp": created.Format(time.RFC3339),
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "opencode-scale",
				},
			},
			"status": map[string]interface{}{
				"phase": phase,
			},
		},
	}
	obj.SetCreationTimestamp(metav1.NewTime(created))
	return obj
}

func TestCleanupOrphaned_DeletesOldTerminal(t *testing.T) {
	oldTime := time.Now().Add(-20 * time.Minute)
	claim := makeSandboxClaim("old-failed", "Failed", oldTime)

	client := newFakeClient(claim)
	r := NewSandboxReconciler(client, "test-ns", testLogger(),
		WithGCTimeout(10*time.Minute),
	)

	r.cleanupOrphaned(context.Background())

	// Verify it was deleted.
	list, err := client.Resource(sandboxClaimGVR).Namespace("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected 0 items after cleanup, got %d", len(list.Items))
	}
}

func TestCleanupOrphaned_SkipsRecentTerminal(t *testing.T) {
	recentTime := time.Now().Add(-2 * time.Minute)
	claim := makeSandboxClaim("recent-completed", "Completed", recentTime)

	client := newFakeClient(claim)
	r := NewSandboxReconciler(client, "test-ns", testLogger(),
		WithGCTimeout(10*time.Minute),
	)

	r.cleanupOrphaned(context.Background())

	// Should not be deleted (too recent).
	list, err := client.Resource(sandboxClaimGVR).Namespace("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item (not deleted), got %d", len(list.Items))
	}
}

func TestCleanupOrphaned_SkipsRunning(t *testing.T) {
	oldTime := time.Now().Add(-20 * time.Minute)
	claim := makeSandboxClaim("old-running", "Running", oldTime)

	client := newFakeClient(claim)
	r := NewSandboxReconciler(client, "test-ns", testLogger(),
		WithGCTimeout(10*time.Minute),
	)

	r.cleanupOrphaned(context.Background())

	// Running claims should never be cleaned up.
	list, err := client.Resource(sandboxClaimGVR).Namespace("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item (running, not deleted), got %d", len(list.Items))
	}
}

func TestCleanupOrphaned_EmptyList(t *testing.T) {
	client := newFakeClient()
	r := NewSandboxReconciler(client, "test-ns", testLogger(),
		WithGCTimeout(10*time.Minute),
	)

	// Should not panic on empty list.
	r.cleanupOrphaned(context.Background())
}

func TestNewSandboxReconciler_Defaults(t *testing.T) {
	client := newFakeClient()
	r := NewSandboxReconciler(client, "test-ns", testLogger())

	if r.gcTimeout != 10*time.Minute {
		t.Fatalf("expected default gcTimeout 10m, got %v", r.gcTimeout)
	}
	if r.metrics != nil {
		t.Fatal("expected nil metrics by default")
	}
}

func TestNewSandboxReconciler_WithOptions(t *testing.T) {
	client := newFakeClient()
	r := NewSandboxReconciler(client, "test-ns", testLogger(),
		WithGCTimeout(5*time.Minute),
	)

	if r.gcTimeout != 5*time.Minute {
		t.Fatalf("expected gcTimeout 5m, got %v", r.gcTimeout)
	}
}
