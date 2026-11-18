package agentkit

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"
)

const (
	defaultOpenRouterURL   = "https://openrouter.ai/api/v1"
	defaultModel           = "anthropic/claude-haiku-5.5"
	defaultMaxOutputTokens = 4096
)

// GenerateConfig caps how many tokens one model reply may use
// (MAX_OUTPUT_TOKENS, default 4096). Without a cap, OpenRouter reserves
// credit for the model's full output limit (65,536 tokens for Claude) and
// refuses the call with 402 when the account cannot cover that.
func GenerateConfig() (*genai.GenerateContentConfig, error) {
	maxTokens := defaultMaxOutputTokens
	if v := Env("MAX_OUTPUT_TOKENS", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("MAX_OUTPUT_TOKENS must be a positive number, got %q", v)
		}
		maxTokens = n
	}
	return &genai.GenerateContentConfig{MaxOutputTokens: int32(maxTokens)}, nil
}

// NewModel returns a model served by OpenRouter through its OpenAI-compatible
// Chat Completions API.
//
// modelVar names the agent's own model variable, such as TRIAGE_MODEL.
// OPENROUTER_MODEL is the fallback for both agents.
func NewModel(ctx context.Context, modelVar string) (model.LLM, string, error) {
	apiKey, err := RequireEnv("OPENROUTER_API_KEY")
	if err != nil {
		return nil, "", err
	}
	name := Env(modelVar, Env("OPENROUTER_MODEL", defaultModel))

	llm, err := openaimodel.NewModel(ctx, name, &openaimodel.ClientConfig{
		// Set explicitly: an empty key would make the SDK fall back to
		// OPENAI_API_KEY and send that key to OpenRouter.
		APIKey:  apiKey,
		BaseURL: Env("OPENROUTER_BASE_URL", defaultOpenRouterURL),
		API:     openaimodel.APIChatCompletions,
	})
	if err != nil {
		return nil, "", fmt.Errorf("create model %s: %w", name, err)
	}
	return llm, name, nil
}
