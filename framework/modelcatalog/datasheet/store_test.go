package datasheet

import (
	"slices"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	configstoreTables "github.com/maximhq/bifrost/framework/configstore/tables"
)

func TestPricingLookupsNormalizeRuntimeProvider(t *testing.T) {
	const model = "deepseek-ai/DeepSeek-V4-Flash-0731"
	inputCost := 0.00000014
	provider := schemas.ModelProvider("together")
	s := NewTestStore(nil)
	s.pricingData[makeKey(model, "together_ai", "chat")] = configstoreTables.TableModelPricing{
		Model:             model,
		Provider:          "together_ai",
		Mode:              "chat",
		InputCostPerToken: &inputCost,
	}

	row := s.Get(model, provider, schemas.ChatCompletionRequest)
	if row == nil || row.InputCostPerToken == nil || *row.InputCostPerToken != inputCost {
		t.Fatalf("Get() did not resolve the catalog provider: %#v", row)
	}

	pricing := s.GetPricingEntryForModel(model, provider)
	if pricing == nil || pricing.InputCostPerToken == nil || *pricing.InputCostPerToken != inputCost {
		t.Fatalf("GetPricingEntryForModel() did not resolve the catalog provider: %#v", pricing)
	}

	capability := s.GetCapabilityEntry(model, provider)
	if capability == nil || capability.InputCostPerToken == nil || *capability.InputCostPerToken != inputCost {
		t.Fatalf("GetCapabilityEntry() did not resolve the catalog provider: %#v", capability)
	}

	s.mu.Lock()
	s.rebuildDatasheetViewUnsafe()
	s.mu.Unlock()
	if got := s.DatasheetModelsForProvider(provider); !slices.Equal(got, []string{model}) {
		t.Fatalf("DatasheetModelsForProvider() = %v, want [%s]", got, model)
	}
	if got := s.DatasheetProviders(); !slices.Equal(got, []schemas.ModelProvider{provider}) {
		t.Fatalf("DatasheetProviders() = %v, want [%s]", got, provider)
	}
}

func TestGetPricingEntryForModelUsesSharedOfficialFallback(t *testing.T) {
	store := NewTestStore(nil)
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = configstoreTables.TableModelPricing{
		Model: "deepseek-v4-flash", Provider: "deepseek", Mode: "chat", MaxInputTokens: capabilityIntPtr(1_000_000),
	}
	store.pricingData[makeKey("glm-5.3-flash", "zai", "chat")] = configstoreTables.TableModelPricing{
		Model: "glm-5.3-flash", Provider: "zai", Mode: "chat", MaxInputTokens: capabilityIntPtr(1_048_576),
	}
	store.pricingData[makeKey("gpt-5.6-sol", "openai", "chat")] = configstoreTables.TableModelPricing{
		Model: "gpt-5.6-sol", Provider: "openai", Mode: "chat", MaxInputTokens: capabilityIntPtr(1_050_000),
	}

	for _, tc := range []struct {
		provider schemas.ModelProvider
		model    string
		want     int
	}{
		{"CommandCode", "deepseek/deepseek-v4-flash", 1_000_000},
		{"CommandCode", "z-ai/glm-5.3-flash", 1_048_576},
		{"OpenAI", "gpt-5.6-sol", 1_050_000},
	} {
		entry := store.GetPricingEntryForModel(tc.model, tc.provider)
		if entry == nil || entry.MaxInputTokens == nil || *entry.MaxInputTokens != tc.want {
			t.Fatalf("lookup %s/%s = %#v, want max_input_tokens %d", tc.provider, tc.model, entry, tc.want)
		}
	}
}

func TestGetPricingEntryForModelKeepsOriginalMatch(t *testing.T) {
	store := NewTestStore(nil)
	store.pricingData[makeKey("deepseek-v4-flash", "opencode-go", "chat")] = configstoreTables.TableModelPricing{
		Model: "deepseek-v4-flash", Provider: "opencode-go", Mode: "chat", InputCostPerToken: capabilityFloatPtr(4),
	}
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = configstoreTables.TableModelPricing{
		Model: "deepseek-v4-flash", Provider: "deepseek", Mode: "chat", InputCostPerToken: capabilityFloatPtr(2),
	}

	entry := store.GetPricingEntryForModel("deepseek-v4-flash", schemas.OpencodeGo)
	if entry == nil || entry.InputCostPerToken == nil || *entry.InputCostPerToken != 4 {
		t.Fatalf("original provider match must win, got %#v", entry)
	}
}

func capabilityFloatPtr(v float64) *float64 { return &v }

func TestDeprecatedDatasheetModelsForProviderUsesRebuiltIndex(t *testing.T) {
	s := NewTestStore(nil)
	s.mu.Lock()
	s.pricingData[makeKey("deprecated-b", "openai", "chat")] = configstoreTables.TableModelPricing{
		Model:        "deprecated-b",
		Provider:     "openai",
		Mode:         "chat",
		IsDeprecated: true,
	}
	s.pricingData[makeKey("deprecated-a", "openai", "chat")] = configstoreTables.TableModelPricing{
		Model:        "deprecated-a",
		Provider:     "openai",
		Mode:         "chat",
		IsDeprecated: true,
	}
	s.pricingData[makeKey("deprecated-a", "openai", "responses")] = configstoreTables.TableModelPricing{
		Model:        "deprecated-a",
		Provider:     "openai",
		Mode:         "responses",
		IsDeprecated: true,
	}
	s.pricingData[makeKey("active", "openai", "chat")] = configstoreTables.TableModelPricing{
		Model:    "active",
		Provider: "openai",
		Mode:     "chat",
	}
	s.pricingData[makeKey("deprecated-vertex", "vertex_ai", "chat")] = configstoreTables.TableModelPricing{
		Model:        "deprecated-vertex",
		Provider:     "vertex_ai",
		Mode:         "chat",
		IsDeprecated: true,
	}
	s.rebuildDatasheetViewUnsafe()
	s.mu.Unlock()

	got := s.DeprecatedDatasheetModelsForProvider(schemas.OpenAI)
	want := []string{"deprecated-a", "deprecated-b"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected deprecated OpenAI models %v, got %v", want, got)
	}

	got[0] = "mutated"
	got = s.DeprecatedDatasheetModelsForProvider(schemas.OpenAI)
	if !slices.Equal(got, want) {
		t.Fatalf("expected defensive copy from index %v, got %v", want, got)
	}

	got = s.DeprecatedDatasheetModelsForProvider(schemas.Vertex)
	want = []string{"deprecated-vertex"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected deprecated Vertex models %v, got %v", want, got)
	}
}
