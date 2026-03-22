package pool

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

var sandboxClaimGVR = schema.GroupVersionResource{
	Group:    "agents.x-k8s.io",
	Version:  "v1alpha1",
	Resource: "sandboxclaims",
}

// K8sSandboxProvider implements SandboxProvider using the Agent Sandbox CRDs.
// It creates SandboxClaim resources and waits for them to be fulfilled.
type K8sSandboxProvider struct {
	client       dynamic.Interface
	namespace    string
	templateName string
	port         int
}

// NewK8sSandboxProvider creates a provider that manages sandbox lifecycle via K8s CRDs.
func NewK8sSandboxProvider(client dynamic.Interface, namespace, templateName string, port int) *K8sSandboxProvider {
	return &K8sSandboxProvider{
		client:       client,
		namespace:    namespace,
		templateName: templateName,
		port:         port,
	}
}

// CreateSandbox creates a SandboxClaim CR and waits for the sandbox to become ready.
// Returns the sandbox name and its in-cluster service FQDN.
func (p *K8sSandboxProvider) CreateSandbox(ctx context.Context) (string, string, error) {
	name := fmt.Sprintf("oc-%d", time.Now().UnixMilli())

	claim := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "agents.x-k8s.io/v1alpha1",
			"kind":       "SandboxClaim",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": p.namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "opencode-scale",
				},
			},
			"spec": map[string]interface{}{
				"templateRef": map[string]interface{}{
					"name": p.templateName,
				},
			},
		},
	}

	_, err := p.client.Resource(sandboxClaimGVR).Namespace(p.namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		return "", "", fmt.Errorf("creating sandbox claim: %w", err)
	}

	// Wait for the sandbox to become ready.
	var fqdn string
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		got, getErr := p.client.Resource(sandboxClaimGVR).Namespace(p.namespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return false, nil // Retry on transient errors.
		}

		phase, found, _ := unstructured.NestedString(got.Object, "status", "phase")
		if !found {
			return false, nil
		}
		if phase == "Ready" {
			// Build the service FQDN from the sandbox name.
			sandboxName, _, _ := unstructured.NestedString(got.Object, "status", "sandboxName")
			if sandboxName == "" {
				sandboxName = name
			}
			fqdn = fmt.Sprintf("%s.%s.svc.cluster.local:%d", sandboxName, p.namespace, p.port)
			return true, nil
		}
		if phase == "Failed" {
			msg, _, _ := unstructured.NestedString(got.Object, "status", "message")
			return false, fmt.Errorf("sandbox claim failed: %s", msg)
		}
		return false, nil
	})
	if err != nil {
		// Clean up the claim on failure.
		_ = p.client.Resource(sandboxClaimGVR).Namespace(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return "", "", fmt.Errorf("waiting for sandbox ready: %w", err)
	}

	return name, fqdn, nil
}

// DeleteSandbox deletes the SandboxClaim CR, which triggers sandbox cleanup.
func (p *K8sSandboxProvider) DeleteSandbox(ctx context.Context, name string) error {
	err := p.client.Resource(sandboxClaimGVR).Namespace(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("deleting sandbox claim: %w", err)
	}
	return nil
}
