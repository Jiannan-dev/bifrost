package semanticcache

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestPreLLMHookBypassesSearchMarkedRequest(t *testing.T) {
	plugin := &Plugin{}
	ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	ctx.SetValue(schemas.BifrostContextKeyBypassSemanticCache, true)
	req := &schemas.BifrostRequest{RequestType: schemas.ResponsesStreamRequest, ResponsesRequest: &schemas.BifrostResponsesRequest{}}
	got, shortCircuit, err := plugin.PreLLMHook(ctx, req)
	if err != nil || shortCircuit != nil || got != req {
		t.Fatalf("got=%p shortCircuit=%v err=%v", got, shortCircuit, err)
	}
}
