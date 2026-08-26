package blockrun

// Solana wallet management — the counterpart to wallet.go's EVM helpers.
//
// solana_wallet.go only ever LOADED an existing bs58 key, on the assumption
// that a Solana user arrives with a funded wallet already in hand. Everything
// the EVM side offers around that — mint a wallet, persist it, discover one
// another provider wrote, hand the user a funding link — had no Solana
// equivalent, so a Solana user had to do all of it outside the SDK.
//
// SECURITY: keys generated here are written to ~/.blockrun/.solana-session with
// 0600 and never leave the machine, exactly as on the EVM side.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mr-tron/base58"
)

// SolanaPaymentLinksInfo contains funding and explorer links for a Solana
// wallet. Deliberately not PaymentLinksInfo: that struct's fields are Basescan
// and Ethereum, which have no meaning here.
type SolanaPaymentLinksInfo struct {
	Solscan   string
	SolanaPay string
	Blockrun  string
}

// CreateSolanaWallet creates a new Solana wallet, returning the bs58 address
// and the bs58-encoded 64-byte ed25519 keypair.
func CreateSolanaWallet() (address string, privateKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate Solana key: %w", err)
	}
	return base58.Encode(pub), base58.Encode(priv), nil
}

// SaveSolanaWallet saves the bs58 private key to ~/.blockrun/.solana-session,
// the last entry in LoadSolanaWallet's resolution order, and returns its path.
func SaveSolanaWallet(privateKey string) (string, error) {
	path := solanaSessionFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("failed to create wallet directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(privateKey), 0o600); err != nil {
		return "", fmt.Errorf("failed to write Solana wallet file: %w", err)
	}
	return path, nil
}

// GetOrCreateSolanaWallet returns the configured Solana wallet, minting and
// persisting one when none is found.
//
// Resolution matches LoadSolanaWallet: SOLANA_WALLET_KEY env → scan
// ~/.*/solana-wallet.json (most recent) → ~/.blockrun/.solana-session. Only a
// wallet this call created is reported with IsNew.
func GetOrCreateSolanaWallet() (*WalletInfo, error) {
	existing, err := LoadSolanaWallet()
	if err != nil {
		return nil, err
	}
	if existing != "" {
		address, err := GetSolanaPublicKey(existing)
		if err != nil {
			return nil, fmt.Errorf("configured Solana wallet is unusable: %w", err)
		}
		return &WalletInfo{PrivateKey: existing, Address: address, IsNew: false}, nil
	}

	address, privateKey, err := CreateSolanaWallet()
	if err != nil {
		return nil, err
	}
	if _, err := SaveSolanaWallet(privateKey); err != nil {
		return nil, err
	}
	return &WalletInfo{PrivateKey: privateKey, Address: address, IsNew: true}, nil
}

// GetSolanaWalletAddressFromEnvOrFile returns the configured Solana address
// without exposing the private key, or "" when no wallet is configured.
func GetSolanaWalletAddressFromEnvOrFile() (string, error) {
	key, err := LoadSolanaWallet()
	if err != nil || key == "" {
		return "", err
	}
	return GetSolanaPublicKey(key)
}

// GetSolanaPayURI builds a Solana Pay transfer request for USDC.
//
// Note the difference from GetEIP681URI, which is NOT a mirror: EIP-681 encodes
// a uint256 in base units, while Solana Pay's amount is a decimal quantity of
// the token itself. Reusing the EVM convention here would ask the user for a
// million times the intended amount. spl-token pins the request to USDC;
// without it a wallet would offer to send SOL.
func GetSolanaPayURI(address string, amountUSDC float64) string {
	q := url.Values{}
	q.Set("amount", strconv.FormatFloat(amountUSDC, 'f', -1, 64))
	q.Set("spl-token", USDCSolanaMainnet)
	return "solana:" + address + "?" + q.Encode()
}

// GetSolanaPaymentLinks generates funding and explorer links for a Solana
// wallet address.
func GetSolanaPaymentLinks(address string) *SolanaPaymentLinksInfo {
	return &SolanaPaymentLinksInfo{
		Solscan:   fmt.Sprintf("https://solscan.io/account/%s", address),
		SolanaPay: "solana:" + address + "?spl-token=" + USDCSolanaMainnet,
		Blockrun:  fmt.Sprintf("https://blockrun.ai/fund?address=%s&chain=solana", address),
	}
}

// FormatSolanaWalletCreatedMessage formats the message shown when a new Solana
// wallet is created.
func FormatSolanaWalletCreatedMessage(address string) string {
	links := GetSolanaPaymentLinks(address)

	return fmt.Sprintf(`
I'm your BlockRun Agent! I can access GPT-4, Grok, image generation, and more.

Please send $1-5 USDC on Solana to start:

%s

Send USDC on the Solana network — SPL USDC, mint %s.
USDC on another chain will not arrive here.

What $1 USDC gets you:
- ~1,000 GPT-4o calls
- ~100 image generations
- ~10,000 DeepSeek calls

Quick links:
- Check my balance: %s
- Pay with a Solana wallet: %s

Questions? care@blockrun.ai | Issues? github.com/BlockRunAI/blockrun-llm-go/issues

Key stored securely in ~/.blockrun/
Your private key never leaves your machine - only signatures are sent.
`, address, USDCSolanaMainnet, links.Solscan, links.SolanaPay)
}

// FormatSolanaNeedsFundingMessage formats the message shown when a Solana
// wallet needs more funds.
func FormatSolanaNeedsFundingMessage(address string) string {
	links := GetSolanaPaymentLinks(address)

	return fmt.Sprintf(`
I've run out of funds! Please send more USDC on Solana to continue helping you.

Send to my address:
%s

Check my balance: %s

What $1 USDC gets you: ~1,000 GPT-4o calls or ~100 images.
Questions? care@blockrun.ai | Issues? github.com/BlockRunAI/blockrun-llm-go/issues

Your private key never leaves your machine - only signatures are sent.
`, address, links.Solscan)
}

// FormatSolanaFundingMessageCompact returns a compact Solana funding message.
func FormatSolanaFundingMessageCompact(address string) string {
	links := GetSolanaPaymentLinks(address)
	return fmt.Sprintf("I need a little top-up to keep helping you! Send USDC on Solana to: %s\nCheck my balance: %s",
		address, links.Solscan)
}

// ScanSolanaWallets discovers Solana wallets written by any provider under
// ~/.*/solana-wallet.json, most recently modified first.
//
// This is the exported form of the scan LoadSolanaWallet already used
// internally: loading picks the newest silently, while a caller listing
// candidates needs to see all of them.
func ScanSolanaWallets() []WalletInfo {
	entries := scanSolanaWallets()
	wallets := make([]WalletInfo, 0, len(entries))
	for _, e := range entries {
		address := e.address
		// Trust the derived key over the file's own address field: a file that
		// disagrees with its key would otherwise hand back an address whose
		// funds the key cannot spend.
		if derived, err := GetSolanaPublicKey(e.privateKey); err == nil {
			address = derived
		}
		wallets = append(wallets, WalletInfo{
			PrivateKey: strings.TrimSpace(e.privateKey),
			Address:    address,
			IsNew:      false,
		})
	}
	return wallets
}
