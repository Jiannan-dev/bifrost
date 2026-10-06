package bifrost

import (
	"context"
	"strings"
	"testing"
	"time"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

func reasoningItemIDError() *schemas.BifrostError {
	return &schemas.BifrostError{
		StatusCode: schemas.Ptr(400),
		Error: &schemas.ErrorField{
			Type:    schemas.Ptr("invalid_request_error"),
			Message: "Referenced reasoning item 'rs_87af699f26de1dd24bc40066ec1bd60e8cc576f99c993e9051' was not found or has expired.",
		},
	}
}

func newThinkingOnlyReasoningRequest(id string) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{
		RequestType: schemas.ResponsesRequest,
		ResponsesRequest: &schemas.BifrostResponsesRequest{
			Provider: schemas.OpencodeGo,
			Model:    "muse-spark-1.3-contributor",
			Input: []schemas.ResponsesMessage{
				{
					ID:   schemas.Ptr("msg_user_1"),
					Type: schemas.Ptr(schemas.ResponsesMessageTypeMessage),
					Role: schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
					Content: &schemas.ResponsesMessageContent{
						ContentStr: schemas.Ptr("run the tests"),
					},
				},
				{
					ID:   schemas.Ptr(id),
					Type: schemas.Ptr(schemas.ResponsesMessageTypeReasoning),
					Content: &schemas.ResponsesMessageContent{
						ContentBlocks: []schemas.ResponsesMessageContentBlock{
							{
								Type: schemas.ResponsesOutputMessageContentTypeReasoning,
								Text: schemas.Ptr("I will run the tests."),
							},
						},
					},
				},
			},
		},
	}
}

func TestIsReasoningItemIDRejection(t *testing.T) {
	tests := []struct {
		name string
		err  *schemas.BifrostError
		want bool
	}{
		{"production OpenCode Go 400", reasoningItemIDError(), true},
		{
			name: "not found without expired",
			err: &schemas.BifrostError{
				StatusCode: schemas.Ptr(400),
				Error:      &schemas.ErrorField{Message: "Referenced reasoning item 'rs_abc' was not found"},
			},
			want: true,
		},
		{
			name: "expired without not found",
			err: &schemas.BifrostError{
				StatusCode: schemas.Ptr(400),
				Error:      &schemas.ErrorField{Message: "Referenced reasoning item 'rs_abc' has expired"},
			},
			want: true,
		},
		{"encrypted-content refusal is a different fail-soft", encryptedContentError(), false},
		{
			name: "500 with the same text is a transient fault",
			err: &schemas.BifrostError{
				StatusCode: schemas.Ptr(500),
				Error:      &schemas.ErrorField{Message: reasoningItemIDError().Error.Message},
			},
			want: false,
		},
		{
			name: "unrelated 400",
			err: &schemas.BifrostError{
				StatusCode: schemas.Ptr(400),
				Error:      &schemas.ErrorField{Message: "Invalid 'input': missing required field"},
			},
			want: false,
		},
		{"nil error", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReasoningItemIDRejection(tc.err); got != tc.want {
				t.Errorf("isReasoningItemIDRejection() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStripResponsesReasoningItemIDs(t *testing.T) {
	t.Run("strips a thinking-only item that has no ResponsesReasoning", func(t *testing.T) {
		req := newThinkingOnlyReasoningRequest("rs_think_only")
		if !stripResponsesReasoningItemIDs(nil, req) {
			t.Fatal("expected the strip to report a change")
		}
		if req.ResponsesRequest.Input[1].ID != nil {
			t.Errorf("expected the thinking-only item id to be dropped, got %q", *req.ResponsesRequest.Input[1].ID)
		}
		if req.ResponsesRequest.Input[1].Content == nil || len(req.ResponsesRequest.Input[1].Content.ContentBlocks) != 1 {
			t.Fatal("expected thinking content to survive")
		}
		if req.ResponsesRequest.Input[0].ID == nil || *req.ResponsesRequest.Input[0].ID != "msg_user_1" {
			t.Errorf("expected ordinary message ids to be preserved, got %+v", req.ResponsesRequest.Input[0].ID)
		}
	})

	t.Run("keeps encrypted_content and summary", func(t *testing.T) {
		req := newEncryptedReasoningRequest("ciphertext")
		if !stripResponsesReasoningItemIDs(nil, req) {
			t.Fatal("expected the strip to report a change")
		}
		reasoning := req.ResponsesRequest.Input[1].ResponsesReasoning
		if reasoning == nil || reasoning.EncryptedContent == nil || *reasoning.EncryptedContent != "ciphertext" {
			t.Errorf("expected encrypted_content to survive, got %+v", reasoning)
		}
		if len(reasoning.Summary) != 1 || reasoning.Summary[0].Text != "planning the run" {
			t.Errorf("expected the summary to survive, got %+v", reasoning.Summary)
		}
		if req.ResponsesRequest.Input[1].ID != nil {
			t.Errorf("expected the reasoning item id to be dropped, got %q", *req.ResponsesRequest.Input[1].ID)
		}
	})

	t.Run("does not mutate the caller's item id", func(t *testing.T) {
		req := newThinkingOnlyReasoningRequest("rs_original")
		original := &req.ResponsesRequest.Input[1]
		if !stripResponsesReasoningItemIDs(nil, req) {
			t.Fatal("expected the strip to report a change")
		}
		if original.ID == nil || *original.ID != "rs_original" {
			t.Error("expected the original slice element to keep its id")
		}
	})

	t.Run("reports no change when there is nothing to strip", func(t *testing.T) {
		req := newThinkingOnlyReasoningRequest("rs_think_only")
		req.ResponsesRequest.Input[1].ID = nil
		if stripResponsesReasoningItemIDs(nil, req) {
			t.Error("expected no change to be reported")
		}
	})

	t.Run("rewrites a raw body under passthrough", func(t *testing.T) {
		ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
		ctx.SetValue(schemas.BifrostContextKeyUseRawRequestBody, true)

		req := newThinkingOnlyReasoningRequest("rs_think_only")
		req.ResponsesRequest.RawRequestBody = []byte(`{"model":"muse-spark-1.3-contributor","input":[` +
			`{"type":"message","id":"msg_1","role":"user","content":"run the tests"},` +
			`{"type":"reasoning","id":"rs_think_only","content":[{"type":"reasoning_text","text":"planning"}],"unmodeled_field":7}` +
			`],"store":false}`)

		if !stripResponsesReasoningItemIDs(ctx, req) {
			t.Fatal("expected the raw body to be rewritten")
		}
		body := string(req.ResponsesRequest.RawRequestBody)
		if strings.Contains(body, `"rs_think_only"`) {
			t.Errorf("expected the reasoning item id to be dropped, got %s", body)
		}
		if !strings.Contains(body, `"msg_1"`) {
			t.Errorf("expected ordinary message ids to be preserved, got %s", body)
		}
		if !strings.Contains(body, `"unmodeled_field":7`) {
			t.Errorf("expected fields Bifrost does not model to survive, got %s", body)
		}
		if !strings.Contains(body, `"store":false`) {
			t.Errorf("expected the rest of the body to be untouched, got %s", body)
		}
	})

	t.Run("declines large payload mode", func(t *testing.T) {
		ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
		ctx.SetValue(schemas.BifrostContextKeyLargePayloadMode, true)

		req := newThinkingOnlyReasoningRequest("rs_think_only")
		if stripResponsesReasoningItemIDs(ctx, req) {
			t.Error("expected no change to be claimed when the body streams past core unparsed")
		}
		if req.ResponsesRequest.Input[1].ID == nil {
			t.Error("expected the typed input to be left alone in large payload mode")
		}
	})

	t.Run("ignores chat requests", func(t *testing.T) {
		req := &schemas.BifrostRequest{RequestType: schemas.ChatCompletionRequest}
		if stripResponsesReasoningItemIDs(nil, req) {
			t.Error("expected no change for a chat request")
		}
	})
}

func TestExecuteRequestWithRetries_StripsReasoningItemIDsAndRetries(t *testing.T) {
	config := createTestConfig(0, time.Millisecond, time.Millisecond)
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	ctx.SetValue(schemas.BifrostContextKeyTracer, &schemas.NoOpTracer{})
	logger := NewDefaultLogger(schemas.LogLevelError)

	req := newThinkingOnlyReasoningRequest("rs_87af699f26de1dd24bc40066ec1bd60e8cc576f99c993e9051")
	original := &req.ResponsesRequest.Input[1]

	callCount := 0
	var secondAttemptInput []schemas.ResponsesMessage
	handler := func(_ schemas.Key) (string, *schemas.BifrostError) {
		callCount++
		if callCount == 1 {
			return "", reasoningItemIDError()
		}
		secondAttemptInput = req.ResponsesRequest.Input
		return "success", nil
	}

	result, err := executeRequestWithRetries(ctx, config, handler, nil,
		schemas.ResponsesRequest, schemas.OpencodeGo, "muse-spark-1.3-contributor", req, logger)

	if err != nil {
		t.Fatalf("expected the stripped retry to succeed, got %v", err)
	}
	if result != "success" {
		t.Fatalf("expected 'success', got %q", result)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 attempts (original + stripped retry), got %d", callCount)
	}
	if len(secondAttemptInput) != 2 {
		t.Fatalf("expected both input items to survive the strip, got %d", len(secondAttemptInput))
	}
	if secondAttemptInput[1].ID != nil {
		t.Errorf("expected the thinking-only item id to be stripped, got %q", *secondAttemptInput[1].ID)
	}
	if secondAttemptInput[1].Content == nil || len(secondAttemptInput[1].Content.ContentBlocks) != 1 {
		t.Fatal("expected thinking content to survive")
	}
	if secondAttemptInput[0].ID == nil || *secondAttemptInput[0].ID != "msg_user_1" {
		t.Errorf("expected ordinary message ids to be preserved, got %+v", secondAttemptInput[0].ID)
	}
	if original.ID == nil || *original.ID != "rs_87af699f26de1dd24bc40066ec1bd60e8cc576f99c993e9051" {
		t.Error("expected the caller's original item to keep its id")
	}
	if _, ok := ctx.Value(schemas.BifrostContextKeyBypassSemanticCache).(bool); ok {
		t.Error("fail-soft must not set BifrostContextKeyBypassSemanticCache")
	}
	for _, entry := range ctx.GetRoutingEngineLogs() {
		if strings.Contains(strings.ToLower(entry.Message), "opaque-stateless-reasoning") {
			t.Errorf("routing log must not mention opaque-stateless-reasoning: %s", entry.Message)
		}
		if strings.Contains(entry.Message, "I will run the tests.") {
			t.Errorf("routing log must not dump thinking text: %s", entry.Message)
		}
	}
}

func TestExecuteRequestWithRetries_ReasoningItemIDErrorDoesNotStripCiphertext(t *testing.T) {
	config := createTestConfig(0, time.Millisecond, time.Millisecond)
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	ctx.SetValue(schemas.BifrostContextKeyTracer, &schemas.NoOpTracer{})
	logger := NewDefaultLogger(schemas.LogLevelError)

	req := newEncryptedReasoningRequest("gAAAAABkeep-this-ciphertext")

	callCount := 0
	handler := func(_ schemas.Key) (string, *schemas.BifrostError) {
		callCount++
		if callCount == 1 {
			return "", reasoningItemIDError()
		}
		if req.ResponsesRequest.Input[1].ResponsesReasoning == nil ||
			req.ResponsesRequest.Input[1].ResponsesReasoning.EncryptedContent == nil ||
			*req.ResponsesRequest.Input[1].ResponsesReasoning.EncryptedContent != "gAAAAABkeep-this-ciphertext" {
			t.Error("reasoning-item-id fail-soft must not strip encrypted_content")
		}
		return "success", nil
	}

	if _, err := executeRequestWithRetries(ctx, config, handler, nil,
		schemas.ResponsesRequest, schemas.OpencodeGo, "muse-spark-1.3-contributor", req, logger); err != nil {
		t.Fatalf("expected the stripped retry to succeed, got %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 attempts, got %d", callCount)
	}
}
