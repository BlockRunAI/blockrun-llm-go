package blockrun

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	// privateKeyRegex validates a 64-character hex string (with optional 0x prefix)
	privateKeyRegex = regexp.MustCompile(`^(0x)?[a-fA-F0-9]{64}$`)

	// modelRegex validates model IDs (provider/model format)
	modelRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+(/[a-zA-Z0-9._-]+)?$`)
)

// ValidatePrivateKey validates the format of a private key.
func ValidatePrivateKey(key string) error {
	if key == "" {
		return &ValidationError{
			Field:   "privateKey",
			Message: "Private key is required",
		}
	}

	if !privateKeyRegex.MatchString(key) {
		return &ValidationError{
			Field:   "privateKey",
			Message: "Private key must be a 64-character hex string (with optional 0x prefix)",
		}
	}

	return nil
}

// ValidateAPIURL validates the API URL format.
func ValidateAPIURL(apiURL string) error {
	if apiURL == "" {
		return &ValidationError{
			Field:   "apiURL",
			Message: "API URL is required",
		}
	}

	parsed, err := url.Parse(apiURL)
	if err != nil {
		return &ValidationError{
			Field:   "apiURL",
			Message: "Invalid URL format",
		}
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &ValidationError{
			Field:   "apiURL",
			Message: "URL must use http or https scheme",
		}
	}

	return nil
}

// ValidateModel validates the model ID format.
func ValidateModel(model string) error {
	if model == "" {
		return &ValidationError{
			Field:   "model",
			Message: "Model is required",
		}
	}

	if !modelRegex.MatchString(model) {
		return &ValidationError{
			Field:   "model",
			Message: "Invalid model format. Expected format: 'provider/model' or 'model-name'",
		}
	}

	return nil
}

// ValidateMaxTokens validates the max_tokens parameter.
func ValidateMaxTokens(maxTokens int) error {
	if maxTokens < 0 {
		return &ValidationError{
			Field:   "maxTokens",
			Message: "max_tokens must be non-negative",
		}
	}

	if maxTokens > 1000000 {
		return &ValidationError{
			Field:   "maxTokens",
			Message: "max_tokens exceeds maximum allowed value",
		}
	}

	return nil
}

// ValidateTemperature validates the temperature parameter.
func ValidateTemperature(temperature float64) error {
	if temperature < 0 {
		return &ValidationError{
			Field:   "temperature",
			Message: "temperature must be non-negative",
		}
	}

	if temperature > 2.0 {
		return &ValidationError{
			Field:   "temperature",
			Message: "temperature must be at most 2.0",
		}
	}

	return nil
}

// ValidateTopP validates the top_p parameter.
func ValidateTopP(topP float64) error {
	if topP < 0 {
		return &ValidationError{
			Field:   "topP",
			Message: "top_p must be non-negative",
		}
	}

	if topP > 1.0 {
		return &ValidationError{
			Field:   "topP",
			Message: "top_p must be at most 1.0",
		}
	}

	return nil
}

// ValidateResourceURL validates that a resource URL is safe.
func ValidateResourceURL(resourceURL, expectedBase string) (string, error) {
	if resourceURL == "" {
		return expectedBase + "/v1/chat/completions", nil
	}

	parsed, err := url.Parse(resourceURL)
	if err != nil {
		return "", &ValidationError{
			Field:   "resourceURL",
			Message: "Invalid resource URL format",
		}
	}

	// Ensure the URL uses a safe scheme
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", &ValidationError{
			Field:   "resourceURL",
			Message: "Resource URL must use http or https scheme",
		}
	}

	// Normalize the URL
	return strings.TrimSuffix(resourceURL, "/"), nil
}
