package blockrun

import (
	"context"
	"fmt"
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
