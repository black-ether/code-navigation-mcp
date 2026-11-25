package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Exercise the CLI contract before advertising tools. Neither CLI offers a
// stable version option, and help alone cannot confirm recursive retrieval.
func (cfg config) verifyCompatibility() error {
	dir, err := os.MkdirTemp("", "code-navigation-probe-")
	if err != nil {
		return fmt.Errorf("create CLI compatibility fixture: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		return fmt.Errorf("create CLI compatibility fixture: %w", err)
	}
	path := filepath.Join("nested", "probe.go")
	source := "package probe\n// Probe heading orthogonal\nfunc navigatorprobe() int {\n\treturn 42\n}\n"
	if err := os.WriteFile(filepath.Join(dir, path), []byte(source), 0600); err != nil {
		return fmt.Errorf("write CLI compatibility fixture: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	check := func(binary string, args, expected []string) error {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = dir
		cmd.WaitDelay = 250 * time.Millisecond
		out := limitedBuffer{limit: 4096}
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		for _, fragment := range expected {
			if !strings.Contains(out.String(), fragment) {
				return fmt.Errorf("probe output is missing %q", fragment)
			}
		}
		return nil
	}

	if cfg.codecore != "" {
		search := searchInput{Query: "orthogonal", DefOnly: true, PerFile: true, Word: true, NoTests: true}
		if err := check(cfg.codecore, searchArguments(search, 1, "."), []string{"probe.go [L:3-4]", "func navigatorprobe"}); err != nil {
			return fmt.Errorf("incompatible Codecore %q: ranked recursive search failed (%w). Build revision 26b8bde or use a compatible binary", cfg.codecore, err)
		}
		if err := check(cfg.codecore, []string{"--depth", "1", "--", "."}, []string{"[L:3]", "func navigatorprobe", "[L:4]", "return 42"}); err != nil {
			return fmt.Errorf("incompatible Codecore %q: recursive outline failed (%w). Build revision 26b8bde or use a compatible binary", cfg.codecore, err)
		}
	}
	if cfg.codegos != "" {
		for _, probe := range []struct {
			args, expected []string
		}{
			{[]string{path, "--plain"}, []string{"2 | Probe heading", "[block 2-5]"}},
			{[]string{path, "go", "2", "--plain"}, []string{"2 | Probe heading", "4 |", "return 42"}},
			{[]string{path, "show", "4-4", "--plain"}, []string{"4 |", "return 42"}},
		} {
			if err := check(cfg.codegos, probe.args, probe.expected); err != nil {
				return fmt.Errorf("incompatible Codegos %q: comment navigation or numbered source read failed (%w). Build revision bc789ba or use a compatible binary", cfg.codegos, err)
			}
		}
	}
	return nil
}
