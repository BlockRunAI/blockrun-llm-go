package blockrun

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// USDCBaseContract is the USDC contract address on Base chain.
	USDCBaseContract = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"

	// BaseChainIDStr is the Base chain ID as string.
	BaseChainIDStr = "8453"
)

var (
	// WalletDir is the directory where wallet files are stored.
	WalletDir = filepath.Join(os.Getenv("HOME"), ".blockrun")

	// WalletFile is the path to the wallet key file.
	WalletFile = filepath.Join(WalletDir, ".session")
)

// WalletInfo contains information about a wallet.
type WalletInfo struct {
	PrivateKey string
	Address    string
	IsNew      bool
}

// PaymentLinksInfo contains various payment links for a wallet.
type PaymentLinksInfo struct {
	Basescan   string
	WalletLink string
	Ethereum   string
	Blockrun   string
}

// CreateWallet creates a new Ethereum wallet.
func CreateWallet() (address string, privateKey string, err error) {
	key, err := crypto.GenerateKey()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate key: %w", err)
	}

	privateKeyBytes := crypto.FromECDSA(key)
	privateKey = "0x" + fmt.Sprintf("%x", privateKeyBytes)
	address = crypto.PubkeyToAddress(key.PublicKey).Hex()

	return address, privateKey, nil
}

// SaveWallet saves the wallet private key to ~/.blockrun/.session
func SaveWallet(privateKey string) (string, error) {
	// Ensure directory exists
	if err := os.MkdirAll(WalletDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create wallet directory: %w", err)
	}

	// Write key with restricted permissions
	if err := os.WriteFile(WalletFile, []byte(privateKey), 0600); err != nil {
		return "", fmt.Errorf("failed to write wallet file: %w", err)
	}

	return WalletFile, nil
}

// LoadWallet loads the wallet private key from file.
func LoadWallet() (string, error) {
	// Check .session first (preferred)
	if data, err := os.ReadFile(WalletFile); err == nil {
		key := strings.TrimSpace(string(data))
		if key != "" {
			return key, nil
		}
	}

	// Check legacy wallet.key
	legacyFile := filepath.Join(WalletDir, "wallet.key")
	if data, err := os.ReadFile(legacyFile); err == nil {
		key := strings.TrimSpace(string(data))
		if key != "" {
			return key, nil
		}
	}

	return "", nil
}

// GetOrCreateWallet gets an existing wallet or creates a new one.
//
// Priority:
// 1. BLOCKRUN_WALLET_KEY environment variable
// 2. BASE_CHAIN_WALLET_KEY environment variable
// 3. ~/.blockrun/.session file
// 4. ~/.blockrun/wallet.key file (legacy)
// 5. Create new wallet
func GetOrCreateWallet() (*WalletInfo, error) {
	// Check environment variables first
	envKey := os.Getenv("BLOCKRUN_WALLET_KEY")
	if envKey == "" {
		envKey = os.Getenv("BASE_CHAIN_WALLET_KEY")
	}

	if envKey != "" {
		address, err := GetAddressFromKey(envKey)
		if err != nil {
			return nil, err
		}
		return &WalletInfo{
			PrivateKey: envKey,
			Address:    address,
			IsNew:      false,
		}, nil
	}

	// Check file
	fileKey, _ := LoadWallet()
	if fileKey != "" {
		address, err := GetAddressFromKey(fileKey)
		if err != nil {
			return nil, err
		}
		return &WalletInfo{
			PrivateKey: fileKey,
			Address:    address,
			IsNew:      false,
		}, nil
	}

	// Create new wallet
	address, privateKey, err := CreateWallet()
	if err != nil {
		return nil, err
	}

	if _, err := SaveWallet(privateKey); err != nil {
		return nil, err
	}

	return &WalletInfo{
		PrivateKey: privateKey,
		Address:    address,
		IsNew:      true,
	}, nil
}

// GetWalletAddressFromEnvOrFile gets the wallet address without exposing the private key.
func GetWalletAddressFromEnvOrFile() (string, error) {
	envKey := os.Getenv("BLOCKRUN_WALLET_KEY")
	if envKey == "" {
		envKey = os.Getenv("BASE_CHAIN_WALLET_KEY")
	}

	if envKey != "" {
		return GetAddressFromKey(envKey)
	}

	fileKey, _ := LoadWallet()
	if fileKey != "" {
		return GetAddressFromKey(fileKey)
	}

	return "", nil
}

// GetAddressFromKey derives the Ethereum address from a private key.
func GetAddressFromKey(privateKey string) (string, error) {
	key := strings.TrimPrefix(privateKey, "0x")
	ecdsaKey, err := crypto.HexToECDSA(key)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}
	return crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex(), nil
}

// GetPrivateKeyFromHex parses a hex private key string into an ECDSA private key.
func GetPrivateKeyFromHex(privateKey string) (*ecdsa.PrivateKey, error) {
	key := strings.TrimPrefix(privateKey, "0x")
	return crypto.HexToECDSA(key)
}

// GetEIP681URI generates an EIP-681 URI for USDC transfer on Base.
func GetEIP681URI(address string, amountUSDC float64) string {
	// USDC has 6 decimals
	amountWei := int64(amountUSDC * 1_000_000)
	return fmt.Sprintf("ethereum:%s@%s/transfer?address=%s&uint256=%d",
		USDCBaseContract, BaseChainIDStr, address, amountWei)
}

// GetPaymentLinks generates payment links for the wallet address.
func GetPaymentLinks(address string) *PaymentLinksInfo {
	return &PaymentLinksInfo{
		Basescan:   fmt.Sprintf("https://basescan.org/address/%s", address),
		WalletLink: fmt.Sprintf("ethereum:%s@%s/transfer?address=%s", USDCBaseContract, BaseChainIDStr, address),
		Ethereum:   fmt.Sprintf("ethereum:%s@%s", address, BaseChainIDStr),
		Blockrun:   fmt.Sprintf("https://blockrun.ai/fund?address=%s", address),
	}
}

// FormatWalletCreatedMessage formats the message shown when a new wallet is created.
func FormatWalletCreatedMessage(address string) string {
	links := GetPaymentLinks(address)

	return fmt.Sprintf(`
I'm your BlockRun Agent! I can access GPT-4, Grok, image generation, and more.

Please send $1-5 USDC on Base to start:

%s

What is Base? Base is Coinbase's blockchain network.
You can buy USDC on Coinbase and send it directly to me.

What $1 USDC gets you:
- ~1,000 GPT-4o calls
- ~100 image generations
- ~10,000 DeepSeek calls

Quick links:
- Check my balance: %s
- Get USDC: https://www.coinbase.com or https://bridge.base.org

Questions? care@blockrun.ai | Issues? github.com/BlockRunAI/blockrun-llm-go/issues

Key stored securely in ~/.blockrun/
Your private key never leaves your machine - only signatures are sent.
`, address, links.Basescan)
}

// FormatNeedsFundingMessage formats the message shown when wallet needs more funds.
func FormatNeedsFundingMessage(address string) string {
	links := GetPaymentLinks(address)

	return fmt.Sprintf(`
I've run out of funds! Please send more USDC on Base to continue helping you.

Send to my address:
%s

Check my balance: %s

What $1 USDC gets you: ~1,000 GPT-4o calls or ~100 images.
Questions? care@blockrun.ai | Issues? github.com/BlockRunAI/blockrun-llm-go/issues

Your private key never leaves your machine - only signatures are sent.
`, address, links.Basescan)
}

// FormatFundingMessageCompact returns a compact funding message.
func FormatFundingMessageCompact(address string) string {
	links := GetPaymentLinks(address)
	return fmt.Sprintf("I need a little top-up to keep helping you! Send USDC on Base to: %s\nCheck my balance: %s",
		address, links.Basescan)
}
