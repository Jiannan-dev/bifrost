package bifrost

import (
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// isReasoningItemIDRejection reports whether err is an upstream refusal to
// resolve a replayed reasoning item id.
//
// OpenAI-compatible Responses hosts with store:false (OpenCode Go in
// particular) treat input[].id on a reasoning item as a server-side handle.
// Bifrost used to mint a placeholder `rs_*` when converting unmarked Anthropic
// thinking blocks, so the follow-up turn 400s:
//
//	Referenced reasoning item 'rs_...' was not found or has expired.
//
// Retrying the same id cannot help. The match is gated on 400 and on that
// phrasing: neighbouring encrypted-content refusals name a different field and
// keep their own fail-soft, which strips ciphertext and keeps ids -- the
// opposite rewrite.
func isReasoningItemIDRejection(err *schemas.BifrostError) bool {
	if err == nil || err.Error == nil {
		return false
	}
	if err.StatusCode == nil || *err.StatusCode != 400 {
		return false
	}
	message := strings.ToLower(err.Error.Message)
	if err.Error.Code != nil {
		message = message + " " + strings.ToLower(*err.Error.Code)
	}
	if !strings.Contains(message, "referenced reasoning item") {
		return false
	}
	return strings.Contains(message, "not found") || strings.Contains(message, "expired")
}

// stripResponsesReasoningItemIDs removes ids from every reasoning-typed input
// item, reporting whether anything changed. Summaries, encrypted_content,
// thinking text, and ordinary message ids are preserved: the upstream refused
// the handle, not the client's own bytes.
//
// Thinking-only items (Type=reasoning, no ResponsesReasoning struct) must be
// included. A ResponsesReasoning-only check misses Claude Code thinking blocks
// and would spend the one retry on an unchanged payload.
//
// The caller's slice and structs are shared with plugins and the transport
// layer, so the rewrite builds a new slice and nils id on copies rather than
// mutating in place.
func stripResponsesReasoningItemIDs(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) bool {
	inputRef, rawBodyRef := encryptedReasoningCarriers(req)
	if inputRef == nil {
		return false
	}

	if ctx != nil {
		if isLargePayload, ok := ctx.Value(schemas.BifrostContextKeyLargePayloadMode).(bool); ok && isLargePayload {
			return false
		}
		if useRawBody, ok := ctx.Value(schemas.BifrostContextKeyUseRawRequestBody).(bool); ok && useRawBody {
			return stripRawResponsesReasoningItemIDs(rawBodyRef)
		}
	}

	if len(*inputRef) == 0 {
		return false
	}

	input := *inputRef
	stripped := make([]schemas.ResponsesMessage, 0, len(input))
	changed := false
	for _, message := range input {
		if message.IsReasoningItem() && message.ID != nil {
			message.ID = nil
			changed = true
		}
		stripped = append(stripped, message)
	}
	if !changed {
		return false
	}
	*inputRef = stripped
	return true
}

// stripRawResponsesReasoningItemIDs applies the same rewrite to a buffered raw
// request body. Each item keeps its original bytes minus the deleted id so
// fields Bifrost's schema does not model are not lost.
func stripRawResponsesReasoningItemIDs(rawBody *[]byte) bool {
	if rawBody == nil || len(*rawBody) == 0 {
		return false
	}
	body := *rawBody
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}

	items := make([]string, 0, len(input.Array()))
	changed := false
	for _, item := range input.Array() {
		if !isRawResponsesReasoningItem(item) || !item.Get("id").Exists() {
			items = append(items, item.Raw)
			continue
		}
		updated, err := sjson.Delete(item.Raw, "id")
		if err != nil {
			items = append(items, item.Raw)
			continue
		}
		items = append(items, updated)
		changed = true
	}
	if !changed {
		return false
	}

	updated, err := sjson.SetRawBytes(body, "input", []byte("["+strings.Join(items, ",")+"]"))
	if err != nil {
		return false
	}
	*rawBody = updated
	return true
}

func isRawResponsesReasoningItem(item gjson.Result) bool {
	if item.Get("type").String() == string(schemas.ResponsesMessageTypeReasoning) {
		return true
	}
	return item.Get("encrypted_content").Exists()
}
