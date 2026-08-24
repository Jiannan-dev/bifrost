package anthropic

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestForcedWebSearchNeutralStreamConvertsToAnthropicServerToolResult(t *testing.T) {
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	query := "latest AI news"
	callID := "bf_websearch_test"
	title := "Example source"
	encrypted := "bifrost:test"
	item := &schemas.ResponsesMessage{
		ID: schemas.Ptr(callID), Type: schemas.Ptr(schemas.ResponsesMessageTypeWebSearchCall), Status: schemas.Ptr("completed"),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{Action: &schemas.ResponsesToolMessageActionStruct{
			ResponsesWebSearchToolCallAction: &schemas.ResponsesWebSearchToolCallAction{
				Type: "search", Query: &query, Queries: []string{query},
				Sources: []schemas.ResponsesWebSearchToolCallActionSearchSource{{Type: "url", URL: "https://example.com/news", Title: &title, EncryptedContent: &encrypted}},
			},
		}},
	}
	inProgress := *item
	inProgress.Status = schemas.Ptr("in_progress")
	outputIndex := 0
	chunks := []*schemas.BifrostResponsesStreamResponse{
		{Type: schemas.ResponsesStreamResponseTypeCreated, SequenceNumber: 0, Response: &schemas.BifrostResponsesResponse{ID: schemas.Ptr("resp_test"), Model: "local-model"}},
		{Type: schemas.ResponsesStreamResponseTypeOutputItemAdded, SequenceNumber: 1, OutputIndex: &outputIndex, Item: &inProgress},
		{Type: schemas.ResponsesStreamResponseTypeOutputItemDone, SequenceNumber: 2, OutputIndex: &outputIndex, Item: item},
		{Type: schemas.ResponsesStreamResponseTypeCompleted, SequenceNumber: 3, Response: &schemas.BifrostResponsesResponse{ID: schemas.Ptr("resp_test"), Model: "local-model"}},
	}

	var sawServerTool, sawResult, sawStop bool
	for _, chunk := range chunks {
		for _, event := range ToAnthropicResponsesStreamResponse(ctx, chunk) {
			if event == nil {
				continue
			}
			switch event.Type {
			case AnthropicStreamEventTypeContentBlockStart:
				if event.ContentBlock == nil {
					continue
				}
				if event.ContentBlock.Type == AnthropicContentBlockTypeServerToolUse {
					sawServerTool = event.ContentBlock.Name != nil && *event.ContentBlock.Name == "web_search" && event.ContentBlock.ID != nil && *event.ContentBlock.ID == callID
				}
				if event.ContentBlock.Type == AnthropicContentBlockTypeWebSearchToolResult {
					sawResult = event.ContentBlock.ToolUseID != nil && *event.ContentBlock.ToolUseID == callID && event.ContentBlock.Content != nil && len(event.ContentBlock.Content.ContentBlocks) == 1 && event.ContentBlock.Content.ContentBlocks[0].URL != nil && *event.ContentBlock.Content.ContentBlocks[0].URL == "https://example.com/news"
				}
			case AnthropicStreamEventTypeMessageStop:
				sawStop = true
			}
		}
	}
	if !sawServerTool || !sawResult || !sawStop {
		t.Fatalf("server_tool_use=%v result=%v message_stop=%v", sawServerTool, sawResult, sawStop)
	}
}
