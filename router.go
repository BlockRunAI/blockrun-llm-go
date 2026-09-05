package blockrun

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// RoutingProfile controls which tier of models to use.
type RoutingProfile string

const (
	RoutingFree    RoutingProfile = "free"
	RoutingEco     RoutingProfile = "eco"
	RoutingAuto    RoutingProfile = "auto"
	RoutingPremium RoutingProfile = "premium"
)

// RoutingTier represents the complexity tier of a prompt.
type RoutingTier string

const (
	TierSimple    RoutingTier = "SIMPLE"
	TierMedium    RoutingTier = "MEDIUM"
	TierComplex   RoutingTier = "COMPLEX"
	TierReasoning RoutingTier = "REASONING"
)

// RoutingDecision contains the routing result metadata.
type RoutingDecision struct {
	Model      string  `json:"model"`
	Tier       string  `json:"tier"`
	Confidence float64 `json:"confidence"`
	Method     string  `json:"method"`
	Reasoning  string  `json:"reasoning"`
}

// SmartChatResponse wraps the chat response with routing metadata.
type SmartChatResponse struct {
	Response string          `json:"response"`
	Model    string          `json:"model"`
	Routing  RoutingDecision `json:"routing"`
}

// SmartChatOptions configures a SmartChat request.
type SmartChatOptions struct {
	System         string
	MaxTokens      int
	Temperature    float64
	RoutingProfile RoutingProfile
}

// routingTable maps (profile, tier) to the model to use.
//
// Moonshot flagship promotion (2026-06-15): kimi-k2.7 is now the catalog
// flagship (256K context, image+video input, reasoning_content). kimi-k2.6 and
// kimi-k2.5 are hidden:true in /v1/models as superseded; chat completions still
// serves them for clients pinned to their pricing, but the SmartChat default
// now points at k2.7.
//
// DeepSeek V4 family (2026-04-24, paid catalog): deepseek/deepseek-chat and
// deepseek/deepseek-reasoner are now V4 Flash chat / thinking modes
// ($0.20 in / $0.40 out per 1M, 1M context). deepseek/deepseek-v4-pro is
// the flagship paid SKU ($0.435 in / $0.87 out — the 75% launch promo
// became DeepSeek's permanent list price after 2026-05-31).
//
// NVIDIA free-tier refresh (2026-06-07 live sweep, every visible free model
// probed):
//   - nvidia/qwen3-next-80b-a3b-thinking hit NVIDIA END-OF-LIFE 2026-05-21
//     (HTTP 410 Gone; the gateway redirects pinned callers to
//     llama-4-maverick) — dropped as the Complex/Reasoning primary.
//   - nvidia/mistral-small-4-119b is timing out upstream (3/3 probes >60s) —
//     do not route to it.
//   - nvidia/deepseek-v4-flash probed healthy (896ms; recovered from the
//     05-09 NIM regression) and keeps 1M context — TierSimple primary,
//     replacing privacy-encumbered nvidia/gpt-oss-120b (NVIDIA's free tier
//     may use prompts for service improvement; a privacy-sensitive default
//     was wrong for auto-routing — direct calls by full ID still work).
//   - TierComplex → nvidia/qwen3-coder-480b (871ms, 480B MoE);
//     TierReasoning → nvidia/nemotron-3-nano-omni-30b-a3b-reasoning (681ms,
//     256K ctx, explicit reasoning + vision). Table now matches the Python
//     SDK's FREE_TIERS.
//
// Gemini 3.5 Flash promotion (2026-05-27): google/gemini-3.5-flash is the
// latest-generation Flash with built-in thinking mode ($0.50 in / $3.00 out,
// 1M context) and supersedes google/gemini-2.5-flash as the go-to Flash SKU.
// auto/MEDIUM and premium/SIMPLE now point at it; 2.5-flash remains available
// for clients pinned to its pricing.
//
// Claude Opus 4.8 promotion (2026-05-29): anthropic/claude-opus-4.8 is now the
// premium/COMPLEX flagship, replacing anthropic/claude-opus-4.5. Opus 4.8 is
// Anthropic's most capable model with a 1M-token context window. Older Opus
// IDs remain available for clients pinned to their pricing.
var routingTable = map[RoutingProfile]map[RoutingTier]string{
	RoutingFree: {
		TierSimple: "nvidia/deepseek-v4-flash", TierMedium: "nvidia/llama-4-maverick",
		TierComplex: "nvidia/qwen3-coder-480b", TierReasoning: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning",
	},
	RoutingEco: {
		TierSimple: "moonshot/kimi-k2.7", TierMedium: "deepseek/deepseek-chat",
		TierComplex: "google/gemini-2.5-pro", TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingAuto: {
		TierSimple: "moonshot/kimi-k2.7", TierMedium: "google/gemini-3.5-flash",
		TierComplex: "google/gemini-3.1-pro", TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingPremium: {
		TierSimple: "google/gemini-3.5-flash", TierMedium: "openai/gpt-5.5",
		TierComplex: "anthropic/claude-opus-4.8", TierReasoning: "openai/o3",
	},
}

// Precompiled regex patterns for the scoring dimensions.
var (
	reCode       = regexp.MustCompile("(?i)(```|def |func |class |import |const |let |var |function |return |if\\s*\\(|for\\s*\\(|\\{\\s*\\n|=>|->)")
	reReasoning  = regexp.MustCompile("(?i)(explain|why|how does|analyze|compare|evaluate|assess|reason|justify|proof|derive|theorem|hypothesis)")
	reTechnical  = regexp.MustCompile("(?i)(algorithm|database|API|kubernetes|docker|microservice|architecture|infrastructure|latency|throughput|scalab|distribut|concurren)")
	reCreative   = regexp.MustCompile("(?i)(write a (poem|story|song|essay)|creative|imagine|fiction|narrative|brainstorm)")
	reSimple     = regexp.MustCompile("(?i)^(hi|hello|hey|thanks|thank you|yes|no|ok|okay|sure|what time|what day|how are you|goodbye|bye)\\b")
	reMultiStep  = regexp.MustCompile("(?i)(step[- ]by[- ]step|first[,.].*then|1\\.|\\bplan\\b.*\\bfor\\b|multi[- ]step|breakdown|walk me through|outline the process)")
	reAgentic    = regexp.MustCompile("(?i)(search (for|the)|look up|find (me|the|all)|browse|fetch|retrieve|scrape|crawl|automate|execute|run the)")
)

// scorePrompt computes a complexity score for the given prompt.
// All computation is local; no API calls are made.
func scorePrompt(prompt string) (float64, string) {
	var score float64
	var reasons []string

	// 1. Code presence (weight 0.15)
	if reCode.MatchString(prompt) {
		score += 0.15
		reasons = append(reasons, "code_detected")
	}

	// 2. Reasoning markers (weight 0.18)
	if reReasoning.MatchString(prompt) {
		score += 0.18
		reasons = append(reasons, "reasoning_markers")
	}

	// 3. Technical terms (weight 0.10)
	if reTechnical.MatchString(prompt) {
		score += 0.10
		reasons = append(reasons, "technical_terms")
	}

	// 4. Creative markers (weight 0.05)
	if reCreative.MatchString(prompt) {
		score += 0.05
		reasons = append(reasons, "creative_markers")
	}

	// 5. Simple indicators (weight -0.02, negative)
	if reSimple.MatchString(prompt) {
		score -= 0.02
		reasons = append(reasons, "simple_indicator")
	}

	// 6. Multi-step patterns (weight 0.12)
	if reMultiStep.MatchString(prompt) {
		score += 0.12
		reasons = append(reasons, "multi_step")
	}

	// 7. Token count proxy: word count (weight 0.08)
	wordCount := len(strings.Fields(prompt))
	if wordCount > 100 {
		score += 0.08
		reasons = append(reasons, "long_prompt")
	} else if wordCount > 50 {
		score += 0.04
		reasons = append(reasons, "medium_prompt")
	}

	// 8. Agentic patterns (weight 0.04)
	if reAgentic.MatchString(prompt) {
		score += 0.04
		reasons = append(reasons, "agentic_patterns")
	}

	reasoning := strings.Join(reasons, ", ")
	if reasoning == "" {
		reasoning = "no_signals"
	}

	return score, reasoning
}

// tierFromScore maps a numeric score to a RoutingTier.
func tierFromScore(score float64) RoutingTier {
	switch {
	case score < 0.0:
		return TierSimple
	case score < 0.3:
		return TierMedium
	case score < 0.5:
		return TierComplex
	default:
		return TierReasoning
	}
}

// routePrompt scores a prompt and selects a model based on the routing profile.
func routePrompt(prompt string, profile RoutingProfile) (RoutingDecision, error) {
	score, reasoning := scorePrompt(prompt)
	tier := tierFromScore(score)

	tiers, ok := routingTable[profile]
	if !ok {
		return RoutingDecision{}, fmt.Errorf("unknown routing profile: %s", profile)
	}

	model, ok := tiers[tier]
	if !ok {
		return RoutingDecision{}, fmt.Errorf("no model for tier %s in profile %s", tier, profile)
	}

	return RoutingDecision{
		Model:      model,
		Tier:       string(tier),
		Confidence: score,
		Method:     "claw_router_v1",
		Reasoning:  reasoning,
	}, nil
}

// SmartChat scores the prompt locally, selects the best model, and sends the chat request.
// The routing computation is entirely local — only the final chat call hits the API.
func (c *LLMClient) SmartChat(ctx context.Context, prompt string, opts *SmartChatOptions) (*SmartChatResponse, error) {
	if prompt == "" {
		return nil, &ValidationError{Field: "prompt", Message: "Prompt is required"}
	}

	// Defaults
	profile := RoutingAuto
	if opts != nil && opts.RoutingProfile != "" {
		profile = opts.RoutingProfile
	}

	decision, err := routePrompt(prompt, profile)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	// Make the chat call
	var response string
	if opts != nil && opts.System != "" {
		response, err = c.ChatWithSystem(ctx, decision.Model, prompt, opts.System)
	} else {
		response, err = c.Chat(ctx, decision.Model, prompt)
	}
	if err != nil {
		return nil, err
	}

	return &SmartChatResponse{
		Response: response,
		Model:    decision.Model,
		Routing:  decision,
	}, nil
}
