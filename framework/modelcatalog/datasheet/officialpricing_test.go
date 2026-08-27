package datasheet

import (
	"testing"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	configstoreTables "github.com/maximhq/bifrost/framework/configstore/tables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func officialPricingRow(model, provider string, inputCost float64) configstoreTables.TableModelPricing {
	return configstoreTables.TableModelPricing{
		Model:              model,
		Provider:           provider,
		Mode:               "chat",
		InputCostPerToken:  bifrost.Ptr(inputCost),
		OutputCostPerToken: bifrost.Ptr(inputCost * 2),
	}
}

func resolvedInputCost(t *testing.T, store *Store, provider schemas.ModelProvider, model string, scopes LookupScopes) float64 {
	t.Helper()
	pricing := store.resolvePricing(schemas.RoutingInfo{Provider: provider, Model: model}, schemas.ChatCompletionRequest, scopes)
	require.NotNil(t, pricing)
	require.NotNil(t, pricing.InputCostPerToken)
	return *pricing.InputCostPerToken
}

func TestOfficialPricingFallbackNormalizesStandardProviderCase(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("gpt-5.6-sol", "openai", "chat")] = officialPricingRow("gpt-5.6-sol", "openai", 1)

	assert.Equal(t, 1.0, resolvedInputCost(t, store, "OpenAI", "gpt-5.6-sol", LookupScopes{Provider: "OpenAI"}))
}

func TestOfficialPricingFallbackUsesPublisherForCustomProviderModels(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = officialPricingRow("deepseek-v4-flash", "deepseek", 2)
	store.pricingData[makeKey("glm-4.7", "zai", "chat")] = officialPricingRow("glm-4.7", "zai", 3)
	store.pricingData[makeKey("gpt-5.6-sol", "openai", "chat")] = officialPricingRow("gpt-5.6-sol", "openai", 4)
	store.pricingData[makeKey("o4-mini", "openai", "chat")] = officialPricingRow("o4-mini", "openai", 4.5)
	store.pricingData[makeKey("claude-sonnet-4-6", "anthropic", "chat")] = officialPricingRow("claude-sonnet-4-6", "anthropic", 5)
	store.pricingData[makeKey("grok-4", "xai", "chat")] = officialPricingRow("grok-4", "xai", 6)
	store.pricingData[makeKey("gemini-3-pro", "gemini", "chat")] = officialPricingRow("gemini-3-pro", "gemini", 7)
	store.pricingData[makeKey("codestral-latest", "mistral", "chat")] = officialPricingRow("codestral-latest", "mistral", 8)
	store.pricingData[makeKey("command-r-plus", "cohere", "chat")] = officialPricingRow("command-r-plus", "cohere", 9)

	assert.Equal(t, 2.0, resolvedInputCost(t, store, "CommandCode", "deepseek-v4-flash", LookupScopes{Provider: "CommandCode"}))
	assert.Equal(t, 2.0, resolvedInputCost(t, store, "CommandCode", "deepseek/deepseek-v4-flash", LookupScopes{Provider: "CommandCode"}))
	assert.Equal(t, 3.0, resolvedInputCost(t, store, "GLMProxy", "GLM-4.7", LookupScopes{Provider: "GLMProxy"}))
	assert.Equal(t, 4.0, resolvedInputCost(t, store, "custom", "openai/gpt-5.6-sol", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 4.5, resolvedInputCost(t, store, "custom", "openai/o4-mini", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 5.0, resolvedInputCost(t, store, "custom", "anthropic/claude-sonnet-4-6", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 6.0, resolvedInputCost(t, store, "custom", "xai/grok-4", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 7.0, resolvedInputCost(t, store, "custom", "google/gemini-3-pro", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 8.0, resolvedInputCost(t, store, "custom", "mistralai/codestral-latest", LookupScopes{Provider: "custom"}))
	assert.Equal(t, 9.0, resolvedInputCost(t, store, "custom", "cohere/command-r-plus", LookupScopes{Provider: "custom"}))
}

func TestOfficialPricingFallbackUsesDatasheetBaseModel(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = officialPricingRow("deepseek-v4-flash", "deepseek", 2)
	store.baseModelIndex["deepseek-v4-flash-0731"] = "deepseek-v4-flash"

	assert.Equal(t, 2.0, resolvedInputCost(t, store, "CommandCode", "deepseek/deepseek-v4-flash-0731", LookupScopes{Provider: "CommandCode"}))
}

func TestOfficialPricingFallbackKeepsExactProviderPricingFirst(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("deepseek-v4-flash", "opencode-go", "chat")] = officialPricingRow("deepseek-v4-flash", "opencode-go", 4)
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = officialPricingRow("deepseek-v4-flash", "deepseek", 2)

	assert.Equal(t, 4.0, resolvedInputCost(t, store, schemas.OpencodeGo, "deepseek-v4-flash", LookupScopes{Provider: string(schemas.OpencodeGo)}))
}

func TestOfficialPricingFallbackDoesNotGuessUnknownOrLlamaPublisher(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("internal-chat-model", "some-host", "chat")] = officialPricingRow("internal-chat-model", "some-host", 5)
	store.pricingData[makeKey("meta-llama/llama-3.3-70b-instruct", "openrouter", "chat")] = officialPricingRow("meta-llama/llama-3.3-70b-instruct", "openrouter", 6)

	assert.Nil(t, store.resolvePricing(schemas.RoutingInfo{Provider: "custom", Model: "internal-chat-model"}, schemas.ChatCompletionRequest, LookupScopes{Provider: "custom"}))
	assert.Nil(t, store.resolvePricing(schemas.RoutingInfo{Provider: "custom", Model: "meta-llama/llama-3.3-70b-instruct"}, schemas.ChatCompletionRequest, LookupScopes{Provider: "custom"}))
}

func TestOfficialPricingFallbackPreservesOverrideScopeAndWireModel(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("deepseek-v4-flash", "deepseek", "chat")] = officialPricingRow("deepseek-v4-flash", "deepseek", 2)

	commandCode := "CommandCode"
	require.NoError(t, store.SetOverrides([]configstoreTables.TablePricingOverride{
		{
			ID:               "publisher-scope-must-not-win",
			ScopeKind:        string(ScopeKindProvider),
			ProviderID:       bifrost.Ptr("deepseek"),
			MatchType:        string(MatchTypeExact),
			Pattern:          "deepseek/deepseek-v4-flash",
			RequestTypes:     []schemas.RequestType{schemas.ChatCompletionRequest},
			PricingPatchJSON: `{"input_cost_per_token":8}`,
		},
		{
			ID:               "actual-provider-wire-model",
			ScopeKind:        string(ScopeKindProvider),
			ProviderID:       &commandCode,
			MatchType:        string(MatchTypeExact),
			Pattern:          "deepseek/deepseek-v4-flash",
			RequestTypes:     []schemas.RequestType{schemas.ChatCompletionRequest},
			PricingPatchJSON: `{"input_cost_per_token":7}`,
		},
	}))

	assert.Equal(t, 7.0, resolvedInputCost(t, store, "CommandCode", "deepseek/deepseek-v4-flash", LookupScopes{Provider: "CommandCode"}))
}

func TestOfficialPricingFallbackPreservesGlobalAndExplicitZeroOverrides(t *testing.T) {
	store := newTestStore()
	store.pricingData[makeKey("glm-4.7", "zai", "chat")] = officialPricingRow("glm-4.7", "zai", 3)
	require.NoError(t, store.SetOverrides([]configstoreTables.TablePricingOverride{
		{
			ID:               "global-zero",
			ScopeKind:        string(ScopeKindGlobal),
			MatchType:        string(MatchTypeExact),
			Pattern:          "GLM-4.7",
			RequestTypes:     []schemas.RequestType{schemas.ChatCompletionRequest},
			PricingPatchJSON: `{"input_cost_per_token":0}`,
		},
	}))

	assert.Equal(t, 0.0, resolvedInputCost(t, store, "GLMProxy", "GLM-4.7", LookupScopes{Provider: "GLMProxy"}))
}

func TestOfficialPricingFallbackPreservesOverrideOnlyPricing(t *testing.T) {
	store := newTestStore()
	require.NoError(t, store.SetOverrides([]configstoreTables.TablePricingOverride{
		{
			ID:               "internal-model-price",
			ScopeKind:        string(ScopeKindGlobal),
			MatchType:        string(MatchTypeExact),
			Pattern:          "internal-chat-model",
			RequestTypes:     []schemas.RequestType{schemas.ChatCompletionRequest},
			PricingPatchJSON: `{"input_cost_per_token":9,"output_cost_per_token":10}`,
		},
	}))

	assert.Equal(t, 9.0, resolvedInputCost(t, store, "custom", "internal-chat-model", LookupScopes{Provider: "custom"}))
}
