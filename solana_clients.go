package blockrun

// Solana constructors for every BlockRun client.
//
// Each NewXClientSolana mirrors its Base counterpart NewXClient but pays USDC on
// Solana via sol.blockrun.ai (SVM exact-scheme x402) instead of Base (EIP-712).
// privateKey is a bs58 Solana key; if empty it is loaded via LoadSolanaWallet
// (SOLANA_WALLET_KEY → ~/.*/solana-wallet.json → ~/.blockrun/.solana-session).
// rpcURL (blockhash + mint info) defaults to DefaultSolanaRPCURL / SOLANA_RPC_URL
// when empty. Use the same With*APIURL / With*Timeout options as the Base clients.

// NewLLMClientSolana creates an LLM client paid on Solana.
func NewLLMClientSolana(privateKey, rpcURL string, opts ...ClientOption) (*LLMClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, defaultTimeout())
	if err != nil {
		return nil, err
	}
	client := &LLMClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewAnthropicClientSolana creates an Anthropic-native client paid on Solana.
func NewAnthropicClientSolana(privateKey, rpcURL string, opts ...AnthropicClientOption) (*AnthropicClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, defaultTimeout())
	if err != nil {
		return nil, err
	}
	c := &AnthropicClient{baseClient: bc}
	c.Messages = &AnthropicMessagesAPI{client: bc}
	for _, opt := range opts {
		opt(c)
	}
	bc.checkEnvAPIURL()
	return c, nil
}

// NewImageClientSolana creates an Image client paid on Solana.
func NewImageClientSolana(privateKey, rpcURL string, opts ...ImageClientOption) (*ImageClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultImageTimeout)
	if err != nil {
		return nil, err
	}
	client := &ImageClient{baseClient: bc, pollInterval: imagePollInterval}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewVideoClientSolana creates a Video client paid on Solana.
func NewVideoClientSolana(privateKey, rpcURL string, opts ...VideoClientOption) (*VideoClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultVideoTimeout)
	if err != nil {
		return nil, err
	}
	client := &VideoClient{baseClient: bc, pollInterval: videoPollInterval}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewSpeechClientSolana creates a Speech client paid on Solana.
func NewSpeechClientSolana(privateKey, rpcURL string, opts ...SpeechClientOption) (*SpeechClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultSpeechTimeout)
	if err != nil {
		return nil, err
	}
	client := &SpeechClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewMusicClientSolana creates a Music client paid on Solana.
func NewMusicClientSolana(privateKey, rpcURL string, opts ...MusicClientOption) (*MusicClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultMusicTimeout)
	if err != nil {
		return nil, err
	}
	client := &MusicClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewVoiceClientSolana creates a Voice client paid on Solana.
func NewVoiceClientSolana(privateKey, rpcURL string, opts ...VoiceClientOption) (*VoiceClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultVoiceTimeout)
	if err != nil {
		return nil, err
	}
	client := &VoiceClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewPhoneClientSolana creates a Phone client paid on Solana.
func NewPhoneClientSolana(privateKey, rpcURL string, opts ...PhoneClientOption) (*PhoneClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	client := &PhoneClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewRealFaceClientSolana creates a RealFace client paid on Solana.
func NewRealFaceClientSolana(privateKey, rpcURL string, opts ...RealFaceClientOption) (*RealFaceClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	client := &RealFaceClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}

// NewPortraitClientSolana creates a Virtual Portrait client paid on Solana.
func NewPortraitClientSolana(privateKey, rpcURL string, opts ...PortraitClientOption) (*PortraitClient, error) {
	bc, err := newSolanaBaseClient(privateKey, "", rpcURL, DefaultTimeout)
	if err != nil {
		return nil, err
	}
	client := &PortraitClient{baseClient: bc}
	for _, opt := range opts {
		opt(client)
	}
	bc.checkEnvAPIURL()
	return client, nil
}
