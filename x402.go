package blockrun

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
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
	validAfter := now - 600  // 10 minutes before (allows for clock skew)
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
		Extensions: extensions,
	}

	// Encode as JSON then base64
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// ParsePaymentRequired parses the payment-required header from a 402 response.
func ParsePaymentRequired(headerValue string) (*PaymentRequirement, error) {
	decoded, err := base64.StdEncoding.DecodeString(headerValue)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payment required header: %w", err)
	}

	var req PaymentRequirement
	if err := json.Unmarshal(decoded, &req); err != nil {
		return nil, fmt.Errorf("failed to parse payment required: %w", err)
	}

	return &req, nil
}

// ExtractPaymentDetails extracts payment details from a PaymentRequirement.
// Returns the first payment option if multiple are available.
func ExtractPaymentDetails(req *PaymentRequirement) (*PaymentOption, error) {
	if len(req.Accepts) == 0 {
		return nil, fmt.Errorf("no payment options in payment required response")
	}

	option := req.Accepts[0]

	// Support both v1 (maxAmountRequired) and v2 (amount) formats
	if option.Amount == "" {
		if extra, ok := option.Extra["maxAmountRequired"].(string); ok {
			option.Amount = extra
		}
	}

	if option.Amount == "" {
		return nil, fmt.Errorf("no amount found in payment requirements")
	}

	return &option, nil
}
