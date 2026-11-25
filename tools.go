package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type outlineInput struct {
	Path  string `json:"path" jsonschema:"Source file or recursively walked directory within the workspace."`
	Depth *int   `json:"depth,omitempty" jsonschema:"Child levels to show, from 0 to 4. Omit for Codecore's language default."`
}

type searchInput struct {
	Path    string `json:"path" jsonschema:"File or recursively searched directory within the workspace. Use . for the workspace root."`
	Query   string `json:"query" jsonschema:"Identifier or short topic to look up."`
	Limit   *int   `json:"limit,omitempty" jsonschema:"Maximum ranked hits. Defaults to 5 or the configured cap, whichever is lower."`
	DefOnly bool   `json:"def_only,omitempty" jsonschema:"Keep definitions only."`
	PerFile bool   `json:"per_file,omitempty" jsonschema:"Keep the best hit per file."`
	Word    bool   `json:"word,omitempty" jsonschema:"Match whole words and suppress prefix matches."`
	NoTests bool   `json:"no_tests,omitempty" jsonschema:"Exclude test files and directories."`
}

type fileInput struct {
	Path string `json:"path" jsonschema:"Source file within the workspace."`
}

type expandInput struct {
	fileInput
	Line int `json:"line" jsonschema:"First source line of a comment heading from the outline."`
}

type readInput struct {
	fileInput
	Start int `json:"start" jsonschema:"First source line to include, starting at 1."`
	End   int `json:"end" jsonschema:"Last source line to include."`
}

func newServer(cfg config) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "code-navigation-mcp", Version: "0.2.0"}, nil)
	if cfg.codecore != "" {
		mcp.AddTool(server, readOnlyTool("codecore_outline", "Map file or repository structure with line anchors to identify what to read."), cfg.outlineCode)
		mcp.AddTool(server, readOnlyTool("codecore_search", fmt.Sprintf("Find where a symbol or topic is implemented using ranked matches with file paths, line ranges, and enclosing context. Up to %d hits.", cfg.maxSearchResults)), cfg.searchCode)
	}
	if cfg.codegos != "" {
		mcp.AddTool(server, readOnlyTool("codegos_outline", "Find documented workflows and intent through a file's comment headings and line ranges."), cfg.outlineComments)
		mcp.AddTool(server, readOnlyTool("codegos_expand", "Follow a comment heading to its child sections or a short implementation preview."), cfg.expandComment)
		mcp.AddTool(server, readOnlyTool("codegos_read", fmt.Sprintf("Verify implementation details by reading up to %d exact, numbered source lines.", cfg.maxReadLines)), cfg.readSource)
	}
	return server
}

func readOnlyTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name: name, Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func (cfg config) outlineCode(ctx context.Context, _ *mcp.CallToolRequest, in outlineInput) (*mcp.CallToolResult, any, error) {
	if in.Depth != nil && (*in.Depth < 0 || *in.Depth > maxDepth) {
		return nil, nil, fmt.Errorf("depth must be between 0 and %d", maxDepth)
	}
	result, err := cfg.run(ctx, cfg.codecore, in.Path, true, "Use a single file or a smaller depth.", func(path string) []string {
		if in.Depth == nil {
			return []string{"--", path}
		}
		return []string{"--depth", strconv.Itoa(*in.Depth), "--", path}
	})
	return result, nil, err
}

func (cfg config) searchCode(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, nil, fmt.Errorf("query must contain a searchable term")
	}
	limit := min(defaultSearchLimit, cfg.maxSearchResults)
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 || limit > cfg.maxSearchResults {
		return nil, nil, fmt.Errorf("limit must be between 1 and %d", cfg.maxSearchResults)
	}
	result, err := cfg.run(ctx, cfg.codecore, in.Path, true, "Use a narrower path or a smaller limit.", func(path string) []string {
		return searchArguments(in, limit, path)
	})
	return result, nil, err
}

func searchArguments(in searchInput, limit int, path string) []string {
	args := []string{"--ask", in.Query, "--top", strconv.Itoa(limit)}
	if in.DefOnly {
		args = append(args, "--def-only")
	}
	if in.PerFile {
		args = append(args, "--per-file")
	}
	if in.Word {
		args = append(args, "--word")
	}
	if in.NoTests {
		args = append(args, "--no-tests")
	}
	return append(args, "--", path)
}

func (cfg config) outlineComments(ctx context.Context, _ *mcp.CallToolRequest, in fileInput) (*mcp.CallToolResult, any, error) {
	result, err := cfg.run(ctx, cfg.codegos, in.Path, false, "Use codegos_read with a small line range.", func(path string) []string {
		return []string{path, "--plain"}
	})
	return result, nil, err
}

func (cfg config) expandComment(ctx context.Context, _ *mcp.CallToolRequest, in expandInput) (*mcp.CallToolResult, any, error) {
	if in.Line < 1 {
		return nil, nil, fmt.Errorf("line must be at least 1")
	}
	result, err := cfg.run(ctx, cfg.codegos, in.Path, false, "Expand a child heading or read a small line range.", func(path string) []string {
		return []string{path, "go", strconv.Itoa(in.Line), "--plain"}
	})
	return result, nil, err
}

func (cfg config) readSource(ctx context.Context, _ *mcp.CallToolRequest, in readInput) (*mcp.CallToolResult, any, error) {
	if in.Start < 1 || in.End < in.Start {
		return nil, nil, fmt.Errorf("range requires 1 <= start <= end")
	}
	if in.End-in.Start >= cfg.maxReadLines {
		return nil, nil, fmt.Errorf("range must contain at most %d lines", cfg.maxReadLines)
	}
	result, err := cfg.run(ctx, cfg.codegos, in.Path, false, "Request a smaller line range.", func(path string) []string {
		return []string{path, "show", fmt.Sprintf("%d-%d", in.Start, in.End), "--plain"}
	})
	return result, nil, err
}
