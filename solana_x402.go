package blockrun

// Solana (SVM) x402 "exact"-scheme payment signing.
//
// Mirrors the reference Python x402 SDK (x402/mechanisms/svm/exact/client.py):
// build a v0 transaction that pays USDC via SPL TransferChecked, signed locally
// with the client's ed25519 key. The facilitator (fee payer) co-signs and
// submits it, so USDC moves gaslessly for the client. The response body is never
// touched — the official SDK still parses the gateway's verbatim upstream reply.
//
// SECURITY: the bs58 key is used ONLY for local ed25519 signing. The key never
// leaves the machine; only the signed transaction is transmitted.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
)

// solanaPaymentEnvelope is the x402 v2 PaymentPayload for the SVM exact scheme,
// serialized with camelCase keys and base64-encoded into the payment header.
// "accepted" echoes the fulfilled 402 requirement verbatim.
type solanaPaymentEnvelope struct {
	X402Version int               `json:"x402Version"`
	Payload     map[string]string `json:"payload"` // {"transaction": <base64 tx>}
	Accepted    PaymentOption     `json:"accepted"`
}

// CreateSolanaPaymentPayload builds a signed x402 SVM exact-scheme payment
// payload (base64) for the given 402 payment option. rpcURL (blockhash + mint
// info) defaults to DefaultSolanaRPCURL when empty. resourceURL/description/
// extensions are accepted for signature parity with CreatePaymentPayload; the
// SVM envelope does not carry them (matching the Python client).
func CreateSolanaPaymentPayload(bs58Key string, option *PaymentOption, resourceURL, description string, extensions map[string]any, rpcURL string) (string, error) {
	if option == nil {
		return "", &PaymentError{Message: "nil payment option"}
	}
	if rpcURL == "" {
		rpcURL = DefaultSolanaRPCURL
	}

	edPriv, err := solanaKeypair(bs58Key)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("invalid Solana wallet key: %v", err)}
	}
	priv := solana.PrivateKey(edPriv)
	payer := priv.PublicKey()

	feePayerStr, _ := option.Extra["feePayer"].(string)
	if feePayerStr == "" {
		return "", &PaymentError{Message: "feePayer is required in payment requirement extra for Solana transactions"}
	}
	feePayer, err := solana.PublicKeyFromBase58(feePayerStr)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("invalid feePayer: %v", err)}
	}
	payTo, err := solana.PublicKeyFromBase58(option.PayTo)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("invalid payTo: %v", err)}
	}
	mint, err := solana.PublicKeyFromBase58(option.Asset)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("invalid asset mint: %v", err)}
	}

	amount, err := strconv.ParseUint(option.Amount, 10, 64)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("invalid amount %q: %v", option.Amount, err)}
	}

	// Token program + decimals from the mint account (Token vs Token-2022).
	tokenProgram, decimals, err := solanaMintInfo(rpcURL, option.Asset)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("failed to fetch mint info: %v", err)}
	}

	blockhash, err := solanaLatestBlockhash(rpcURL)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("failed to fetch blockhash: %v", err)}
	}

	txBytes, err := buildSignedSolanaExactTx(priv, feePayer, payer, payTo, mint, tokenProgram, amount, decimals, blockhash)
	if err != nil {
		return "", &PaymentError{Message: err.Error()}
	}

	envelope := solanaPaymentEnvelope{
		X402Version: 2,
		Payload:     map[string]string{"transaction": base64.StdEncoding.EncodeToString(txBytes)},
		Accepted:    *option,
	}
	jsonBytes, err := json.Marshal(envelope)
	if err != nil {
		return "", &PaymentError{Message: fmt.Sprintf("marshal payload: %v", err)}
	}
	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

// buildSignedSolanaExactTx builds and client-signs the x402 SVM exact-scheme v0
// transaction, returning the serialized bytes. It performs no I/O so it is unit
// testable: the four instructions (compute limit, compute price, SPL
// TransferChecked, memo nonce) are compiled into a MessageV0 with feePayer as
// payer, and the client's ed25519 signature is placed at slot 1 (slot 0 is the
// fee payer placeholder the facilitator fills in).
func buildSignedSolanaExactTx(priv solana.PrivateKey, feePayer, payer, payTo, mint, tokenProgram solana.PublicKey, amount uint64, decimals uint8, blockhash solana.Hash) ([]byte, error) {
	sourceATA, err := deriveATA(payer, tokenProgram, mint)
	if err != nil {
		return nil, fmt.Errorf("derive source ATA: %w", err)
	}
	destATA, err := deriveATA(payTo, tokenProgram, mint)
	if err != nil {
		return nil, fmt.Errorf("derive dest ATA: %w", err)
	}

	computeBudget := solana.MustPublicKeyFromBase58(computeBudgetProgramAddress)
	memoProgram := solana.MustPublicKeyFromBase58(memoProgramAddress)

	// 1. SetComputeUnitLimit: [0x02, u32 LE units]
	cuLimitData := make([]byte, 5)
	cuLimitData[0] = 0x02
	binary.LittleEndian.PutUint32(cuLimitData[1:], defaultComputeUnitLimit)

	// 2. SetComputeUnitPrice: [0x03, u64 LE microLamports]
	cuPriceData := make([]byte, 9)
	cuPriceData[0] = 0x03
	binary.LittleEndian.PutUint64(cuPriceData[1:], defaultComputeUnitPriceMicro)

	// 3. TransferChecked: [0x0c, u64 LE amount, u8 decimals]
	transferData := make([]byte, 10)
	transferData[0] = 0x0c
	binary.LittleEndian.PutUint64(transferData[1:9], amount)
	transferData[9] = decimals

	// 4. Memo: random 16-byte nonce, hex-encoded (uniqueness; matches Python).
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	memoData := []byte(hex.EncodeToString(nonce))

	instructions := []solana.Instruction{
		solana.NewInstruction(computeBudget, nil, cuLimitData),
		solana.NewInstruction(computeBudget, nil, cuPriceData),
		solana.NewInstruction(tokenProgram, solana.AccountMetaSlice{
			{PublicKey: sourceATA, IsWritable: true, IsSigner: false},
			{PublicKey: mint, IsWritable: false, IsSigner: false},
			{PublicKey: destATA, IsWritable: true, IsSigner: false},
			{PublicKey: payer, IsWritable: false, IsSigner: true},
		}, transferData),
		solana.NewInstruction(memoProgram, nil, memoData),
	}

	tx, err := solana.NewTransaction(instructions, blockhash, solana.TransactionPayer(feePayer))
	if err != nil {
		return nil, fmt.Errorf("build transaction: %w", err)
	}
	tx.Message.SetVersion(solana.MessageVersionV0)

	// For a v0 transaction, MarshalBinary already prepends the 0x80 version byte,
	// which is exactly the payload that must be ed25519-signed.
	msgBytes, err := tx.Message.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	clientSig, err := priv.Sign(msgBytes)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// Signature slot 0 = fee payer (facilitator fills it in), slot 1 = client.
	tx.Signatures = []solana.Signature{{}, clientSig}

	return tx.MarshalBinary()
}

// deriveATA derives the associated token account for owner/mint under the given
// token program, matching the SPL ATA program's seed layout.
func deriveATA(owner, tokenProgram, mint solana.PublicKey) (solana.PublicKey, error) {
	ata, _, err := solana.FindProgramAddress(
		[][]byte{owner[:], tokenProgram[:], mint[:]},
		solana.MustPublicKeyFromBase58(associatedTokenProgramAddr),
	)
	return ata, err
}

// solanaRPCCall performs a single JSON-RPC call and decodes result into out.
func solanaRPCCall(rpcURL, method string, params []any, out any) error {
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, rpcURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode rpc response: %w (body: %s)", err, truncate(body, 200))
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	return json.Unmarshal(envelope.Result, out)
}

// solanaLatestBlockhash fetches a recent blockhash for the transaction.
func solanaLatestBlockhash(rpcURL string) (solana.Hash, error) {
	var res struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err := solanaRPCCall(rpcURL, "getLatestBlockhash", []any{map[string]string{"commitment": "confirmed"}}, &res); err != nil {
		return solana.Hash{}, err
	}
	if res.Value.Blockhash == "" {
		return solana.Hash{}, fmt.Errorf("empty blockhash in rpc response")
	}
	return solana.HashFromBase58(res.Value.Blockhash)
}

// solanaMintInfo returns the mint's token program (owner) and decimals.
func solanaMintInfo(rpcURL, mint string) (solana.PublicKey, uint8, error) {
	var res struct {
		Value *struct {
			Owner string   `json:"owner"`
			Data  []string `json:"data"` // [base64Data, "base64"]
		} `json:"value"`
	}
	err := solanaRPCCall(rpcURL, "getAccountInfo", []any{mint, map[string]string{"encoding": "base64", "commitment": "confirmed"}}, &res)
	if err != nil {
		return solana.PublicKey{}, 0, err
	}
	if res.Value == nil {
		return solana.PublicKey{}, 0, fmt.Errorf("mint account not found: %s", mint)
	}
	owner, err := solana.PublicKeyFromBase58(res.Value.Owner)
	if err != nil {
		return solana.PublicKey{}, 0, fmt.Errorf("invalid mint owner: %w", err)
	}
	if len(res.Value.Data) < 1 {
		return solana.PublicKey{}, 0, fmt.Errorf("mint account has no data")
	}
	raw, err := base64.StdEncoding.DecodeString(res.Value.Data[0])
	if err != nil {
		return solana.PublicKey{}, 0, fmt.Errorf("decode mint data: %w", err)
	}
	// SPL Mint layout: decimals is at byte offset 44.
	if len(raw) < 45 {
		return solana.PublicKey{}, 0, fmt.Errorf("mint data too short: %d bytes", len(raw))
	}
	return owner, raw[44], nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
