package datasheet

import (
	"slices"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

// canonicalStandardProvider maps a case-insensitive built-in provider name to
// the canonical identifier stored in the pricing datasheet. It deliberately
// does not normalize custom provider names.
func canonicalStandardProvider(provider string) (string, bool) {
	for _, standard := range schemas.StandardProviders {
		if strings.EqualFold(provider, string(standard)) {
			return normalizeProvider(string(standard)), true
		}
	}
	return "", false
}

type catalogLookupCandidate struct {
	model    string
	provider string
}

// catalogLookupCandidates keeps the upstream provider/model lookup first and
// adds first-party publisher candidates only after that lookup would miss.
// Pricing and model-info reads share this ordering so fallback behavior cannot
// drift between call paths.
func (s *Store) catalogLookupCandidates(models []string, provider string) []catalogLookupCandidate {
	candidates := make([]catalogLookupCandidate, 0, len(models)*4)
	appendCandidate := func(model, provider string) {
		if model == "" {
			return
		}
		candidate := catalogLookupCandidate{model: model, provider: provider}
		if !slices.Contains(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}

	catalogProvider := normalizeProvider(provider)
	for _, model := range models {
		appendCandidate(model, catalogProvider)
	}
	if canonicalProvider, ok := canonicalStandardProvider(provider); ok {
		for _, model := range models {
			appendCandidate(model, canonicalProvider)
		}
	}
	if provider == "" {
		return candidates
	}
	for _, model := range models {
		for _, normalizedModel := range s.officialPricingModelCandidates(model) {
			if officialProvider, ok := officialProviderForModel(normalizedModel); ok {
				appendCandidate(normalizedModel, officialProvider)
			}
		}
	}
	return candidates
}

// officialPricingModelCandidates returns conservative model-name variants for
// publisher pricing. The exact wire model is tried first; a single namespace
// prefix is removed only as a fallback (for example deepseek/deepseek-v4-flash).
func (s *Store) officialPricingModelCandidates(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	candidates := make([]string, 0, 6)
	appendCandidate := func(candidate string) {
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	appendVariants := func(candidate string) {
		appendCandidate(candidate)
		appendCandidate(strings.ToLower(candidate))
		appendCandidate(s.BaseModelName(candidate))
		appendCandidate(strings.ToLower(s.BaseModelName(candidate)))
	}

	appendVariants(model)
	if slash := strings.IndexByte(model, '/'); slash >= 0 && slash+1 < len(model) {
		appendVariants(model[slash+1:])
	}
	return candidates
}

// officialProviderForModel identifies publishers only for model families with
// a high-confidence first-party datasheet provider. The returned provider is a
// catalog identifier; pricing values remain sourced from the synced datasheet.
func officialProviderForModel(model string) (string, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case schemas.IsOpenAIModel(model):
		return string(schemas.OpenAI), true
	case schemas.IsAnthropicModel(model):
		return string(schemas.Anthropic), true
	case isDeepSeekModel(model):
		return string(schemas.DeepSeek), true
	case schemas.IsGLMModel(model):
		return "zai", true
	case schemas.IsGrokModel(model):
		return string(schemas.XAI), true
	case schemas.IsGeminiModel(model):
		return string(schemas.Gemini), true
	case schemas.IsMistralModel(model):
		return string(schemas.Mistral), true
	case isCohereModel(model):
		return string(schemas.Cohere), true
	default:
		return "", false
	}
}

func isDeepSeekModel(model string) bool {
	return strings.HasPrefix(model, "deepseek-") ||
		strings.HasPrefix(model, "deepseek/") ||
		strings.HasPrefix(model, "deepseek-ai/") ||
		strings.Contains(model, "/deepseek-")
}

func isCohereModel(model string) bool {
	return schemas.IsCohereModel(model) ||
		strings.HasPrefix(model, "command-") ||
		strings.HasPrefix(model, "embed-english-") ||
		strings.HasPrefix(model, "embed-multilingual-") ||
		strings.HasPrefix(model, "rerank-english-") ||
		strings.HasPrefix(model, "rerank-multilingual-")
}
