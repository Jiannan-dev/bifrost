package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

func findFunctionCall(t *testing.T, msgs []schemas.ResponsesMessage) *schemas.ResponsesMessage {
	t.Helper()
	for i := range msgs {
		if msgs[i].Type != nil && *msgs[i].Type == schemas.ResponsesMessageTypeFunctionCall {
			return &msgs[i]
		}
	}
	t.Fatalf("no function_call among %d converted messages", len(msgs))
	return nil
}

func toolUseBlocks(callID, name, args string) []AnthropicContentBlock {
	return []AnthropicContentBlock{{
		Type:  AnthropicContentBlockTypeToolUse,
		ID:    schemas.Ptr(callID),
		Name:  schemas.Ptr(name),
		Input: json.RawMessage(args),
	}}
}

// TestConvertAnthropicToolUse_FunctionCallItemIDIsStable pins the DeepSeek
// prefix-cache fix: Anthropic history replay must derive the Responses
// function_call item id from the stable tool_use id, not mint fc_* + random
// bytes every turn. Both converters must agree. call_id stays the correlator.
func TestConvertAnthropicToolUse_FunctionCallItemIDIsStable(t *testing.T) {
	const callID = "call_00_cZgKrKbnWaYm8imc9JzH8651"
	blocks := toolUseBlocks(callID, "Bash", `{"command":"ls"}`)
	roleVal := schemas.ResponsesMessageRoleType(AnthropicMessageRoleAssistant)
	ctx := schemas.NewBifrostContext(nil, time.Time{})

	groupedA := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessagesGrouped(blocks, &roleVal, true))
	groupedB := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessagesGrouped(blocks, &roleVal, true))
	ungroupedA := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessages(ctx, blocks, &roleVal, true, ""))
	ungroupedB := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessages(ctx, blocks, &roleVal, true, ""))

	if groupedA.ID == nil || groupedB.ID == nil {
		t.Fatal("expected grouped conversion to mint a function_call item id")
	}
	if *groupedA.ID != *groupedB.ID {
		t.Errorf("grouped item id reminted: %q vs %q", *groupedA.ID, *groupedB.ID)
	}
	if ungroupedA.ID == nil || ungroupedB.ID == nil {
		t.Fatal("expected ungrouped conversion to mint a function_call item id")
	}
	if *ungroupedA.ID != *ungroupedB.ID {
		t.Errorf("ungrouped item id reminted: %q vs %q", *ungroupedA.ID, *ungroupedB.ID)
	}
	if *groupedA.ID != *ungroupedA.ID {
		t.Errorf("grouped and ungrouped ids differ: %q vs %q", *groupedA.ID, *ungroupedA.ID)
	}
	if !strings.HasPrefix(*groupedA.ID, "fc_") {
		t.Errorf("expected fc_ prefix, got %q", *groupedA.ID)
	}
	if len(*groupedA.ID) > 64 {
		t.Errorf("item id exceeds OpenAI's 64-char limit: %d", len(*groupedA.ID))
	}
	if groupedA.ResponsesToolMessage == nil || groupedA.ResponsesToolMessage.CallID == nil ||
		*groupedA.ResponsesToolMessage.CallID != callID {
		t.Errorf("expected call_id %q to survive, got %+v", callID, groupedA.ResponsesToolMessage)
	}

	other := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessagesGrouped(
		toolUseBlocks("call_other", "Bash", `{"command":"ls"}`), &roleVal, true))
	if other.ID == nil || *other.ID == *groupedA.ID {
		t.Errorf("expected a different call_id to mint a different item id, got %+v", other.ID)
	}

	inputOnly := findFunctionCall(t, convertAnthropicContentBlocksToResponsesMessagesGrouped(blocks, &roleVal, false))
	if inputOnly.ID != nil {
		t.Errorf("expected no item id on input-side conversion, got %q", *inputOnly.ID)
	}
}

func TestResponsesToolCallItemIDOmitsEmptyCallID(t *testing.T) {
	if got := responsesToolCallItemID(nil); got != nil {
		t.Errorf("nil call_id: got %q", *got)
	}
	if got := responsesToolCallItemID(schemas.Ptr("")); got != nil {
		t.Errorf("empty call_id: got %q", *got)
	}
}
