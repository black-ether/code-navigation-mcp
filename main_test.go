package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mockMode = "CODE_NAV_MCP_TEST_SUBPROCESS"

func TestMain(m *testing.M) {
	switch os.Getenv(mockMode) {
	case "args":
		_ = json.NewEncoder(os.Stdout).Encode(os.Args[1:])
	case "ok":
		fmt.Print("[L:2] package example\n[L:5]   nested()\n")
	case "empty":
	case "unsupported":
		fmt.Fprintln(os.Stderr, "error: unsupported file extension .txt")
		os.Exit(2)
	case "warning":
		fmt.Fprintln(os.Stdout, "[L:2] func f() {")
		fmt.Fprintln(os.Stderr, "unbalanced brackets; outline may be incomplete")
	case "flood":
		fmt.Fprintln(os.Stdout, "sample.ps1 (powershell, 502 lines)")
		fmt.Fprint(os.Stdout, "  1 | "+strings.Repeat("資料", 50000))
	case "sleep":
		time.Sleep(10 * time.Second)
	default:
		os.Exit(m.Run())
	}
	os.Exit(0)
}

func createFile(t *testing.T, path, data string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testConfig(t *testing.T) config {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseConfig([]string{"--workspace", t.TempDir(), "--codecore", self, "--codegos", self}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func connect(t *testing.T, transport mcp.Transport) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "local-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if got := session.InitializeResult().ServerInfo.Name; got != "code-navigation-mcp" {
		t.Fatalf("server name = %q", got)
	}
	return session
}

func sessionFor(t *testing.T, cfg config) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(cfg).Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	return connect(t, clientTransport)
}

func call(t *testing.T, session *mcp.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 || res.StructuredContent != nil {
		t.Fatalf("expected one plain text item, got %#v", res)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content %T", res.Content[0])
	}
	return text.Text, res.IsError
}

func TestToolRegistration(t *testing.T) {
	for _, family := range []string{"both", "codecore", "codegos"} {
		t.Run(family, func(t *testing.T) {
			cfg := testConfig(t)
			want := []string{"codecore_outline", "codegos_expand", "codegos_outline", "codegos_read"}
			switch family {
			case "codecore":
				cfg.codegos = ""
				want = want[:1]
			case "codegos":
				cfg.codecore = ""
				want = want[1:]
			}
			res, err := sessionFor(t, cfg).ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, tool := range res.Tools {
				got = append(got, tool.Name)
				if !tool.Annotations.ReadOnlyHint || tool.InputSchema == nil || tool.OutputSchema != nil {
					t.Fatalf("unexpected tool metadata: %#v", tool)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("tools = %v, want %v", got, want)
			}
		})
	}
}

func TestCommandArguments(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv(mockMode, "args")
	session := sessionFor(t, cfg)
	// A path containing shell metacharacters travels as one literal argument.
	name := "-sample with spaces & echo SHOULD_NOT_RUN.go"
	path := createFile(t, filepath.Join(cfg.workspace, name), "")
	for _, tt := range []struct {
		name string
		args map[string]any
		want []string
	}{
		{"codecore_outline", map[string]any{"path": name}, []string{"--depth", "0", "--", path}},
		{"codecore_outline", map[string]any{"path": ".", "depth": 2}, []string{"--depth", "2", "--", cfg.workspace}},
		{"codegos_outline", map[string]any{"path": path}, []string{path, "--plain"}},
		{"codegos_expand", map[string]any{"path": name, "line": 5}, []string{path, "go", "5", "--plain"}},
		{"codegos_read", map[string]any{"path": name, "start": 5, "end": 104}, []string{path, "show", "5-104", "--plain"}},
	} {
		got, failed := call(t, session, tt.name, tt.args)
		var args []string
		if failed || json.Unmarshal([]byte(got), &args) != nil || !reflect.DeepEqual(args, tt.want) {
			t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestInputValidation(t *testing.T) {
	cfg := testConfig(t)
	createFile(t, filepath.Join(cfg.workspace, "x.go"), "")
	session := sessionFor(t, cfg)
	t.Setenv(mockMode, "ok")
	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{"codecore_outline", map[string]any{"path": ".", "depth": -1}},
		{"codecore_outline", map[string]any{"path": ".", "depth": 5}},
		{"codecore_outline", map[string]any{"path": ".", "depth": "2"}},
		{"codegos_outline", map[string]any{}},
		{"codegos_outline", map[string]any{"path": " "}},
		{"codegos_outline", map[string]any{"path": "."}},
		{"codegos_expand", map[string]any{"path": "x.go", "line": 0}},
		{"codegos_expand", map[string]any{"path": "x.go", "line": 1, "full": true}},
		{"codegos_read", map[string]any{"path": "x.go", "start": 0, "end": 1}},
		{"codegos_read", map[string]any{"path": "x.go", "start": 5, "end": 4}},
		{"codegos_read", map[string]any{"path": "x.go", "start": 5, "end": 105}},
	} {
		if got, failed := call(t, session, tt.tool, tt.args); !failed {
			t.Errorf("%s accepted %v: %s", tt.tool, tt.args, got)
		}
	}
}

func TestExecutablePathsAndConfig(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "tool directory with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "mock-tool" + filepath.Ext(self)
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, target := range []string{name, path} {
		cfg, err := parseConfig([]string{"--workspace", dir, "--tools", "codegos", "--codegos", target, "--codecore", "absent.exe",
			"--max-output-bytes", "512", "--max-read-lines", "3", "--timeout", "3s"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.codegos != path || cfg.codecore != "" || cfg.maxOutputBytes != 512 || cfg.maxReadLines != 3 || cfg.timeout != 3*time.Second {
			t.Fatalf("unexpected configuration: %#v", cfg)
		}
		t.Setenv(mockMode, "ok")
		args := map[string]any{"path": createFile(t, filepath.Join(dir, "sample file.go"), "")}
		if got, failed := call(t, sessionFor(t, cfg), "codegos_outline", args); failed || !strings.HasPrefix(got, "[L:2]") {
			t.Fatalf("execution from spaced path: %q", got)
		}
	}
	for _, extra := range [][]string{
		{"--tools", "unknown"}, {"--max-output-bytes", "255"}, {"--max-read-lines", "0"}, {"--timeout", "0s"}, {"--codegos", filepath.Join(dir, "absent.exe")},
	} {
		args := []string{"--workspace", dir, "--tools", "codegos", "--codegos", path}
		if _, err := parseConfig(append(args, extra...), io.Discard); err == nil {
			t.Errorf("accepted invalid configuration %v", extra)
		}
	}
}

func TestWorkspaceBoundaries(t *testing.T) {
	cfg := testConfig(t)
	outside := createFile(t, filepath.Join(t.TempDir(), "secret.go"), "")
	relative, err := filepath.Rel(cfg.workspace, outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, relative, filepath.Join(cfg.workspace, "..", "secret.go")} {
		if _, err := cfg.sourcePath(context.Background(), path, false); err == nil || !strings.Contains(err.Error(), "within the workspace") {
			t.Errorf("expected boundary error for %q, got %v", path, err)
		}
	}
	t.Run("links", func(t *testing.T) {
		link := filepath.Join(cfg.workspace, "linked.go")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("file symlinks unavailable: %v", err)
		}
		if _, err := cfg.sourcePath(context.Background(), link, false); err == nil {
			t.Fatal("followed a source link outside the workspace")
		}
		if _, err := cfg.sourcePath(context.Background(), ".", true); err == nil {
			t.Fatal("passed an unsafe directory to Codecore")
		}
	})
	if runtime.GOOS == "windows" {
		t.Run("junction", func(t *testing.T) {
			junction := filepath.Join(cfg.workspace, "outside-junction")
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
				"New-Item -ErrorAction Stop -ItemType Junction -Path $env:CODE_NAV_TEST_JUNCTION -Target $env:CODE_NAV_TEST_JUNCTION_TARGET | Out-Null")
			cmd.Env = append(os.Environ(), "CODE_NAV_TEST_JUNCTION="+junction, "CODE_NAV_TEST_JUNCTION_TARGET="+filepath.Dir(outside))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("junction creation unavailable: %v\n%s", err, output)
			}
			// Remove just the junction before the temporary directories are cleaned up.
			t.Cleanup(func() {
				if err := os.Remove(junction); err != nil {
					t.Error(err)
				}
			})
			if _, err := cfg.sourcePath(context.Background(), filepath.Join(junction, filepath.Base(outside)), false); err == nil {
				t.Fatal("followed a junction outside the workspace")
			}
		})
	}
}

func TestSubprocessResults(t *testing.T) {
	cfg := testConfig(t)
	cfg.maxOutputBytes = 256
	session := sessionFor(t, cfg)
	args := map[string]any{"path": createFile(t, filepath.Join(cfg.workspace, "sample.txt"), "")}
	for _, tt := range []struct {
		mode   string
		failed bool
		want   []string
	}{
		{"ok", false, []string{"[L:2] package example\n[L:5]   nested()\n"}},
		{"empty", false, []string{"No outline rows"}},
		{"unsupported", true, []string{"exit status 2", "unsupported file extension"}},
		{"warning", false, []string{"[L:2] func f() {\n", "unbalanced brackets"}},
		{"flood", false, []string{"  1 | ", "Result incomplete", "Use codegos_read"}},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			t.Setenv(mockMode, tt.mode)
			got, failed := call(t, session, "codegos_outline", args)
			if failed != tt.failed || len(got) > cfg.maxOutputBytes || !utf8.ValidString(got) {
				t.Fatalf("invalid result (failed=%v, bytes=%d): %q", failed, len(got), got)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("result %q is missing %q", got, want)
				}
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	cfg := testConfig(t)
	cfg.timeout = 250 * time.Millisecond
	t.Setenv(mockMode, "sleep")
	args := map[string]any{"path": createFile(t, filepath.Join(cfg.workspace, "sample.go"), "")}
	start := time.Now()
	got, failed := call(t, sessionFor(t, cfg), "codecore_outline", args)
	if !failed || !strings.Contains(got, "timed out") || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout result after %s: %q", time.Since(start), got)
	}
}

// Set all three paths to compare the built adapter's stdio results with real CLIs.
func TestRealCLIsOverStdio(t *testing.T) {
	adapter, core, gos := os.Getenv("CODE_NAV_MCP_BIN"), os.Getenv("CODE_NAV_CORE_BIN"), os.Getenv("CODE_NAV_GOS_BIN")
	if adapter == "" || core == "" || gos == "" {
		t.Skip("set CODE_NAV_MCP_BIN, CODE_NAV_CORE_BIN, and CODE_NAV_GOS_BIN")
	}
	cfg := testConfig(t)
	source := "package main\n\n// Workflow\nfunc main() {\n\t// Read config\n\tprintln(1)\n\t// Validate config\n\tprintln(2)\n}\n"
	path := createFile(t, filepath.Join(cfg.workspace, "source with spaces.go"), source)
	createFile(t, filepath.Join(cfg.workspace, "another.go"), "package main\n")
	session := connect(t, &mcp.CommandTransport{Command: exec.Command(adapter,
		"--workspace", cfg.workspace, "--codecore", core, "--codegos", gos,
	)})
	for _, tt := range []struct {
		tool, binary string
		args         map[string]any
		cliArgs      []string
	}{
		{"codecore_outline", core, map[string]any{"path": path, "depth": 1}, []string{"--depth", "1", path}},
		{"codecore_outline", core, map[string]any{"path": "."}, []string{cfg.workspace}},
		{"codegos_outline", gos, map[string]any{"path": path}, []string{path, "--plain"}},
		{"codegos_expand", gos, map[string]any{"path": path, "line": 3}, []string{path, "go", "3", "--plain"}},
		{"codegos_expand", gos, map[string]any{"path": path, "line": 5}, []string{path, "go", "5", "--plain"}},
		{"codegos_read", gos, map[string]any{"path": path, "start": 4, "end": 7}, []string{path, "show", "4-7", "--plain"}},
	} {
		want, err := exec.Command(tt.binary, tt.cliArgs...).Output()
		if err != nil {
			t.Fatalf("direct CLI failed: %v", err)
		}
		if got, failed := call(t, session, tt.tool, tt.args); failed || got != string(want) {
			t.Errorf("%s = %q, want %q", tt.tool, got, want)
		}
	}
	noComments := filepath.Join(cfg.workspace, "another.go")
	if got, failed := call(t, session, "codegos_outline", map[string]any{"path": noComments}); failed || got != "another.go (go, 1 lines)\n" {
		t.Errorf("empty hierarchy: %q (failed=%v)", got, failed)
	}
	unsupported := createFile(t, filepath.Join(cfg.workspace, "readme.txt"), "first\nsecond\n")
	if got, failed := call(t, session, "codegos_outline", map[string]any{"path": unsupported}); !failed || !strings.Contains(got, "unsupported file extension") {
		t.Errorf("unsupported file: %q", got)
	}
	if got, failed := call(t, session, "codegos_read", map[string]any{"path": unsupported, "start": 2, "end": 2}); failed || got != "2 | second\n" {
		t.Errorf("read on unsupported comment language: %q", got)
	}
	if got, failed := call(t, session, "codegos_read", map[string]any{"path": path, "start": 8, "end": 10}); !failed || !strings.Contains(got, "exceeds file length") {
		t.Errorf("range past EOF: %q", got)
	}
}
