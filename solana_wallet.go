package blockrun

// Solana (SVM) wallet loading for x402 payments.
//
// VIP customers already hold a funded Solana wallet — this file only LOADS the
// existing bs58 key for local SVM signing; the key never leaves the machine.
// Signing itself lives in solana_x402.go.
//
// Resolution order mirrors the Python blockrun-llm-vip package:
//   explicit arg → SOLANA_WALLET_KEY env → scan ~/.*/solana-wallet.json
//   (most recent, any provider) → ~/.blockrun/.solana-session.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mr-tron/base58"
)

// Solana gateway + payment constants.
const (
	// DefaultSolanaAPIURL is the BlockRun Solana gateway base URL. Pay USDC on
	// Solana here instead of Base. Override with BLOCKRUN_SOLANA_API_URL.
	DefaultSolanaAPIURL = "https://sol.blockrun.ai/api"

	// DefaultSolanaRPCURL is BlockRun's free Solana JSON-RPC proxy, used to fetch
	// the recent blockhash and mint info while signing. Override with SOLANA_RPC_URL.
	DefaultSolanaRPCURL = "https://sol.blockrun.ai/api/v1/solana/rpc"

	// USDCSolanaMainnet is the USDC SPL mint on Solana mainnet.
	USDCSolanaMainnet = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// Solana program addresses used to build the x402 SVM exact-scheme transaction.
	tokenProgramAddress          = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	token2022ProgramAddress      = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
	computeBudgetProgramAddress  = "ComputeBudget111111111111111111111111111111"
	memoProgramAddress           = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"
	associatedTokenProgramAddr   = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	defaultComputeUnitLimit      = uint32(20000)
	defaultComputeUnitPriceMicro = uint64(1)
)

// solanaPollResignInterval is how often an async poll loop re-signs the Solana
// payment with a fresh blockhash. It must stay comfortably under a Solana
// blockhash's ~60-90s lifetime so the settling poll's transaction is valid.
const solanaPollResignInterval = 30 * time.Second

// SolanaSessionFile is the legacy fallback path for a bare bs58 Solana key.
func solanaSessionFile() string {
	return filepath.Join(os.Getenv("HOME"), ".blockrun", ".solana-session")
}

// LoadSolanaWallet loads the existing Solana wallet's bs58 private key, or "".
//
// Order: SOLANA_WALLET_KEY env → scan ~/.*/solana-wallet.json (most recent) →
// ~/.blockrun/.solana-session. Returns ("", nil) when none is configured.
func LoadSolanaWallet() (string, error) {
	if env := strings.TrimSpace(os.Getenv("SOLANA_WALLET_KEY")); env != "" {
		return env, nil
	}

	if wallets := scanSolanaWallets(); len(wallets) > 0 {
		return wallets[0].privateKey, nil
	}

	if data, err := os.ReadFile(solanaSessionFile()); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, nil
		}
	}
	return "", nil
}

type solanaWalletEntry struct {
	mtime      int64
	privateKey string
	address    string
}

// scanSolanaWallets scans ~/.*/solana-wallet.json files (agentcash and other
// providers). Each file holds JSON with "privateKey" and "address". Most-recent
// first; 32-byte seeds are expanded to 64-byte keypairs.
func scanSolanaWallets() []solanaWalletEntry {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}

	var results []solanaWalletEntry
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		walletFile := filepath.Join(home, entry.Name(), "solana-wallet.json")
		info, err := os.Stat(walletFile)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(walletFile)
		if err != nil {
			continue
		}
		var parsed struct {
			PrivateKey string `json:"privateKey"`
			Address    string `json:"address"`
		}
		if json.Unmarshal(data, &parsed) != nil {
			continue
		}
		if parsed.PrivateKey == "" || parsed.Address == "" {
			continue
		}
		pk := parsed.PrivateKey
		if expanded, err := expandSolanaSeed(pk); err == nil {
			pk = expanded
		}
		results = append(results, solanaWalletEntry{
			mtime:      info.ModTime().UnixNano(),
			privateKey: pk,
			address:    parsed.Address,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].mtime > results[j].mtime
	})
	return results
}

// expandSolanaSeed expands a 32-byte bs58 seed into a 64-byte bs58 keypair. If
// the input already decodes to 64 bytes it is returned unchanged.
func expandSolanaSeed(bs58Key string) (string, error) {
	decoded, err := base58.Decode(bs58Key)
	if err != nil {
		return "", err
	}
	switch len(decoded) {
	case 64:
		return bs58Key, nil
	case 32:
		full := ed25519.NewKeyFromSeed(decoded)
		return base58.Encode(full), nil
	default:
		return "", fmt.Errorf("invalid Solana private key: expected 32 or 64 bytes, got %d", len(decoded))
	}
}

// solanaKeypair decodes a bs58 Solana secret key into a 64-byte ed25519 private
// key, accepting either a 64-byte keypair or a 32-byte seed.
func solanaKeypair(bs58Key string) (ed25519.PrivateKey, error) {
	decoded, err := base58.Decode(strings.TrimSpace(bs58Key))
	if err != nil {
		return nil, fmt.Errorf("invalid base58 Solana key: %w", err)
	}
	switch len(decoded) {
	case ed25519.PrivateKeySize: // 64
		return ed25519.PrivateKey(decoded), nil
	case ed25519.SeedSize: // 32
		return ed25519.NewKeyFromSeed(decoded), nil
	default:
		return nil, fmt.Errorf("invalid Solana private key: expected 32 or 64 bytes, got %d", len(decoded))
	}
}

// GetSolanaPublicKey returns the bs58 public key (address) for a bs58 Solana
// secret key. Accepts both 64-byte keypairs and 32-byte seeds.
func GetSolanaPublicKey(bs58Key string) (string, error) {
	priv, err := solanaKeypair(bs58Key)
	if err != nil {
		return "", err
	}
	pub := priv.Public().(ed25519.PublicKey)
	return base58.Encode(pub), nil
}
