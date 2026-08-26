package blockrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-tron/base58"
)

// isolatedHome points HOME at a fresh temp dir so wallet files land there and
// LoadSolanaWallet's ~/.*/solana-wallet.json scan finds nothing pre-existing.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SOLANA_WALLET_KEY", "")
	return home
}

// TestCreateSolanaWalletRoundTrip: the generated key must actually derive the
// address handed back with it.
func TestCreateSolanaWalletRoundTrip(t *testing.T) {
	address, privateKey, err := CreateSolanaWallet()
	if err != nil {
		t.Fatalf("CreateSolanaWallet: %v", err)
	}

	derived, err := GetSolanaPublicKey(privateKey)
	if err != nil {
		t.Fatalf("generated key does not parse: %v", err)
	}
	if derived != address {
		t.Errorf("address = %q but the key derives %q", address, derived)
	}
	decoded, err := base58.Decode(privateKey)
	if err != nil {
		t.Fatalf("private key is not bs58: %v", err)
	}
	if len(decoded) != 64 {
		t.Errorf("key is %d bytes, want a 64-byte ed25519 keypair", len(decoded))
	}
}

// TestCreateSolanaWalletIsRandom guards against a fixed or zero seed.
func TestCreateSolanaWalletIsRandom(t *testing.T) {
	_, a, err := CreateSolanaWallet()
	if err != nil {
		t.Fatalf("CreateSolanaWallet: %v", err)
	}
	_, b, err := CreateSolanaWallet()
	if err != nil {
		t.Fatalf("CreateSolanaWallet: %v", err)
	}
	if a == b {
		t.Fatal("two wallets share a private key — the generator is not random")
	}
}

// TestSaveSolanaWalletIsLoadable: what SaveSolanaWallet writes must be what
// LoadSolanaWallet reads back, or the pair is useless.
func TestSaveSolanaWalletIsLoadable(t *testing.T) {
	isolatedHome(t)
	_, key, err := CreateSolanaWallet()
	if err != nil {
		t.Fatalf("CreateSolanaWallet: %v", err)
	}

	path, err := SaveSolanaWallet(key)
	if err != nil {
		t.Fatalf("SaveSolanaWallet: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(".blockrun", ".solana-session")) {
		t.Errorf("saved to %q, want ~/.blockrun/.solana-session", path)
	}

	loaded, err := LoadSolanaWallet()
	if err != nil {
		t.Fatalf("LoadSolanaWallet: %v", err)
	}
	if loaded != key {
		t.Errorf("loaded %q, want the saved key", loaded)
	}
}

// TestSaveSolanaWalletPermissions: a private key on disk must not be
// world-readable.
func TestSaveSolanaWalletPermissions(t *testing.T) {
	isolatedHome(t)
	_, key, _ := CreateSolanaWallet()
	path, err := SaveSolanaWallet(key)
	if err != nil {
		t.Fatalf("SaveSolanaWallet: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 0600", perm)
	}
}

// TestGetOrCreateSolanaWalletPrefersEnv: an explicitly configured key must win
// over anything on disk, and must not be reported as newly created.
func TestGetOrCreateSolanaWalletPrefersEnv(t *testing.T) {
	isolatedHome(t)
	_, envKey, _ := CreateSolanaWallet()
	t.Setenv("SOLANA_WALLET_KEY", envKey)

	info, err := GetOrCreateSolanaWallet()
	if err != nil {
		t.Fatalf("GetOrCreateSolanaWallet: %v", err)
	}
	if info.PrivateKey != envKey {
		t.Errorf("used %q, want the SOLANA_WALLET_KEY value", info.PrivateKey)
	}
	if info.IsNew {
		t.Error("IsNew = true for a key that already existed in the environment")
	}
	want, _ := GetSolanaPublicKey(envKey)
	if info.Address != want {
		t.Errorf("address = %q, want %q", info.Address, want)
	}
}

// TestGetOrCreateSolanaWalletCreatesAndPersists: with nothing configured it
// must mint a wallet, flag it new, and leave it loadable next time.
func TestGetOrCreateSolanaWalletCreatesAndPersists(t *testing.T) {
	isolatedHome(t)

	created, err := GetOrCreateSolanaWallet()
	if err != nil {
		t.Fatalf("GetOrCreateSolanaWallet: %v", err)
	}
	if !created.IsNew {
		t.Error("IsNew = false for a freshly minted wallet")
	}
	if created.PrivateKey == "" || created.Address == "" {
		t.Fatalf("incomplete wallet: %+v", created)
	}

	again, err := GetOrCreateSolanaWallet()
	if err != nil {
		t.Fatalf("second GetOrCreateSolanaWallet: %v", err)
	}
	if again.PrivateKey != created.PrivateKey {
		t.Error("second call minted a different wallet — the first was not persisted")
	}
	if again.IsNew {
		t.Error("IsNew = true on the second call for an already-persisted wallet")
	}
}

// TestGetSolanaWalletAddressFromEnvOrFile must not expose the private key.
func TestGetSolanaWalletAddressFromEnvOrFile(t *testing.T) {
	isolatedHome(t)
	if addr, err := GetSolanaWalletAddressFromEnvOrFile(); err != nil || addr != "" {
		t.Errorf("with no wallet: got (%q, %v), want empty and no error", addr, err)
	}

	_, key, _ := CreateSolanaWallet()
	if _, err := SaveSolanaWallet(key); err != nil {
		t.Fatalf("SaveSolanaWallet: %v", err)
	}
	want, _ := GetSolanaPublicKey(key)

	got, err := GetSolanaWalletAddressFromEnvOrFile()
	if err != nil {
		t.Fatalf("GetSolanaWalletAddressFromEnvOrFile: %v", err)
	}
	if got != want {
		t.Errorf("address = %q, want %q", got, want)
	}
}

// TestGetSolanaPayURIUsesDecimalAmount is the one that must not be a mirror of
// GetEIP681URI. EIP-681 encodes uint256 base units (1.5 USDC -> 1500000);
// Solana Pay encodes a decimal token amount (1.5 USDC -> 1.5). Copying the
// EVM helper would overstate every funding request by a factor of a million.
func TestGetSolanaPayURIUsesDecimalAmount(t *testing.T) {
	addr := "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin"
	uri := GetSolanaPayURI(addr, 1.5)

	if !strings.HasPrefix(uri, "solana:"+addr) {
		t.Errorf("uri = %q, want it to open with solana:<recipient>", uri)
	}
	if !strings.Contains(uri, "amount=1.5") {
		t.Errorf("uri = %q, want a decimal amount=1.5, not base units", uri)
	}
	if strings.Contains(uri, "1500000") {
		t.Errorf("uri = %q encodes base units — that is the EIP-681 convention, not Solana Pay", uri)
	}
	if !strings.Contains(uri, "spl-token="+USDCSolanaMainnet) {
		t.Errorf("uri = %q, want the USDC SPL mint so wallets request USDC and not SOL", uri)
	}
}

// TestGetSolanaPayURIWholeAmount: a whole number must not render as "1.000000".
func TestGetSolanaPayURIWholeAmount(t *testing.T) {
	uri := GetSolanaPayURI("9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin", 5)
	if !strings.Contains(uri, "amount=5&") && !strings.HasSuffix(uri, "amount=5") {
		t.Errorf("uri = %q, want a clean amount=5", uri)
	}
}

// TestGetSolanaPaymentLinksPointAtSolana: the EVM struct carries a Basescan
// field. Solana links must point at a Solana explorer instead.
func TestGetSolanaPaymentLinksPointAtSolana(t *testing.T) {
	addr := "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin"
	links := GetSolanaPaymentLinks(addr)

	if !strings.Contains(links.Solscan, addr) || !strings.Contains(links.Solscan, "solscan.io") {
		t.Errorf("Solscan = %q, want a solscan.io link for the address", links.Solscan)
	}
	if !strings.HasPrefix(links.SolanaPay, "solana:") {
		t.Errorf("SolanaPay = %q, want a solana: URI", links.SolanaPay)
	}
	if strings.Contains(links.Solscan+links.SolanaPay+links.Blockrun, "basescan.org") {
		t.Error("Solana payment links must never point at Basescan")
	}
}

// TestSolanaFundingMessagesNameSolana: the EVM copy tells users to send USDC on
// Base. Handing that to a Solana user sends funds to the wrong chain.
func TestSolanaFundingMessagesNameSolana(t *testing.T) {
	addr := "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin"
	for name, msg := range map[string]string{
		"created": FormatSolanaWalletCreatedMessage(addr),
		"needs":   FormatSolanaNeedsFundingMessage(addr),
		"compact": FormatSolanaFundingMessageCompact(addr),
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(msg, addr) {
				t.Error("message omits the wallet address")
			}
			if !strings.Contains(msg, "Solana") {
				t.Error("message never names Solana")
			}
			if strings.Contains(msg, "on Base") || strings.Contains(msg, "Basescan") {
				t.Errorf("message tells a Solana user to fund Base:\n%s", msg)
			}
		})
	}
}

// TestScanSolanaWalletsExported: the scan already existed unexported. Exporting
// it is what lets a caller discover a wallet another provider wrote.
func TestScanSolanaWalletsExported(t *testing.T) {
	home := isolatedHome(t)
	_, key, _ := CreateSolanaWallet()
	addr, _ := GetSolanaPublicKey(key)

	dir := filepath.Join(home, ".agentcash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"privateKey":"` + key + `","address":"` + addr + `"}`
	if err := os.WriteFile(filepath.Join(dir, "solana-wallet.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	found := ScanSolanaWallets()
	if len(found) != 1 {
		t.Fatalf("found %d wallets, want 1", len(found))
	}
	if found[0].Address != addr || found[0].PrivateKey != key {
		t.Errorf("found %+v, want the wallet just written", found[0])
	}
	if found[0].IsNew {
		t.Error("a discovered wallet is not new")
	}
}
