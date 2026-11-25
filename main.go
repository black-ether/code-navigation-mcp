package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultOutputBytes = 8192
	defaultReadLines   = 100
	defaultTimeout     = 5 * time.Second
	maxDepth           = 4
)

type config struct {
	workspace, codecore, codegos string
	workspaceInfo                os.FileInfo
	maxOutputBytes, maxReadLines int
	timeout                      time.Duration
}

func parseConfig(args []string, diagnostics io.Writer) (config, error) {
	var cfg config
	var families, core, gos string
	flags := flag.NewFlagSet("code-navigation-mcp", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&cfg.workspace, "workspace", "", "required workspace root for source paths")
	flags.StringVar(&families, "tools", "both", "tool families to expose: codecore, codegos, or both")
	flags.StringVar(&core, "codecore", "codecore", "Codecore executable path or PATH name")
	flags.StringVar(&gos, "codegos", "codegos", "Codegos executable path or PATH name")
	flags.IntVar(&cfg.maxOutputBytes, "max-output-bytes", defaultOutputBytes, "maximum UTF-8 bytes per tool response (at least 256)")
	flags.IntVar(&cfg.maxReadLines, "max-read-lines", defaultReadLines, "maximum inclusive line count per source read")
	flags.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "maximum duration per operation")
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if flags.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected positional arguments")
	}
	if cfg.workspace == "" {
		return cfg, fmt.Errorf("--workspace is required")
	}
	if cfg.maxOutputBytes < 256 || cfg.maxReadLines < 1 || cfg.timeout <= 0 {
		return cfg, fmt.Errorf("choose --max-output-bytes >= 256, --max-read-lines >= 1, and --timeout > 0")
	}
	if families != "both" && families != "codecore" && families != "codegos" {
		return cfg, fmt.Errorf("--tools must be codecore, codegos, or both")
	}

	root, err := filepath.Abs(cfg.workspace)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return cfg, fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return cfg, fmt.Errorf("open workspace: %w", err)
	}
	if !info.IsDir() {
		return cfg, fmt.Errorf("workspace must be a directory")
	}
	cfg.workspace = root
	cfg.workspaceInfo = info

	if families == "both" || families == "codecore" {
		cfg.codecore, err = resolveExecutable(core)
		if err != nil {
			return cfg, fmt.Errorf("resolve Codecore executable: %w", err)
		}
	}
	if families == "both" || families == "codegos" {
		cfg.codegos, err = resolveExecutable(gos)
		if err != nil {
			return cfg, fmt.Errorf("resolve Codegos executable: %w", err)
		}
	}
	return cfg, nil
}

func resolveExecutable(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("executable name is empty")
	}
	if filepath.Base(name) != name {
		var err error
		name, err = filepath.Abs(name)
		if err != nil {
			return "", err
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return "", fmt.Errorf("expected a Windows .exe: %q", path)
	}
	return path, nil
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "code-navigation-mcp:", err)
		os.Exit(2)
	}
	if err := newServer(cfg).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "code-navigation-mcp:", err)
		os.Exit(1)
	}
}
