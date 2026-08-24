package mcp

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestUnsupportedWebSearchRewrittenToConfiguredMCPTool(t *testing.T) {
	schemas.SetCapabilityResolver(func(provider schemas.ModelProvider, model string) *schemas.ModelCapabilities {
		supported := false
		return &schemas.ModelCapabilities{SupportsWebSearch: &supported}
	})
	t.Cleanup(func() { schemas.SetCapabilityResolver(nil) })

	const fallback = "exa-web_search_exa"
	cm := &mockToolClientManager{tools: []schemas.ChatTool{makeTool(fallback), makeTool("other-tool")}}
	tm := NewToolsManager(&schemas.MCPToolManagerConfig{
		MaxAgentDepth:         5,
		WebSearchFallbackTool: fallback,
	}, cm, nil, nil, &MockLogger{})

	req := buildResponsesRequest()
	req.ResponsesRequest.Provider = schemas.VLLM
	req.ResponsesRequest.Model = "local-model"
	req.ResponsesRequest.Params.Tools = []schemas.ResponsesTool{{
		Type:                   schemas.ResponsesToolTypeWebSearch,
		ResponsesToolWebSearch: &schemas.ResponsesToolWebSearch{},
	}}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)

	result := tm.ParseAndAddToolsToRequest(ctx, req)
	var nativeSearch int
	var fallbackCount int
	for _, tool := range result.ResponsesRequest.Params.Tools {
		if tool.Type == schemas.ResponsesToolTypeWebSearch {
			nativeSearch++
		}
		if tool.Name != nil && *tool.Name == fallback {
			fallbackCount++
			if tool.Type != schemas.ResponsesToolTypeFunction {
				t.Fatalf("fallback type = %q, want function", tool.Type)
			}
		}
	}
	if nativeSearch != 0 {
		t.Fatalf("native web_search survived rewrite: %+v", result.ResponsesRequest.Params.Tools)
	}
	if fallbackCount != 1 {
		t.Fatalf("fallback tool count = %d, want 1", fallbackCount)
	}
}

func TestUncataloguedWebSearchUsesConfiguredFallback(t *testing.T) {
	schemas.SetCapabilityResolver(nil)

	const fallback = "exa-web_search_exa"
	cm := &mockToolClientManager{tools: []schemas.ChatTool{makeTool(fallback)}}
	tm := NewToolsManager(&schemas.MCPToolManagerConfig{
		MaxAgentDepth:         5,
		WebSearchFallbackTool: fallback,
	}, cm, nil, nil, &MockLogger{})

	req := buildResponsesRequest()
	req.ResponsesRequest.Provider = schemas.VLLM
	req.ResponsesRequest.Model = "uncatalogued-local-model"
	req.ResponsesRequest.Params.Tools = []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeWebSearch}}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	result := tm.ParseAndAddToolsToRequest(ctx, req)

	if len(result.ResponsesRequest.Params.Tools) == 0 || result.ResponsesRequest.Params.Tools[0].Type == schemas.ResponsesToolTypeWebSearch {
		t.Fatalf("uncatalogued model did not use configured fallback: %+v", result.ResponsesRequest.Params.Tools)
	}
}

func TestSupportedWebSearchIsNotRewritten(t *testing.T) {
	schemas.SetCapabilityResolver(func(provider schemas.ModelProvider, model string) *schemas.ModelCapabilities {
		supported := true
		return &schemas.ModelCapabilities{SupportsWebSearch: &supported}
	})
	t.Cleanup(func() { schemas.SetCapabilityResolver(nil) })

	const fallback = "exa-web_search_exa"
	cm := &mockToolClientManager{tools: []schemas.ChatTool{makeTool(fallback)}}
	tm := NewToolsManager(&schemas.MCPToolManagerConfig{
		MaxAgentDepth:         5,
		WebSearchFallbackTool: fallback,
		DisableAutoToolInject: true,
	}, cm, nil, nil, &MockLogger{})

	req := buildResponsesRequest()
	req.ResponsesRequest.Params.Tools = []schemas.ResponsesTool{{
		Type:                   schemas.ResponsesToolTypeWebSearch,
		ResponsesToolWebSearch: &schemas.ResponsesToolWebSearch{},
	}}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	result := tm.ParseAndAddToolsToRequest(ctx, req)

	if len(result.ResponsesRequest.Params.Tools) != 1 || result.ResponsesRequest.Params.Tools[0].Type != schemas.ResponsesToolTypeWebSearch {
		t.Fatalf("supported native web_search was modified: %+v", result.ResponsesRequest.Params.Tools)
	}
}

func TestUnsupportedWebSearchFallbackRunsThroughResponsesAgentLoop(t *testing.T) {
	const fallback = "exa-web_search_exa"
	const callID = "search-call-1"
	cm := &MockAutoClientManager{}
	executor := &AgentModeExecutor{logger: &MockLogger{}}

	original := &schemas.BifrostResponsesRequest{
		Provider: schemas.VLLM,
		Model:    "local-model",
		Input: []schemas.ResponsesMessage{{
			Type:    schemas.Ptr(schemas.ResponsesMessageTypeMessage),
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("What happened today?")},
		}},
	}
	initial := &schemas.BifrostResponsesResponse{Output: []schemas.ResponsesMessage{{
		Type: schemas.Ptr(schemas.ResponsesMessageTypeFunctionCall),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{
			CallID:    schemas.Ptr(callID),
			Name:      schemas.Ptr(fallback),
			Arguments: schemas.Ptr(`{"query":"today's news"}`),
		},
	}}}

	executed := 0
	executeTool := func(ctx *schemas.BifrostContext, req *schemas.BifrostMCPRequest) (*schemas.BifrostMCPResponse, error) {
		executed++
		if req.ChatAssistantMessageToolCall == nil || req.ChatAssistantMessageToolCall.Function.Name == nil || *req.ChatAssistantMessageToolCall.Function.Name != fallback {
			t.Fatalf("unexpected tool execution request: %+v", req.ChatAssistantMessageToolCall)
		}
		return &schemas.BifrostMCPResponse{ChatMessage: &schemas.ChatMessage{
			Role:            schemas.ChatMessageRoleTool,
			ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: schemas.Ptr(callID)},
			Content:         &schemas.ChatMessageContent{ContentStr: schemas.Ptr(`[{"title":"News","url":"https://example.com/news"}]`)},
		}}, nil
	}

	secondCalls := 0
	makeReq := func(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
		secondCalls++
		foundResult := false
		for _, msg := range req.Input {
			if msg.Type != nil && *msg.Type == schemas.ResponsesMessageTypeFunctionCallOutput && msg.CallID != nil && *msg.CallID == callID {
				foundResult = true
			}
		}
		if !foundResult {
			t.Fatalf("second LLM turn did not receive search result: %+v", req.Input)
		}
		return &schemas.BifrostResponsesResponse{Output: []schemas.ResponsesMessage{{
			Type:    schemas.Ptr(schemas.ResponsesMessageTypeMessage),
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleAssistant),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("According to the search result, here is today's news.")},
		}}}, nil
	}

	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	result, err := executor.ExecuteAgentForResponsesRequest(ctx, 5, original, initial, makeReq, nil, executeTool, cm)
	if err != nil {
		t.Fatalf("agent loop failed: %+v", err)
	}
	if executed != 1 || secondCalls != 1 {
		t.Fatalf("executed=%d secondCalls=%d, want 1/1", executed, secondCalls)
	}
	if result == nil || len(result.Output) == 0 || result.Output[0].Content == nil || result.Output[0].Content.ContentStr == nil {
		t.Fatalf("missing final model response: %+v", result)
	}
}

func TestUnsupportedWebSearchStreamIsNotRewritten(t *testing.T) {
	const fallback = "exa-web_search_exa"
	cm := &mockToolClientManager{tools: []schemas.ChatTool{makeTool(fallback)}}
	tm := NewToolsManager(&schemas.MCPToolManagerConfig{
		MaxAgentDepth:         5,
		WebSearchFallbackTool: fallback,
		DisableAutoToolInject: true,
	}, cm, nil, nil, &MockLogger{})

	req := buildResponsesRequest()
	req.RequestType = schemas.ResponsesStreamRequest
	req.ResponsesRequest.Provider = schemas.VLLM
	req.ResponsesRequest.Model = "local-model"
	req.ResponsesRequest.Params.Tools = []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeWebSearch}}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)

	result := tm.ParseAndAddToolsToRequest(ctx, req)
	if len(result.ResponsesRequest.Params.Tools) != 1 || result.ResponsesRequest.Params.Tools[0].Type != schemas.ResponsesToolTypeWebSearch {
		t.Fatalf("streaming request must not be rewritten before streaming agent mode exists: %+v", result.ResponsesRequest.Params.Tools)
	}
}

func TestUnsupportedWebSearchPreservedWhenFallbackUnavailable(t *testing.T) {
	schemas.SetCapabilityResolver(func(provider schemas.ModelProvider, model string) *schemas.ModelCapabilities {
		supported := false
		return &schemas.ModelCapabilities{SupportsWebSearch: &supported}
	})
	t.Cleanup(func() { schemas.SetCapabilityResolver(nil) })

	tm := NewToolsManager(&schemas.MCPToolManagerConfig{
		MaxAgentDepth:         5,
		WebSearchFallbackTool: "missing-search-tool",
		DisableAutoToolInject: true,
	}, &mockToolClientManager{}, nil, nil, &MockLogger{})
	req := buildResponsesRequest()
	req.ResponsesRequest.Params.Tools = []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeWebSearch}}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)

	result := tm.ParseAndAddToolsToRequest(ctx, req)
	if len(result.ResponsesRequest.Params.Tools) != 1 || result.ResponsesRequest.Params.Tools[0].Type != schemas.ResponsesToolTypeWebSearch {
		t.Fatalf("missing fallback must fail closed and preserve native tool: %+v", result.ResponsesRequest.Params.Tools)
	}
}
