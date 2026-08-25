package anthropic

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

// Anthropic list-models used to strip the first known-provider prefix via
// ParseModelString. That matches native Anthropic (bare claude-* ids) but
// breaks Bifrost as a gateway: CommandCode/deepseek/deepseek-v4-flash became
// deepseek/deepseek-v4-flash, which ParseModelString then routed to the
// built-in deepseek provider.
func TestToAnthropicListModelsResponseKeepsProviderPrefixedID(t *testing.T) {
	const customProvider schemas.ModelProvider = "CommandCode"
	schemas.RegisterKnownProvider(customProvider)
	t.Cleanup(func() { schemas.UnregisterKnownProvider(customProvider) })

	got := ToAnthropicListModelsResponse(&schemas.BifrostListModelsResponse{
		Data: []schemas.Model{
			{ID: "openai/gpt-4o"},
			{ID: "CommandCode/deepseek/deepseek-v4-flash"},
			{ID: "anthropic/claude-sonnet-4-5"},
		},
	})
	if got == nil {
		t.Fatal("expected response")
	}
	if len(got.Data) != 3 {
		t.Fatalf("len(Data) = %d, want 3", len(got.Data))
	}
	want := []string{
		"openai/gpt-4o",
		"CommandCode/deepseek/deepseek-v4-flash",
		"anthropic/claude-sonnet-4-5",
	}
	for i, id := range want {
		if got.Data[i].ID != id {
			t.Errorf("Data[%d].ID = %q, want %q", i, got.Data[i].ID, id)
		}
	}
}
