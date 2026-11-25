package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type outlineInput struct {
	Path  string `json:"path" jsonschema:"Source file or non-recursive directory within the workspace."`
	Depth int    `json:"depth,omitempty" jsonschema:"Child levels to show, from 0 to 4. Defaults to 0."`
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
	server := mcp.NewServer(&mcp.Implementation{Name: "code-navigation-mcp", Version: "0.1.0"}, nil)
	if cfg.codecore != "" {
		mcp.AddTool(server, readOnlyTool("codecore_outline", "Map code structure and source line anchors."), cfg.outlineCode)
	}
	if cfg.codegos != "" {
		mcp.AddTool(server, readOnlyTool("codegos_outline", "Map documented workflows and comment headings."), cfg.outlineComments)
		mcp.AddTool(server, readOnlyTool("codegos_expand", "Inspect a comment heading, its children, or a short source preview."), cfg.expandComment)
		mcp.AddTool(server, readOnlyTool("codegos_read", fmt.Sprintf("Read up to %d numbered source lines.", cfg.maxReadLines)), cfg.readSource)
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
	if in.Depth < 0 || in.Depth > maxDepth {
		return nil, nil, fmt.Errorf("depth must be between 0 and %d", maxDepth)
	}
	result, err := cfg.run(ctx, cfg.codecore, in.Path, true, "Use a single file or a smaller depth.", func(path string) []string {
		return []string{"--depth", strconv.Itoa(in.Depth), "--", path}
	})
	return result, nil, err
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
