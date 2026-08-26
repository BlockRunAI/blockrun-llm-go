package blockrun

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	// BaseChainID is the chain ID for Base mainnet
	BaseChainID = 8453

	// USDCBase is the USDC contract address on Base
	USDCBase = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
)

// createNonce generates a random bytes32 nonce for EIP-3009.
func createNonce() (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return "0x" + common.Bytes2Hex(nonce), nil
}

// BlockRunServiceCode is BlockRun's x402 builder code — the ERC-8021 Schema 2
// service code (s) that tags every payment this SDK signs as BlockRun-originated
// for on-chain attribution.
// See https://docs.cdp.coinbase.com/x402/core-concepts/builder-codes
const BlockRunServiceCode = "blockrun"

// withBuilderCodeServiceCode merges BlockRun's service code (s) into the
// payload's builder-code extension, preserving any app code (a) the server
// echoed back in its 402. The CDP facilitator reads builder-code.info.s and
// encodes it into the settlement calldata suffix — no CBOR happens client-side.
func withBuilderCodeServiceCode(extensions map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range extensions {
		merged[k] = v
	}

	builderCode := map[string]any{}
	info := map[string]any{}
	if bc, ok := merged["builder-code"].(map[string]any); ok {
		for k, v := range bc {
			builderCode[k] = v
		}
		if existingInfo, ok := bc["info"].(map[string]any); ok {
			for k, v := range existingInfo {
				info[k] = v
			}
		}
	}
	info["s"] = []string{BlockRunServiceCode}
	builderCode["info"] = info
	merged["builder-code"] = builderCode
	return merged
}

// CreatePaymentPayload creates a signed x402 v2 payment payload.
//
// This uses EIP-712 typed data signing to create a payment authorization
// that the CDP facilitator can verify and settle.
//
// SECURITY: The private key is used ONLY for local signing.
// Only the signature is sent to the server - the key NEVER leaves your machine.
func CreatePaymentPayload(
	privateKey *ecdsa.PrivateKey,
	recipient string,
	amount string,
	network string,
	resourceURL string,
	resourceDescription string,
	maxTimeoutSeconds int,
	extra map[string]any,
	extensions map[string]any,
) (string, error) {
	// Get wallet address from private key
	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Current timestamp
	now := time.Now().Unix()
	validAfter := now - 600 // 10 minutes before (allows for clock skew)
	validBefore := now + int64(maxTimeoutSeconds)

	// Generate random nonce
	nonce, err := createNonce()
	if err != nil {
		return "", err
	}

	// Parse amount as big.Int
	amountBig, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return "", fmt.Errorf("invalid amount: %s", amount)
	}

	// Get USDC domain parameters
	usdcName := "USD Coin"
	usdcVersion := "2"
	if extra != nil {
		if name, ok := extra["name"].(string); ok {
			usdcName = name
		}
		if version, ok := extra["version"].(string); ok {
			usdcVersion = version
		}
	}

	// EIP-712 typed data for TransferWithAuthorization
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferWithAuthorization": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "validAfter", Type: "uint256"},
				{Name: "validBefore", Type: "uint256"},
				{Name: "nonce", Type: "bytes32"},
			},
		},
		PrimaryType: "TransferWithAuthorization",
		Domain: apitypes.TypedDataDomain{
			Name:              usdcName,
			Version:           usdcVersion,
			ChainId:           math.NewHexOrDecimal256(BaseChainID),
			VerifyingContract: USDCBase,
		},
		Message: apitypes.TypedDataMessage{
			"from":        walletAddress.Hex(),
			"to":          recipient,
			"value":       amountBig.String(),
			"validAfter":  big.NewInt(validAfter).String(),
			"validBefore": big.NewInt(validBefore).String(),
			"nonce":       nonce,
		},
	}

	// Hash the typed data
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", fmt.Errorf("failed to hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return "", fmt.Errorf("failed to hash message: %w", err)
	}

	// Create final hash: keccak256("\x19\x01" + domainSeparator + messageHash)
	rawData := append([]byte{0x19, 0x01}, domainSeparator...)
	rawData = append(rawData, messageHash...)
	hash := crypto.Keccak256Hash(rawData)

	// Sign the hash
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	// Fix signature v value (Ethereum uses 27/28, go-ethereum uses 0/1)
	if signature[64] < 27 {
		signature[64] += 27
	}

	// Build extra for response
	responseExtra := map[string]any{
		"name":    usdcName,
		"version": usdcVersion,
	}

	// Create payment payload
	payload := PaymentPayload{
		X402Version: 2,
		Resource: ResourceInfo{
			URL:         resourceURL,
			Description: resourceDescription,
			MimeType:    "application/json",
		},
		Accepted: PaymentOption{
			Scheme:            "exact",
			Network:           network,
			Amount:            amount,
			Asset:             USDCBase,
			PayTo:             recipient,
			MaxTimeoutSeconds: maxTimeoutSeconds,
			Extra:             responseExtra,
		},
		Payload: PaymentData{
			Signature: "0x" + common.Bytes2Hex(signature),
			Authorization: TransferAuthorization{
				From:        walletAddress.Hex(),
				To:          recipient,
				Value:       amount,
				ValidAfter:  strconv.FormatInt(validAfter, 10),
				ValidBefore: strconv.FormatInt(validBefore, 10),
				Nonce:       nonce,
			},
		},
		Extensions: withBuilderCodeServiceCode(extensions),
	}

	// Encode as JSON then base64
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// ParsePaymentRequired parses payment requirements from a 402 response.
//
// The canonical form is the base64-encoded payment-required header. Some
// gateways instead carry the requirements as raw JSON in the response body, so
// a payload that is already JSON is accepted verbatim: base64 has no '{' in its
// alphabet, which makes the two forms unambiguous.
func ParsePaymentRequired(headerValue string) (*PaymentRequirement, error) {
	trimmed := strings.TrimSpace(headerValue)
	if trimmed == "" {
		return nil, fmt.Errorf("empty payment required header")
	}

	raw := []byte(trimmed)
	if !strings.HasPrefix(trimmed, "{") {
		decoded, err := base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			return nil, fmt.Errorf("failed to decode payment required header: %w", err)
		}
		raw = decoded
	}

	var req PaymentRequirement
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("failed to parse payment required: %w", err)
	}

	return &req, nil
}

// networkChain classifies a 402 option's network string as the chain this SDK
// would have to sign with, or "" when it recognises neither.
//
// Both the short names BlockRun's gateways use ("base", "solana") and the
// CAIP-2 spellings ("eip155:8453", "solana:5eykt4Us…") are accepted, along with
// their testnet variants ("base-sepolia", "solana-devnet").
func networkChain(network string) string {
	n := strings.ToLower(strings.TrimSpace(network))
	switch {
	case n == chainSolana, strings.HasPrefix(n, chainSolana+":"), strings.HasPrefix(n, chainSolana+"-"):
		return chainSolana
	case n == chainBase, strings.HasPrefix(n, chainBase+"-"), strings.HasPrefix(n, "eip155:"):
		return chainBase
	default:
		return ""
	}
}

// ExtractPaymentDetails extracts payment details from a PaymentRequirement,
// expressing no chain preference: the first option is used.
//
// Clients should prefer ExtractPaymentDetailsForChain — an option this client
// cannot sign is worse than no option at all.
func ExtractPaymentDetails(req *PaymentRequirement) (*PaymentOption, error) {
	return ExtractPaymentDetailsForChain(req, "")
}

// ExtractPaymentDetailsForChain selects the payment option this client can
// actually sign and normalises its amount.
//
// Ordering in accepts is the server's preference, not the client's capability:
// a gateway offering [base, solana] used to hand a Solana client the Base
// option, which then died on "invalid asset mint" with a payable Solana option
// untouched in accepts[1]. Selection therefore follows chain, falling back to
// accepts[0] only when no option's network is classifiable — an unrecognised
// network is not evidence of a mismatch, so it is still attempted.
//
// chain is "base", "solana", or "" for no preference.
func ExtractPaymentDetailsForChain(req *PaymentRequirement, chain string) (*PaymentOption, error) {
	if req == nil || len(req.Accepts) == 0 {
		return nil, fmt.Errorf("no payment options in payment required response")
	}

	option, err := selectPaymentOption(req.Accepts, chain)
	if err != nil {
		return nil, err
	}

	// Support both v1 (maxAmountRequired) and v2 (amount) formats
	if option.Amount == "" {
		if extra, ok := option.Extra["maxAmountRequired"].(string); ok {
			option.Amount = extra
		}
	}

	if option.Amount == "" {
		return nil, fmt.Errorf("no amount found in payment requirements")
	}

	return option, nil
}

// selectPaymentOption returns a copy of the first option payable on chain, or
// an error naming the mismatch when every option belongs to another chain.
func selectPaymentOption(accepts []PaymentOption, chain string) (*PaymentOption, error) {
	if chain == "" {
		option := accepts[0]
		return &option, nil
	}

	unknown := -1
	for i := range accepts {
		switch networkChain(accepts[i].Network) {
		case chain:
			option := accepts[i]
			return &option, nil
		case "":
			if unknown < 0 {
				unknown = i
			}
		}
	}

	// Nothing matched. An unrecognised network is not evidence of a mismatch —
	// it may well be payable — so try it before declaring one.
	if unknown >= 0 {
		option := accepts[unknown]
		return &option, nil
	}

	offered := make([]string, 0, len(accepts))
	for i := range accepts {
		offered = append(offered, strconv.Quote(accepts[i].Network))
	}
	return nil, fmt.Errorf(
		"client is configured for %s but the 402 only offers %s",
		chain, strings.Join(offered, ", "),
	)
}
