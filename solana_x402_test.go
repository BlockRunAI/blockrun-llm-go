package blockrun

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

// makeBlockhash returns a deterministic-enough random blockhash for tests.
func makeBlockhash(t *testing.T) solana.Hash {
	t.Helper()
	var h solana.Hash
	if _, err := rand.Read(h[:]); err != nil {
		t.Fatalf("blockhash: %v", err)
	}
	return h
}

// TestBuildSignedSolanaExactTx verifies the SVM exact-scheme transaction matches
// the x402 spec byte-for-byte: 4 instructions, correct discriminators, ATAs, a
// fee-payer placeholder signature, and a valid client signature over 0x80||msg.
func TestBuildSignedSolanaExactTx(t *testing.T) {
	clientPriv, err := solana.NewRandomPrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	payer := clientPriv.PublicKey()
	feePayer := solana.NewWallet().PublicKey()
	payTo := solana.NewWallet().PublicKey()
	mint := solana.MustPublicKeyFromBase58(USDCSolanaMainnet)
	tokenProgram := solana.MustPublicKeyFromBase58(tokenProgramAddress)
	const amount = uint64(1234)
	const decimals = uint8(6)
	blockhash := makeBlockhash(t)

	txBytes, err := buildSignedSolanaExactTx(clientPriv, feePayer, payer, payTo, mint, tokenProgram, amount, decimals, blockhash)
	if err != nil {
		t.Fatalf("buildSignedSolanaExactTx: %v", err)
	}

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		t.Fatalf("decode tx: %v", err)
	}

	if !tx.Message.IsVersioned() {
		t.Error("expected a versioned (v0) transaction")
	}
	if got := tx.Message.GetVersion(); got != solana.MessageVersionV0 {
		t.Errorf("version = %d, want V0", got)
	}

	// Two signatures: slot 0 fee payer placeholder (zero), slot 1 client.
	if len(tx.Signatures) != 2 {
		t.Fatalf("signatures = %d, want 2", len(tx.Signatures))
	}
	if tx.Signatures[0] != (solana.Signature{}) {
		t.Error("slot 0 (fee payer) should be a zero placeholder for the facilitator")
	}
	if tx.Message.Header.NumRequiredSignatures != 2 {
		t.Errorf("NumRequiredSignatures = %d, want 2", tx.Message.Header.NumRequiredSignatures)
	}

	// Fee payer must be account index 0.
	if !tx.Message.AccountKeys[0].Equals(feePayer) {
		t.Errorf("account[0] = %s, want feePayer %s", tx.Message.AccountKeys[0], feePayer)
	}

	// Client signature valid over the exact signed bytes (0x80||message).
	msgBytes, err := tx.Message.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	if len(msgBytes) == 0 || msgBytes[0] != 0x80 {
		t.Fatalf("signed message must start with 0x80 version byte, got %x", msgBytes[0])
	}
	clientPub := ed25519.PublicKey(payer.Bytes())
	if !ed25519.Verify(clientPub, msgBytes, tx.Signatures[1][:]) {
		t.Error("client signature does not verify over 0x80||message")
	}

	// Four instructions in order: computeLimit, computePrice, transferChecked, memo.
	if len(tx.Message.Instructions) != 4 {
		t.Fatalf("instructions = %d, want 4", len(tx.Message.Instructions))
	}
	progID := func(i int) solana.PublicKey {
		pk, err := tx.Message.ResolveProgramIDIndex(tx.Message.Instructions[i].ProgramIDIndex)
		if err != nil {
			t.Fatalf("resolve program %d: %v", i, err)
		}
		return pk
	}
	computeBudget := solana.MustPublicKeyFromBase58(computeBudgetProgramAddress)
	memoProgram := solana.MustPublicKeyFromBase58(memoProgramAddress)

	if !progID(0).Equals(computeBudget) || !progID(1).Equals(computeBudget) {
		t.Error("instructions 0,1 must target the compute budget program")
	}
	if !progID(2).Equals(tokenProgram) {
		t.Errorf("instruction 2 program = %s, want token program", progID(2))
	}
	if !progID(3).Equals(memoProgram) {
		t.Errorf("instruction 3 program = %s, want memo program", progID(3))
	}

	// Discriminators.
	if d := []byte(tx.Message.Instructions[0].Data); len(d) != 5 || d[0] != 0x02 {
		t.Errorf("instr0 data = %x, want SetComputeUnitLimit (0x02 + u32)", d)
	}
	if d := []byte(tx.Message.Instructions[1].Data); len(d) != 9 || d[0] != 0x03 {
		t.Errorf("instr1 data = %x, want SetComputeUnitPrice (0x03 + u64)", d)
	}
	transfer := []byte(tx.Message.Instructions[2].Data)
	if len(transfer) != 10 || transfer[0] != 0x0c || transfer[9] != decimals {
		t.Errorf("instr2 data = %x, want TransferChecked (0x0c + amount + decimals=%d)", transfer, decimals)
	}
	// memo is 16 random bytes hex-encoded = 32 chars.
	if d := []byte(tx.Message.Instructions[3].Data); len(d) != 32 {
		t.Errorf("memo data length = %d, want 32 hex chars", len(d))
	}

	// TransferChecked accounts: [sourceATA, mint, destATA, payer].
	sourceATA, _ := deriveATA(payer, tokenProgram, mint)
	destATA, _ := deriveATA(payTo, tokenProgram, mint)
	acctKeys := tx.Message.AccountKeys
	ti := tx.Message.Instructions[2].Accounts
	if len(ti) != 4 {
		t.Fatalf("transfer accounts = %d, want 4", len(ti))
	}
	if !acctKeys[ti[0]].Equals(sourceATA) {
		t.Errorf("transfer account[0] = %s, want sourceATA %s", acctKeys[ti[0]], sourceATA)
	}
	if !acctKeys[ti[1]].Equals(mint) {
		t.Errorf("transfer account[1] = %s, want mint", acctKeys[ti[1]])
	}
	if !acctKeys[ti[2]].Equals(destATA) {
		t.Errorf("transfer account[2] = %s, want destATA %s", acctKeys[ti[2]], destATA)
	}
	if !acctKeys[ti[3]].Equals(payer) {
		t.Errorf("transfer account[3] = %s, want payer", acctKeys[ti[3]])
	}
}

// TestSolanaPaymentEnvelope verifies the base64 header payload matches the pinned
// x402 v2 SVM wire format (camelCase keys, echoed "accepted" requirement).
func TestSolanaPaymentEnvelope(t *testing.T) {
	opt := PaymentOption{
		Scheme:            "exact",
		Network:           "solana",
		Amount:            "1000",
		Asset:             USDCSolanaMainnet,
		PayTo:             "Recipmust be a real-ish string",
		MaxTimeoutSeconds: 60,
		Extra:             map[string]any{"feePayer": "FeePayerKey"},
	}
	env := solanaPaymentEnvelope{
		X402Version: 2,
		Payload:     map[string]string{"transaction": "BASE64TX"},
		Accepted:    opt,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["x402Version"] != float64(2) {
		t.Errorf("x402Version = %v, want 2", decoded["x402Version"])
	}
	payload, ok := decoded["payload"].(map[string]any)
	if !ok || payload["transaction"] != "BASE64TX" {
		t.Errorf("payload = %v, want {transaction: BASE64TX}", decoded["payload"])
	}
	accepted, ok := decoded["accepted"].(map[string]any)
	if !ok {
		t.Fatalf("accepted missing/wrong type: %v", decoded["accepted"])
	}
	for _, k := range []string{"scheme", "network", "asset", "amount", "payTo", "maxTimeoutSeconds", "extra"} {
		if _, ok := accepted[k]; !ok {
			t.Errorf("accepted missing key %q", k)
		}
	}
	extra, _ := accepted["extra"].(map[string]any)
	if extra["feePayer"] != "FeePayerKey" {
		t.Errorf("accepted.extra.feePayer = %v, want FeePayerKey", extra["feePayer"])
	}
	_ = raw
}

// TestSolanaKeypairAndPublicKey checks 64-byte and 32-byte key handling.
func TestSolanaKeypairAndPublicKey(t *testing.T) {
	priv, err := solana.NewRandomPrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	full := base58.Encode(priv) // 64-byte keypair
	seed := base58.Encode(priv[:32])

	wantPub := priv.PublicKey().String()

	for name, key := range map[string]string{"64-byte": full, "32-byte-seed": seed} {
		got, err := GetSolanaPublicKey(key)
		if err != nil {
			t.Fatalf("%s GetSolanaPublicKey: %v", name, err)
		}
		if got != wantPub {
			t.Errorf("%s pubkey = %s, want %s", name, got, wantPub)
		}
	}

	// A 64-byte keypair must produce the same signature-capable key both ways.
	ed, err := solanaKeypair(full)
	if err != nil || len(ed) != ed25519.PrivateKeySize {
		t.Fatalf("solanaKeypair(full): key len %d err %v", len(ed), err)
	}
}
