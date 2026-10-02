package agentcreator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ApplyFunc func(context.Context, ApplyRequest) (ApplyResult, error)

func RunMCPServer(ctx context.Context, apply ApplyFunc) error {
	server := NewMCPServer(apply)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func NewMCPServer(apply ApplyFunc) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kmx-agent-creator", Version: "v0.1.0"}, nil)
	type schemaArgs struct{}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "kmx_agent_schema",
		Description: "Return the schema used to describe a KMX agent without applying anything.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ schemaArgs) (*mcp.CallToolResult, any, error) {
		result := map[string]any{"schema": IntentJSONSchema()}
		return jsonToolResult(result)
	})
	type planArgs struct {
		Intent AgentIntent `json:"intent" jsonschema:"agent intent to validate and plan"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "kmx_agent_plan",
		Description: "Validate an AgentIntent, ask focused questions for missing fields, and return an immutable review plan. This tool never mutates files or a cluster.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, args planArgs) (*mcp.CallToolResult, any, error) {
		result, err := BuildPlan(args.Intent)
		if err != nil {
			return nil, nil, err
		}
		return jsonToolResult(result)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "kmx_agent_apply",
		Description: "Apply an AgentCreationPlan only when approvedDigest exactly matches the reviewed plan digest.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ApplyRequest) (*mcp.CallToolResult, any, error) {
		if apply == nil {
			return nil, nil, fmt.Errorf("apply is unavailable")
		}
		if err := VerifyApplyRequest(args); err != nil {
			return nil, nil, err
		}
		result, err := apply(ctx, args)
		if err != nil {
			return nil, nil, err
		}
		return jsonToolResult(result)
	})
	return server
}

func jsonToolResult(value any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, value, nil
}
