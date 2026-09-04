package blockrun

// NewPortraitClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewPortraitClientWithAPIKey(apiKey string, opts ...PortraitClientOption) (*PortraitClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &PortraitClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewPhoneClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewPhoneClientWithAPIKey(apiKey string, opts ...PhoneClientOption) (*PhoneClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &PhoneClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}
	return client, nil
}

// NewLLMClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewLLMClientWithAPIKey(apiKey string, opts ...ClientOption) (*LLMClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", defaultTimeout())
	if err != nil {
		return nil, err
	}

	client := &LLMClient{baseClient: bc}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Check for custom API URL in environment (after options so user-set URLs win)
	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewRPCClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewRPCClientWithAPIKey(apiKey string, opts ...RPCClientOption) (*RPCClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultRPCTimeout)
	if err != nil {
		return nil, err
	}

	client := &RPCClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewVoiceClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewVoiceClientWithAPIKey(apiKey string, opts ...VoiceClientOption) (*VoiceClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultVoiceTimeout)
	if err != nil {
		return nil, err
	}

	client := &VoiceClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}
	return client, nil
}

// NewMusicClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewMusicClientWithAPIKey(apiKey string, opts ...MusicClientOption) (*MusicClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultMusicTimeout)
	if err != nil {
		return nil, err
	}

	client := &MusicClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewImageClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewImageClientWithAPIKey(apiKey string, opts ...ImageClientOption) (*ImageClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultImageTimeout)
	if err != nil {
		return nil, err
	}

	client := &ImageClient{baseClient: bc, pollInterval: imagePollInterval}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Check for custom API URL in environment (after options so user-set URLs win)
	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewSurfClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewSurfClientWithAPIKey(apiKey string, opts ...SurfClientOption) (*SurfClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &SurfClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}
	return client, nil
}

// NewAnthropicClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewAnthropicClientWithAPIKey(apiKey string, opts ...AnthropicClientOption) (*AnthropicClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", defaultTimeout())
	if err != nil {
		return nil, err
	}

	c := &AnthropicClient{baseClient: bc}
	c.Messages = &AnthropicMessagesAPI{client: bc}

	for _, opt := range opts {
		opt(c)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return c, nil
}

// NewVideoClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewVideoClientWithAPIKey(apiKey string, opts ...VideoClientOption) (*VideoClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultVideoTimeout)
	if err != nil {
		return nil, err
	}

	client := &VideoClient{baseClient: bc, pollInterval: videoPollInterval}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewSpeechClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewSpeechClientWithAPIKey(apiKey string, opts ...SpeechClientOption) (*SpeechClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultSpeechTimeout)
	if err != nil {
		return nil, err
	}

	client := &SpeechClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}

// NewRealFaceClientWithAPIKey uses account credits without a wallet. Empty key reads BLOCKRUN_API_KEY.
func NewRealFaceClientWithAPIKey(apiKey string, opts ...RealFaceClientOption) (*RealFaceClient, error) {
	bc, err := newAccountBaseClient(apiKey, "", DefaultTimeout)
	if err != nil {
		return nil, err
	}

	client := &RealFaceClient{baseClient: bc}

	for _, opt := range opts {
		opt(client)
	}

	if err := bc.checkEnvAPIURL(); err != nil {
		return nil, err
	}

	return client, nil
}
