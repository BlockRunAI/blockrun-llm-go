package blockrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWallet(t *testing.T) {
	address, privateKey, err := CreateWallet()
	if err != nil {
		t.Fatalf("Failed to create wallet: %v", err)
	}

	// Check address format
	if !strings.HasPrefix(address, "0x") {
		t.Errorf("Expected address to start with 0x, got %s", address)
	}
	if len(address) != 42 {
		t.Errorf("Expected address length 42, got %d", len(address))
	}

	// Check private key format
	if !strings.HasPrefix(privateKey, "0x") {
		t.Errorf("Expected private key to start with 0x, got %s", privateKey)
	}
	if len(privateKey) != 66 {
		t.Errorf("Expected private key length 66, got %d", len(privateKey))
	}
}

func TestGetAddressFromKey(t *testing.T) {
	address, err := GetAddressFromKey(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to get address from key: %v", err)
	}

	if address != testWalletAddress {
		t.Errorf("Expected address %s, got %s", testWalletAddress, address)
	}
}

func TestGetAddressFromKeyWithoutPrefix(t *testing.T) {
	key := strings.TrimPrefix(testPrivateKey, "0x")
	address, err := GetAddressFromKey(key)
	if err != nil {
		t.Fatalf("Failed to get address from key: %v", err)
	}

	if address != testWalletAddress {
		t.Errorf("Expected address %s, got %s", testWalletAddress, address)
	}
}

func TestGetAddressFromKeyInvalid(t *testing.T) {
	_, err := GetAddressFromKey("invalid-key")
	if err == nil {
		t.Error("Expected error for invalid key, got nil")
	}
}

func TestSaveAndLoadWallet(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	WalletDir = tempDir
	WalletFile = filepath.Join(tempDir, ".session")

	// Save wallet
	_, err := SaveWallet(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to save wallet: %v", err)
	}

	// Load wallet
	key, err := LoadWallet()
	if err != nil {
		t.Fatalf("Failed to load wallet: %v", err)
	}

	if key != testPrivateKey {
		t.Errorf("Expected key %s, got %s", testPrivateKey, key)
	}
}

func TestLoadWalletNotFound(t *testing.T) {
	// Create temp directory with no wallet file
	tempDir := t.TempDir()
	WalletDir = tempDir
	WalletFile = filepath.Join(tempDir, ".session")

	key, err := LoadWallet()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if key != "" {
		t.Errorf("Expected empty key, got %s", key)
	}
}

func TestGetOrCreateWalletFromEnv(t *testing.T) {
	t.Setenv("BLOCKRUN_WALLET_KEY", testPrivateKey)
	defer t.Setenv("BLOCKRUN_WALLET_KEY", "")

	info, err := GetOrCreateWallet()
	if err != nil {
		t.Fatalf("Failed to get or create wallet: %v", err)
	}

	if info.Address != testWalletAddress {
		t.Errorf("Expected address %s, got %s", testWalletAddress, info.Address)
	}

	if info.IsNew {
		t.Error("Expected IsNew to be false for env var wallet")
	}
}

func TestGetOrCreateWalletCreatesNew(t *testing.T) {
	// Clear env vars
	t.Setenv("BLOCKRUN_WALLET_KEY", "")
	t.Setenv("BASE_CHAIN_WALLET_KEY", "")

	// Use temp directory
	tempDir := t.TempDir()
	WalletDir = tempDir
	WalletFile = filepath.Join(tempDir, ".session")

	info, err := GetOrCreateWallet()
	if err != nil {
		t.Fatalf("Failed to get or create wallet: %v", err)
	}

	if !info.IsNew {
		t.Error("Expected IsNew to be true for newly created wallet")
	}

	// Verify file was created
	if _, err := os.Stat(WalletFile); os.IsNotExist(err) {
		t.Error("Expected wallet file to be created")
	}
}

func TestGetEIP681URI(t *testing.T) {
	uri := GetEIP681URI(testWalletAddress, 1.0)

	// Should contain the USDC contract address
	if !strings.Contains(uri, USDCBaseContract) {
		t.Errorf("Expected URI to contain USDC contract %s, got %s", USDCBaseContract, uri)
	}

	// Should contain the recipient address
	if !strings.Contains(uri, testWalletAddress) {
		t.Errorf("Expected URI to contain address %s, got %s", testWalletAddress, uri)
	}

	// Should contain the chain ID
	if !strings.Contains(uri, BaseChainIDStr) {
		t.Errorf("Expected URI to contain chain ID %s, got %s", BaseChainIDStr, uri)
	}
}

func TestGetPaymentLinks(t *testing.T) {
	links := GetPaymentLinks(testWalletAddress)

	// Check Basescan link
	if !strings.Contains(links.Basescan, "basescan.org") {
		t.Errorf("Expected Basescan link, got %s", links.Basescan)
	}

	// Check Blockrun link
	if !strings.Contains(links.Blockrun, "blockrun.ai") {
		t.Errorf("Expected Blockrun link, got %s", links.Blockrun)
	}

	// All links should contain the address
	if !strings.Contains(links.Basescan, testWalletAddress) {
		t.Errorf("Expected Basescan link to contain address")
	}
}

func TestFormatWalletCreatedMessage(t *testing.T) {
	msg := FormatWalletCreatedMessage(testWalletAddress)

	// Should contain the address
	if !strings.Contains(msg, testWalletAddress) {
		t.Errorf("Expected message to contain address %s", testWalletAddress)
	}

	// Should mention USDC
	if !strings.Contains(msg, "USDC") {
		t.Error("Expected message to mention USDC")
	}
}

func TestFormatNeedsFundingMessage(t *testing.T) {
	msg := FormatNeedsFundingMessage(testWalletAddress)

	// Should contain the address
	if !strings.Contains(msg, testWalletAddress) {
		t.Errorf("Expected message to contain address %s", testWalletAddress)
	}
}

func TestFormatFundingMessageCompact(t *testing.T) {
	msg := FormatFundingMessageCompact(testWalletAddress)

	// Should contain the address
	if !strings.Contains(msg, testWalletAddress) {
		t.Errorf("Expected message to contain address %s", testWalletAddress)
	}

	// Compact message should be shorter than the full message
	fullMsg := FormatNeedsFundingMessage(testWalletAddress)
	if len(msg) >= len(fullMsg) {
		t.Error("Expected compact message to be shorter than full message")
	}
}
