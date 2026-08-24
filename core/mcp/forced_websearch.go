package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

// ForcedWebSearchExecutor is an optional extension implemented by MCPManager.
// Bifrost uses it before provider dispatch to satisfy Claude Code's dedicated
// forced web_search auxiliary request without sending that request to a model
// which cannot execute Anthropic server tools.
type ForcedWebSearchExecutor interface {
	TryExecuteForcedWebSearch(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, bool, *schemas.BifrostError)
	TryExecuteForcedWebSearchStream(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, bool, *schemas.BifrostError)
}

const (
	forcedWebSearchMaxQueryBytes  = 16 << 10
	forcedWebSearchMaxOutputBytes = 2 << 20
	forcedWebSearchMaxSources     = 32
)

var forcedWebSearchSequence atomic.Uint64
var forcedWebSearchQueryPattern = regexp.MustCompile(`(?is)^\s*Perform a web search for the query:\s*(.+?)\s*$`)
var forcedWebSearchURLPattern = regexp.MustCompile(`https?://[^\s<>\]\)"']+`)

func (m *MCPManager) forcedWebSearchTool() string {
	if m == nil || m.toolsManager == nil {
		return ""
	}
	name, _ := m.toolsManager.webSearchFallbackTool.Load().(string)
	return name
}

// forcedWebSearchQuery recognizes the narrow request shape emitted by Claude
// Code after its outer WebSearch function has already selected a query.
// RequestUsesWebSearch reports whether a Responses request advertises native
// web search. Integrations use it to bypass answer-level semantic caching so a
// cache hit cannot suppress a fresh tool decision or replay stale search data.
func RequestUsesWebSearch(req *schemas.BifrostResponsesRequest) bool {
	if req == nil || req.Params == nil {
		return false
	}
	for _, tool := range req.Params.Tools {
		if tool.Type == schemas.ResponsesToolTypeWebSearch || tool.Type == schemas.ResponsesToolTypeWebSearchPreview {
			return true
		}
		if tool.Name != nil && (*tool.Name == "WebSearch" || *tool.Name == "web_search") {
			return true
		}
	}
	return false
}

func forcedWebSearchQuery(req *schemas.BifrostResponsesRequest) (string, bool) {
	if req == nil || req.Params == nil || len(req.Params.Tools) != 1 || req.Params.ToolChoice == nil {
		return "", false
	}
	tool := req.Params.Tools[0]
	if !RequestUsesWebSearch(req) || (tool.Type != schemas.ResponsesToolTypeWebSearch && tool.Type != schemas.ResponsesToolTypeWebSearchPreview) {
		return "", false
	}
	forcedChoice := false
	if choice := req.Params.ToolChoice.ResponsesToolChoiceStruct; choice != nil && choice.Name != nil && *choice.Name == "web_search" {
		forcedChoice = true
	}
	// The Anthropic integration currently normalizes Claude Code's auxiliary
	// forced choice to the string form "auto". Accept it only with the strict
	// single-web-search + exact query-message signature below.
	if choice := req.Params.ToolChoice.ResponsesToolChoiceStr; choice != nil && *choice == string(schemas.ResponsesToolChoiceTypeAuto) {
		forcedChoice = true
	}
	if !forcedChoice {
		return "", false
	}

	var text string
	for i := len(req.Input) - 1; i >= 0 && text == ""; i-- {
		msg := req.Input[i]
		if msg.Role == nil || *msg.Role != schemas.ResponsesInputMessageRoleUser || msg.Content == nil {
			continue
		}
		if msg.Content.ContentStr != nil {
			text = *msg.Content.ContentStr
		} else {
			var parts []string
			for _, block := range msg.Content.ContentBlocks {
				if block.Text != nil {
					parts = append(parts, *block.Text)
				}
			}
			text = strings.Join(parts, "\n")
		}
	}
	match := forcedWebSearchQueryPattern.FindStringSubmatch(text)
	if len(match) != 2 || strings.TrimSpace(match[1]) == "" {
		return "", false
	}
	query := strings.TrimSpace(match[1])
	if len(query) > forcedWebSearchMaxQueryBytes {
		return "", false
	}
	return query, true
}

func (m *MCPManager) executeForcedWebSearch(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, bool, *schemas.BifrostError) {
	toolName := m.forcedWebSearchTool()
	if toolName == "" {
		return nil, false, nil
	}
	query, ok := forcedWebSearchQuery(req)
	if !ok {
		return nil, false, nil
	}
	m.logger.Info("%s forced web_search auxiliary request intercepted; executing MCP tool %q", MCPLogPrefix, toolName)
	arguments, err := schemas.MarshalSorted(map[string]interface{}{"query": query})
	if err != nil {
		return nil, true, forcedWebSearchError(req, fmt.Sprintf("failed to encode search query: %v", err))
	}
	callID := fmt.Sprintf("bf_websearch_%d_%d", time.Now().UnixNano(), forcedWebSearchSequence.Add(1))
	result, bErr := m.ExecuteResponsesTool(ctx, &schemas.ResponsesToolMessage{
		CallID:    schemas.Ptr(callID),
		Name:      schemas.Ptr(toolName),
		Arguments: schemas.Ptr(string(arguments)),
	})
	if bErr != nil {
		return nil, true, bErr
	}
	sources := forcedWebSearchSources(result)
	if len(sources) == 0 {
		return nil, true, forcedWebSearchError(req, "configured MCP search tool returned no URL-bearing results")
	}
	m.logger.Info("%s forced web_search MCP tool %q returned %d URL-bearing sources", MCPLogPrefix, toolName, len(sources))
	status := "completed"
	stop := string(schemas.BifrostFinishReasonStop)
	n := 1
	item := schemas.ResponsesMessage{
		ID:     schemas.Ptr(callID),
		Type:   schemas.Ptr(schemas.ResponsesMessageTypeWebSearchCall),
		Status: &status,
		ResponsesToolMessage: &schemas.ResponsesToolMessage{Action: &schemas.ResponsesToolMessageActionStruct{
			ResponsesWebSearchToolCallAction: &schemas.ResponsesWebSearchToolCallAction{
				Type: "search", Query: schemas.Ptr(query), Queries: []string{query}, Sources: sources,
			},
		}},
	}
	responseID := "resp_" + callID
	return &schemas.BifrostResponsesResponse{
		ID: schemas.Ptr(responseID), Object: "response", CreatedAt: int(time.Now().Unix()), Model: req.Model,
		Output: []schemas.ResponsesMessage{item}, Status: &status, StopReason: &stop,
		Usage: &schemas.ResponsesResponseUsage{OutputTokensDetails: &schemas.ResponsesResponseOutputTokens{NumSearchQueries: &n}},
	}, true, nil
}

// TryExecuteForcedWebSearch executes a recognized Claude Code auxiliary search
// request synchronously. handled=false means normal provider dispatch must continue.
func (m *MCPManager) TryExecuteForcedWebSearch(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, bool, *schemas.BifrostError) {
	return m.executeForcedWebSearch(ctx, req)
}

// TryExecuteForcedWebSearchStream executes the MCP search once, then exposes the
// result as neutral Responses streaming events. The Anthropic integration's
// existing converter turns these into server_tool_use + web_search_tool_result SSE.
func (m *MCPManager) TryExecuteForcedWebSearchStream(ctx *schemas.BifrostContext, req *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, bool, *schemas.BifrostError) {
	response, handled, bErr := m.executeForcedWebSearch(ctx, req)
	if !handled || bErr != nil {
		return nil, handled, bErr
	}
	return forcedWebSearchResponseStream(response), true, nil
}

func forcedWebSearchResponseStream(response *schemas.BifrostResponsesResponse) chan *schemas.BifrostStreamChunk {
	ch := make(chan *schemas.BifrostStreamChunk, 4)
	go func() {
		defer close(ch)
		item := response.Output[0]
		inProgress := item
		inProgress.Status = schemas.Ptr("in_progress")
		outputIndex := 0
		ch <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
			Type: schemas.ResponsesStreamResponseTypeCreated, SequenceNumber: 0,
			Response: &schemas.BifrostResponsesResponse{ID: response.ID, Object: "response", CreatedAt: response.CreatedAt, Model: response.Model, Status: schemas.Ptr("in_progress")},
		}}
		ch <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
			Type: schemas.ResponsesStreamResponseTypeOutputItemAdded, SequenceNumber: 1, OutputIndex: &outputIndex, Item: &inProgress,
		}}
		ch <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
			Type: schemas.ResponsesStreamResponseTypeOutputItemDone, SequenceNumber: 2, OutputIndex: &outputIndex, Item: &item,
		}}
		ch <- &schemas.BifrostStreamChunk{BifrostResponsesStreamResponse: &schemas.BifrostResponsesStreamResponse{
			Type: schemas.ResponsesStreamResponseTypeCompleted, SequenceNumber: 3, Response: response,
		}}
	}()
	return ch
}

func forcedWebSearchError(req *schemas.BifrostResponsesRequest, message string) *schemas.BifrostError {
	provider, model := schemas.ModelProvider(""), ""
	if req != nil {
		provider, model = req.Provider, req.Model
	}
	return &schemas.BifrostError{IsBifrostError: true, Error: &schemas.ErrorField{Message: message}, ExtraFields: schemas.BifrostErrorExtraFields{
		RequestType: schemas.ResponsesRequest, Provider: provider, OriginalModelRequested: model, ResolvedModelUsed: model,
	}}
}

func forcedWebSearchOutput(result *schemas.ResponsesMessage) string {
	if result == nil {
		return ""
	}
	if result.ResponsesToolMessage != nil && result.ResponsesToolMessage.Output != nil {
		out := result.ResponsesToolMessage.Output
		if out.ResponsesToolCallOutputStr != nil {
			return *out.ResponsesToolCallOutputStr
		}
		if out.ResponsesFunctionToolCallOutputBlocks != nil {
			if b, err := json.Marshal(out.ResponsesFunctionToolCallOutputBlocks); err == nil {
				return string(b)
			}
		}
	}
	if result.Content != nil {
		if result.Content.ContentStr != nil {
			return *result.Content.ContentStr
		}
		if b, err := json.Marshal(result.Content.ContentBlocks); err == nil {
			return string(b)
		}
	}
	return ""
}

func forcedWebSearchSources(result *schemas.ResponsesMessage) []schemas.ResponsesWebSearchToolCallActionSearchSource {
	raw := strings.TrimSpace(forcedWebSearchOutput(result))
	if raw == "" {
		return nil
	}
	if len(raw) > forcedWebSearchMaxOutputBytes {
		raw = raw[:forcedWebSearchMaxOutputBytes]
	}
	var value interface{}
	var found []schemas.ResponsesWebSearchToolCallActionSearchSource
	if json.Unmarshal([]byte(raw), &value) == nil {
		collectForcedWebSearchSources(value, &found)
	}
	if len(found) == 0 {
		for _, url := range forcedWebSearchURLPattern.FindAllString(raw, -1) {
			found = append(found, newForcedWebSearchSource(url, url, raw))
		}
	}
	seen := make(map[string]bool)
	unique := found[:0]
	for _, source := range found {
		if source.URL == "" || seen[source.URL] {
			continue
		}
		seen[source.URL] = true
		unique = append(unique, source)
		if len(unique) == forcedWebSearchMaxSources {
			break
		}
	}
	return unique
}

func collectForcedWebSearchSources(value interface{}, found *[]schemas.ResponsesWebSearchToolCallActionSearchSource) {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			collectForcedWebSearchSources(item, found)
		}
	case map[string]interface{}:
		url, _ := v["url"].(string)
		if url == "" {
			url, _ = v["link"].(string)
		}
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			title, _ := v["title"].(string)
			if title == "" {
				title = url
			}
			encoded, _ := json.Marshal(v)
			*found = append(*found, newForcedWebSearchSource(url, title, string(encoded)))
		}
		for key, child := range v {
			if key != "url" && key != "link" && key != "title" {
				collectForcedWebSearchSources(child, found)
			}
		}
	}
}

func newForcedWebSearchSource(url, title, raw string) schemas.ResponsesWebSearchToolCallActionSearchSource {
	digest := sha256.Sum256([]byte(raw))
	encrypted := "bifrost:sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
	return schemas.ResponsesWebSearchToolCallActionSearchSource{Type: "url", URL: url, Title: schemas.Ptr(title), EncryptedContent: &encrypted}
}
