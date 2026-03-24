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
var routingTable = map[RoutingProfile]map[RoutingTier]string{
	RoutingFree: {
		TierSimple: "nvidia/gpt-oss-120b", TierMedium: "nvidia/gpt-oss-120b",
		TierComplex: "nvidia/gpt-oss-120b", TierReasoning: "nvidia/gpt-oss-120b",
	},
	RoutingEco: {
		TierSimple: "nvidia/kimi-k2.5", TierMedium: "deepseek/deepseek-chat",
		TierComplex: "google/gemini-2.5-pro", TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingAuto: {
		TierSimple: "nvidia/kimi-k2.5", TierMedium: "google/gemini-2.5-flash",
		TierComplex: "google/gemini-3.1-pro", TierReasoning: "deepseek/deepseek-reasoner",
	},
	RoutingPremium: {
		TierSimple: "google/gemini-2.5-flash", TierMedium: "openai/gpt-5.4",
		TierComplex: "anthropic/claude-opus-4.5", TierReasoning: "openai/o3",
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
