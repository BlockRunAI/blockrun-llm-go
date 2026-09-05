package blockrun

import (
	"context"
	"fmt"
)

// SetupAgentWallet creates or loads a wallet and returns a configured LLMClient.
// If the wallet is new, prints funding instructions.
//
// With BLOCKRUN_API_KEY set it mints nothing: the account rail already has a
// funded identity, and creating a wallet on disk for a client that will never
// sign with it is a keyfile to lose for no benefit. This is what lets an agent
// or a skill call SetupAgentWallet unconditionally and work on either rail.
func SetupAgentWallet(opts ...ClientOption) (*LLMClient, error) {
	apiKey, keyErr := resolveAPIKey("")
	if keyErr != nil {
		return nil, keyErr
	}
	if key := apiKey; key != "" {
		return NewLLMClient(key, opts...)
	}

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
//
// On the account rail there is neither: address comes back empty and err
// explains that credit lives on the dashboard, rather than reporting a $0
// balance that reads as an empty wallet. Check PaymentMode() first when the
// same code has to serve both rails.
func (c *LLMClient) Status(ctx context.Context) (address string, balance float64, err error) {
	address = c.GetWalletAddress()
	balance, err = c.GetBalance(ctx)
	return
}
