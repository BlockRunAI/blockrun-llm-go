package blockrun

// Modal Sandbox — POST /v1/modal/{path}.
//
// Pay-per-call sandboxed cloud compute: create a sandbox, run commands,
// tear it down. sandbox/create $0.01 (CPU; $0.05 with GPU); exec / status /
// terminate $0.001 each. Methods live on *LLMClient and pay automatically
// via x402.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Modal calls the Modal sandbox compute API (generic escape hatch, POST).
//
// path is one of: "sandbox/create", "sandbox/exec", "sandbox/status",
// "sandbox/terminate".
func (c *LLMClient) Modal(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, &ValidationError{Field: "path", Message: "Path is required"}
	}
	if body == nil {
		body = map[string]any{}
	}

	respBytes, err := c.doRequest(ctx, "/v1/modal/"+path, body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// ModalSandboxCreate creates a sandboxed compute environment
// ($0.01 CPU / $0.05 GPU).
//
// Common body fields: image ("python:3.11"), gpu (optional GPU type),
// timeout. The response carries a sandbox_id for exec/status/terminate.
func (c *LLMClient) ModalSandboxCreate(ctx context.Context, body map[string]any) (map[string]any, error) {
	return c.Modal(ctx, "sandbox/create", body)
}

// ModalSandboxExec executes a command inside a running sandbox and returns
// stdout/stderr ($0.001).
func (c *LLMClient) ModalSandboxExec(ctx context.Context, sandboxID string, command []string) (map[string]any, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return nil, &ValidationError{Field: "sandboxID", Message: "Sandbox ID is required"}
	}
	if len(command) == 0 {
		return nil, &ValidationError{Field: "command", Message: "Command is required"}
	}
	return c.Modal(ctx, "sandbox/exec", map[string]any{
		"sandbox_id": sandboxID,
		"command":    command,
	})
}

// ModalSandboxStatus checks a sandbox's status ($0.001).
func (c *LLMClient) ModalSandboxStatus(ctx context.Context, sandboxID string) (map[string]any, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return nil, &ValidationError{Field: "sandboxID", Message: "Sandbox ID is required"}
	}
	return c.Modal(ctx, "sandbox/status", map[string]any{"sandbox_id": sandboxID})
}

// ModalSandboxTerminate terminates a sandbox ($0.001).
func (c *LLMClient) ModalSandboxTerminate(ctx context.Context, sandboxID string) (map[string]any, error) {
	if strings.TrimSpace(sandboxID) == "" {
		return nil, &ValidationError{Field: "sandboxID", Message: "Sandbox ID is required"}
	}
	return c.Modal(ctx, "sandbox/terminate", map[string]any{"sandbox_id": sandboxID})
}
