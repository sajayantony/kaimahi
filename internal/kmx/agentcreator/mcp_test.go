package agentcreator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPlanAndDigestGatedApply(t *testing.T) {
	ctx := t.Context()
	applied := 0
	server := NewMCPServer(func(_ context.Context, request ApplyRequest) (ApplyResult, error) {
		applied++
		return ApplyResult{Status: "applied", PlanDigest: request.Plan.Digest, Mode: request.Plan.Intent.Spec.Deployment.Mode}, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	planCall, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "kmx_agent_plan",
		Arguments: map[string]any{"intent": validIntent()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if planCall.IsError || len(planCall.Content) != 1 {
		t.Fatal(planCall)
	}
	text, ok := planCall.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content=%T", planCall.Content[0])
	}
	var planned PlanResult
	if err := json.Unmarshal([]byte(text.Text), &planned); err != nil {
		t.Fatal(err)
	}
	if planned.Plan == nil {
		t.Fatal(planned)
	}

	applyCall, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "kmx_agent_apply",
		Arguments: ApplyRequest{
			Plan: *planned.Plan, ApprovedDigest: planned.Plan.Digest,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applyCall.IsError || applied != 1 {
		t.Fatalf("result=%v applied=%d", applyCall, applied)
	}

	refused, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "kmx_agent_apply",
		Arguments: ApplyRequest{
			Plan: *planned.Plan, ApprovedDigest: "sha256:wrong",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !refused.IsError || applied != 1 {
		t.Fatalf("wrong digest was not refused: result=%v applied=%d", refused, applied)
	}
}
