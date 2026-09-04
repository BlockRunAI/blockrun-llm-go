package blockrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupAgentWallet creates or loads a wallet and returns a configured LLMClient.
// If the wallet is new, prints funding instructions.
func SetupAgentWallet(opts ...ClientOption) (*LLMClient, error) {
	wallet, err := GetOrCreateWallet()
	if err != nil {
		return nil, fmt.Errorf("failed to setup wallet: %w", err)
	}

	client, err := NewLLMClient(wallet.PrivateKey, opts...)
	if err != nil {
		return nil, err
	}

	if wallet.IsNew {
		fmt.Print(FormatWalletCreatedMessage(wallet.Address))
	}

	return client, nil
}

// Status returns wallet address and USDC balance.
func (c *LLMClient) Status(ctx context.Context) (address string, balance float64, err error) {
	address = c.GetWalletAddress()
	balance, err = c.GetBalance(ctx)
	return
}

// SetupAgentClient uses account credentials when configured; otherwise preserves
// a saved chain or a Base-only wallet, preferring Solana for new wallets.
// Pass "base" or "solana" to choose a wallet chain; "" selects automatically.
func SetupAgentClient(chain string, opts ...ClientOption) (*LLMClient, error) {
	if os.Getenv("BLOCKRUN_API_KEY") != "" {
		return NewLLMClientWithAPIKey("", opts...)
	}
	if chain != "" && chain != "base" && chain != "solana" {
		return nil, fmt.Errorf("chain must be base or solana")
	}
	if chain == "" {
		for _, name := range []string{"payment-chain", ".chain"} {
			data, err := os.ReadFile(filepath.Join(WalletDir, name))
			if err == nil {
				saved := strings.TrimSpace(string(data))
				if saved == "base" || saved == "solana" {
					chain = saved
					break
				}
			}
		}
	}
	if chain == "" {
		sol, err := LoadSolanaWallet()
		if err != nil {
			return nil, err
		}
		base, err := LoadWallet()
		if err != nil {
			return nil, err
		}
		if base == "" {
			base = os.Getenv("BLOCKRUN_WALLET_KEY")
		}
		if base == "" {
			base = os.Getenv("BASE_CHAIN_WALLET_KEY")
		}
		chain = preferredWalletChain(sol != "", base != "")
	}
	if chain == "base" {
		return SetupAgentWallet(opts...)
	}
	wallet, err := GetOrCreateSolanaWallet()
	if err != nil {
		return nil, err
	}
	return NewLLMClientSolana(wallet.PrivateKey, "", opts...)
}

func preferredWalletChain(hasSolana, hasBase bool) string {
	if !hasSolana && hasBase {
		return "base"
	}
	return "solana"
}
