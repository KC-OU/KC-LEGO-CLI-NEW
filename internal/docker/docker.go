// Package docker wraps the docker CLI operations the ModernWMS integration
// needs: exec a python3 script inside a container (the only viable access
// path to wms.db — the container has no bind mount and no sqlite3 binary,
// only python3), copy a file out, list containers, and inspect one.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Exec runs `docker exec -i <container> python3 -`, piping pythonCode on
// stdin, and returns trimmed stdout. On a non-zero exit it returns an error
// wrapping stderr.
func Exec(ctx context.Context, container, pythonCode string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", container, "python3", "-")
	cmd.Stdin = strings.NewReader(pythonCode)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker exec %s: %w: %s", container, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ExecJSON runs Exec and unmarshals stdout as JSON into out.
func ExecJSON(ctx context.Context, container, pythonCode string, out any) error {
	stdout, err := Exec(ctx, container, pythonCode)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(stdout), out); err != nil {
		return fmt.Errorf("parsing docker exec output as JSON: %w (output: %s)", err, stdout)
	}
	return nil
}

// Cp runs `docker cp <src> <dst>`.
func Cp(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", src, dst)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker cp %s %s: %w: %s", src, dst, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Rm runs `docker exec <container> rm -f <path>`, best-effort cleanup.
func Rm(ctx context.Context, container, path string) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", container, "rm", "-f", path)
	return cmd.Run()
}

// Ps runs `docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'`
// and returns its raw table output, matching the original Containers tab
// which streams this directly rather than parsing it.
func Ps(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker ps: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// InspectStatus runs `docker inspect -f '{{.State.Status}}' <container>`.
func InspectStatus(ctx context.Context, container string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", container)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker inspect %s: %w: %s", container, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
