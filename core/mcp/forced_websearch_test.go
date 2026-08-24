package mcp

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func forcedSearchRequest(query string) *schemas.BifrostResponsesRequest {
	role := schemas.ResponsesInputMessageRoleUser
	content := "Perform a web search for the query: " + query
	name := "web_search"
	return &schemas.BifrostResponsesRequest{
		Provider: schemas.VLLM,
		Model:    "local-model",
		Input: []schemas.ResponsesMessage{{
			Type: schemas.Ptr(schemas.ResponsesMessageTypeMessage), Role: &role,
			Content: &schemas.ResponsesMessageContent{ContentStr: &content},
		}},
		Params: &schemas.ResponsesParameters{
			Tools: []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeWebSearch}},
			ToolChoice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeFunction, Name: &name,
			}},
		},
	}
}

func TestForcedWebSearchQueryRecognizesClaudeCodeAuxiliaryRequest(t *testing.T) {
	req := forcedSearchRequest("latest AI news")
	query, ok := forcedWebSearchQuery(req)
	if !ok || query != "latest AI news" {
		t.Fatalf("query=%q ok=%v", query, ok)
	}
}

func TestForcedWebSearchQueryAcceptsAnthropicNormalizedAutoChoice(t *testing.T) {
	req := forcedSearchRequest("latest AI news")
	auto := string(schemas.ResponsesToolChoiceTypeAuto)
	req.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: &auto}
	if query, ok := forcedWebSearchQuery(req); !ok || query != "latest AI news" {
		t.Fatalf("normalized auxiliary query=%q ok=%v", query, ok)
	}
}

func TestForcedWebSearchQueryRejectsOrdinaryWebSearchDeclaration(t *testing.T) {
	req := forcedSearchRequest("latest AI news")
	req.Params.ToolChoice = nil
	if query, ok := forcedWebSearchQuery(req); ok {
		t.Fatalf("ordinary request incorrectly handled, query=%q", query)
	}
}

func TestForcedWebSearchQueryRejectsAmbiguousMultiToolRequest(t *testing.T) {
	req := forcedSearchRequest("latest AI news")
	req.Params.Tools = append(req.Params.Tools, schemas.ResponsesTool{Type: schemas.ResponsesToolTypeFunction, Name: schemas.Ptr("other")})
	if query, ok := forcedWebSearchQuery(req); ok {
		t.Fatalf("multi-tool request incorrectly handled, query=%q", query)
	}
}

func TestForcedWebSearchSourcesParsesNestedMCPResult(t *testing.T) {
	raw := `{"results":[{"title":"First","url":"https://example.com/one","snippet":"one"},{"name":"Second","link":"https://example.org/two"}]}`
	result := &schemas.ResponsesMessage{ResponsesToolMessage: &schemas.ResponsesToolMessage{Output: &schemas.ResponsesToolMessageOutputStruct{ResponsesToolCallOutputStr: &raw}}}
	sources := forcedWebSearchSources(result)
	if len(sources) != 2 {
		t.Fatalf("sources=%+v", sources)
	}
	if sources[0].URL != "https://example.com/one" || sources[0].Title == nil || *sources[0].Title != "First" {
		t.Fatalf("first source=%+v", sources[0])
	}
	if sources[0].EncryptedContent == nil || *sources[0].EncryptedContent == "" {
		t.Fatal("encrypted_content was not synthesized")
	}
}

func TestForcedWebSearchSourcesFallsBackToURLsInText(t *testing.T) {
	raw := "Result: https://example.com/a and https://example.org/b"
	result := &schemas.ResponsesMessage{Content: &schemas.ResponsesMessageContent{ContentStr: &raw}}
	sources := forcedWebSearchSources(result)
	if len(sources) != 2 {
		t.Fatalf("sources=%+v", sources)
	}
}

func TestForcedWebSearchStreamEventShape(t *testing.T) {
	query := "latest AI news"
	callID := "bf_websearch_test"
	n := 1
	item := schemas.ResponsesMessage{
		ID: schemas.Ptr(callID), Type: schemas.Ptr(schemas.ResponsesMessageTypeWebSearchCall), Status: schemas.Ptr("completed"),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{Action: &schemas.ResponsesToolMessageActionStruct{
			ResponsesWebSearchToolCallAction: &schemas.ResponsesWebSearchToolCallAction{Type: "search", Query: &query, Queries: []string{query}, Sources: []schemas.ResponsesWebSearchToolCallActionSearchSource{{Type: "url", URL: "https://example.com", Title: schemas.Ptr("Example")}}},
		}},
	}
	response := &schemas.BifrostResponsesResponse{ID: schemas.Ptr("resp_test"), Object: "response", Model: "local-model", Output: []schemas.ResponsesMessage{item}, Usage: &schemas.ResponsesResponseUsage{OutputTokensDetails: &schemas.ResponsesResponseOutputTokens{NumSearchQueries: &n}}}
	ch := forcedWebSearchResponseStream(response)
	var types []schemas.ResponsesStreamResponseType
	for chunk := range ch {
		if chunk.BifrostResponsesStreamResponse == nil {
			t.Fatal("nil responses stream chunk")
		}
		types = append(types, chunk.BifrostResponsesStreamResponse.Type)
	}
	want := []schemas.ResponsesStreamResponseType{schemas.ResponsesStreamResponseTypeCreated, schemas.ResponsesStreamResponseTypeOutputItemAdded, schemas.ResponsesStreamResponseTypeOutputItemDone, schemas.ResponsesStreamResponseTypeCompleted}
	if len(types) != len(want) {
		t.Fatalf("types=%v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types=%v want=%v", types, want)
		}
	}
}
