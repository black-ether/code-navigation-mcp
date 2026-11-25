package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func workspaceRelative(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path must be within the workspace")
	}
	return rel, nil
}

func (cfg config) sourcePath(ctx context.Context, path string, allowDirectory bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsLocal(path) {
			return "", fmt.Errorf("path must be within the workspace")
		}
		path = filepath.Join(cfg.workspace, path)
	}
	path = filepath.Clean(path)
	if _, err := workspaceRelative(cfg.workspace, path); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve source path: %w", err)
	}
	rel, err := workspaceRelative(cfg.workspace, resolved)
	if err != nil {
		return "", err
	}
	// Rel can compare case-insensitively on Windows, including on volumes
	// where two distinct directories differ only in case.
	ancestor := resolved
	for remainder := rel; remainder != "."; remainder = filepath.Dir(remainder) {
		ancestor = filepath.Dir(ancestor)
	}
	ancestorInfo, err := os.Stat(ancestor)
	if err != nil || !os.SameFile(cfg.workspaceInfo, ancestorInfo) {
		return "", fmt.Errorf("resolved path must be within the workspace")
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect source path: %w", err)
	}
	if info.IsDir() && allowDirectory {
		if err := checkDirectory(ctx, resolved); err != nil {
			return "", err
		}
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path must be a regular source file")
	}
	return resolved, nil
}

// Codecore enumerates files recursively and opens matching paths later.
// Reject unsafe entries at every depth before invoking it.
func checkDirectory(ctx context.Context, path string) error {
	return filepath.WalkDir(path, func(item string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("inspect source directory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		kind := entry.Type()
		if !kind.IsRegular() && kind != fs.ModeDir {
			rel, _ := filepath.Rel(path, item)
			return fmt.Errorf("directory contains a linked or special entry %q; use a narrower path", rel)
		}
		return nil
	})
}

func (cfg config) run(ctx context.Context, binary, path string, allowDirectory bool, narrow string, arguments func(string) []string) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	resolved, err := cfg.sourcePath(ctx, path, allowDirectory)
	if err != nil && ctx.Err() == nil {
		return textResult(limitText(err.Error(), cfg.maxOutputBytes, "Use a shorter workspace-relative path.", false), true), nil
	}
	if ctx.Err() != nil {
		return cfg.interrupted(ctx.Err(), narrow)
	}
	if allowDirectory {
		// Codecore's --no-tests checks all path components. Give it a path
		// relative to the workspace so only in-workspace names affect it.
		resolved, err = workspaceRelative(cfg.workspace, resolved)
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.CommandContext(ctx, binary, arguments(resolved)...)
	cmd.Dir = cfg.workspace
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := limitedBuffer{limit: cfg.maxOutputBytes}
	stderr := limitedBuffer{limit: cfg.maxOutputBytes}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return cfg.interrupted(ctx.Err(), narrow)
		}
		message := fmt.Sprintf("%s: %v", filepath.Base(binary), err)
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			message += "\n" + detail
		}
		return textResult(limitText(message, cfg.maxOutputBytes, narrow, stderr.overflow), true), nil
	}

	output := stdout.String()
	if warning := strings.TrimSpace(stderr.String()); warning != "" {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += warning
	}
	if output == "" {
		output = "(No outline rows.)"
	}
	return textResult(limitText(output, cfg.maxOutputBytes, narrow, stdout.overflow || stderr.overflow), false), nil
}

func (cfg config) interrupted(err error, narrow string) (*mcp.CallToolResult, error) {
	if errors.Is(err, context.DeadlineExceeded) {
		return textResult("Execution timed out. "+narrow, true), nil
	}
	return nil, err
}

func textResult(text string, isError bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: isError}
}

type limitedBuffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	keep := min(len(p), b.limit-b.data.Len())
	_, _ = b.data.Write(p[:keep])
	b.overflow = b.overflow || keep < len(p)
	// Discard overflow while letting the CLI finish normally.
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return b.data.String()
}

func limitText(s string, limit int, narrow string, incomplete bool) string {
	if !incomplete && len(s) <= limit {
		s = strings.ToValidUTF8(s, "\uFFFD")
		if len(s) <= limit {
			return s
		}
	}
	note := "[Result incomplete. Output limit reached. " + narrow + "]"
	budget := limit - len(note) - 1
	prefix := s[:min(len(s), budget)]
	if last := strings.LastIndexByte(prefix, '\n'); last >= 0 {
		prefix = strings.ToValidUTF8(prefix[:last+1], "?")
		return prefix + note
	}
	return strings.ToValidUTF8(prefix, "?") + "\n" + note
}
