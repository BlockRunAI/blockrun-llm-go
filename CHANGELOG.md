# Changelog

All notable changes to blockrun-llm-go will be documented in this file.

## 0.16.0

- **`VideoClient.GenerateFromContent(ctx, content, opts)`** — submits a standard
  Seedance `content[]` body to the gateway's `POST /v1/videos` endpoint
  (validates unsupported inputs before charging, then delegates to the same
  x402 pipeline as `Generate`). For migrating existing `content[]`-shaped
  payloads unchanged; most callers should still prefer `Generate(ctx, prompt, opts)`
  with structured options. Image inputs go inside the content items; only
  `Model` and the scalar render fields are forwarded from `opts`.

## 0.15.0

Backfills four endpoint families the Go SDK never covered, and fixes a payment bug in `PM()`.

- **`Exa*` — Exa web search (`/v1/exa/*`, coverage backfill; Python/TS had it since March).** `Exa(path, body)` + `ExaSearch` / `ExaFindSimilar` / `ExaContents` / `ExaAnswer` on `*LLMClient`. $0.01/request (contents $0.002/URL).
- **`Defi*` — DefiLlama (`/v1/defillama/*`, live since 2026-05-02).** `Defi(path, params)` + `DefiProtocols` / `DefiProtocol` / `DefiChains` / `DefiYields` / `DefiPrices`. $0.005/call (prices $0.001). Array responses are wrapped as `{"data": [...]}`.
- **`Dex*` — 0x Swap + Gasless (`/v1/zerox/*`, live since 2026-05-02).** Free (no x402; BlockRun monetizes via on-chain affiliate fee): `Dex(path, params)` + `DexPrice` / `DexQuote` / `DexGaslessPrice` / `DexGaslessQuote` / `DexGaslessSubmit` (POST) / `DexGaslessStatus` / `DexChains` / `DexGaslessChains`.
- **`Modal*` — Modal sandbox compute (`/v1/modal/*`, live since 2026-04-09).** `Modal(path, body)` + `ModalSandboxCreate` ($0.01 CPU / $0.05 GPU) / `ModalSandboxExec` / `ModalSandboxStatus` / `ModalSandboxTerminate` ($0.001 each).
- **Fix: `PM()` (paid GET) now actually pays.** It previously used the payment-blind `doGet`, so any 402 from `/v1/pm/*` surfaced as an `APIError` instead of being signed and retried. Now routed through `doGetWithPayment` (same flow as the Pyth market client). `PMQuery` (POST) was already payment-aware.

## 0.14.0

Removes the X/Twitter (AttentionVC) surface — the backend dropped the integration on 2026-04-30 and every `/v1/x/*` endpoint has returned HTTP 404 since.

- **BREAKING: deleted `x_twitter.go` / `x_twitter_types.go`** — the 15 `X*` methods on `*LLMClient` (`XUserLookup`, `XFollowers`, `XSearch`, `XTrending`, ...) and their `X*` response types are gone. If you need X/Twitter data, use Grok Live Search (`Search` with the `x` source) instead.
- Cache: dropped the dead `/v1/x/` TTL rule; `readRequestBody` test helper moved to `prediction_market_test.go`.
- README: X/Twitter section removed.

## 0.13.0

Adds the new multi-chain RPC client, the 2026-06-02 Seedance video parameters, and rebuilds the free routing tier from a live sweep.

- **`RPCClient` — multi-chain JSON-RPC (40+ chains) via x402.** New file `rpc.go` wraps the new backend `POST /v1/rpc/{network}` (Tatum gateway passthrough, launched 2026-06-07). Flat $0.002 per call; a JSON-RPC batch charges per element. API on `*RPCClient`:
  - `Call(ctx, network, method, params)` — single JSON-RPC 2.0 call. EVM chains speak `eth_*`; non-EVM (Solana / Bitcoin-family / NEAR / Sui / XRP Ledger / Polkadot) speak their native JSON-RPC. `RPCResponse` carries the raw JSON-RPC envelope (`Result` as `json.RawMessage`, optional `*RPCError`) plus gateway metadata from response headers: `Network` (X-Network), `CacheHit` (X-Cache), `TxHash` (X-Payment-Receipt).
  - `Batch(ctx, network, []RPCBatchRequest)` — JSON-RPC batch, priced per element; `jsonrpc` and missing ids are auto-filled.
  - `RPCSupportedNetworks` (40 curated chains), `RPCNetworkAliases` (eth, arb, op, matic, bnb, avax, sol, btc, xrp, dot, ...), `RPCPriceUSD`. Unknown well-formed slugs fall through server-side to `{slug}-mainnet`, so new Tatum chains work without an SDK update.
- **`VideoGenerateOptions` new Seedance fields** (backend 2026-06-02):
  - `LastFrameURL` — first-and-last-frame interpolation: tweens from `ImageURL` (first frame) to `LastFrameURL` (final frame). Requires `ImageURL` + a Seedance model. Priced as image-to-video.
  - `ReferenceImageURLs` — omni / multi-reference: up to 9 reference images for character/style consistency (Seedance 2.0 only); cite them as "image 1", "image 2" in the prompt. Mutually exclusive with `ImageURL` / `LastFrameURL` / `RealFaceAssetID`.
  - token360 passthroughs that were already live upstream: `AspectRatio`, `Seed` (`*int`), `Watermark` (`*bool`), `ReturnLastFrame`.
  - Client-side validation mirrors the backend mutual-exclusion rules.
- **Free routing tier rebuilt from a 2026-06-07 live sweep** (every visible free model probed):
  - `nvidia/qwen3-next-80b-a3b-thinking` hit NVIDIA end-of-life 2026-05-21 (HTTP 410) — dropped as Complex/Reasoning primary. Complex → `nvidia/qwen3-coder-480b` (871ms probe); Reasoning → `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning` (681ms, explicit reasoning + vision).
  - `nvidia/mistral-small-4-119b` is timing out upstream (3/3 probes >60s) — not routed.
  - Simple → `nvidia/deepseek-v4-flash` (896ms probe, 1M context; recovered from the 05-09 NIM regression), replacing the privacy-encumbered `nvidia/gpt-oss-120b` default. Medium → `nvidia/llama-4-maverick`. The free table now matches the Python SDK's `FREE_TIERS`.
- **`baseClient.doRequestHeaders` / `handlePaymentAndRetryHeaders`** — internal header-returning variants of the POST request path so clients can surface gateway metadata headers; the existing `doRequest` / `handlePaymentAndRetry` signatures delegate and are unchanged.
- README: RPC section, new video examples, free-model tables refreshed (qwen3-next retired, mistral-small flagged).

## 0.12.0

Adds BlockRun Voice (ElevenLabs TTS + sound effects) and syncs the model catalog with the 2026-06-04/05 backend changes.

- **`SpeechClient` — text-to-speech & sound effects via x402.** New file `speech.go` wraps `POST /v1/audio/speech` (OpenAI-compatible TTS), `POST /v1/audio/sound-effects`, and the free `GET /v1/audio/voices`. API on `*SpeechClient`:
  - `Generate(ctx, input, *SpeechGenerateOptions)` — TTS. Models: `elevenlabs/flash-v2.5` (default, $0.05/1k chars, ~75ms), `elevenlabs/turbo-v2.5` ($0.05/1k), `elevenlabs/multilingual-v2` ($0.10/1k), `elevenlabs/v3` ($0.10/1k). Options: `Voice` (aliases sarah/george/laura/charlie/river/roger/callum/harry or raw ElevenLabs voice_ids), `ResponseFormat` (mp3/opus/pcm/wav), `Speed` (0.7–1.2). Price = `(chars / 1000) × model rate`, minimum $0.001/request.
  - `SoundEffect(ctx, text, *SoundEffectOptions)` — cinematic sound effects up to 22s, flat $0.05/generation (`elevenlabs/sound-effects`). Options: `DurationSeconds` (0.5–22), `PromptInfluence` (0–1), `ResponseFormat`.
  - `ListVoices(ctx)` — free voice discovery (rate-limited 60 req/min/IP); returns `[]VoiceInfo`.
  Construct with `NewSpeechClient("")`; options `WithSpeechAPIURL` / `WithSpeechTimeout` / `WithSpeechHTTPClient` (default timeout 120s — synthesis is synchronous).
- **Catalog sync (README + router comments):** added `xai/grok-4.3` ($1.50/$4.00, 1M context, reasoning + vision) and `xai/grok-build-0.1` ($1.50/$3.00, 256K, agentic coding) — resold via BlockRun's OpenRouter credit pool; older Grok chat SKUs are now hidden from `/v1/models`. `zai/glm-5.1`'s launch promo ended 2026-06-05 (now per-token $1.40/$4.40; glm-5/glm-5-turbo remain flat $0.001/call). `deepseek/deepseek-v4-pro` pricing corrected to $0.435/$0.87 — the 75% launch promo became the permanent list price after 2026-05-31.

## 0.11.0

Adds OpenAI-compatible JSON mode and stop sequences to chat.

- **`ResponseFormat` and `Stop` on `ChatCompletionOptions`.** The gateway now honors both OpenAI params on `/v1/chat/completions` — natively for OpenAI/Azure, and emulated for Anthropic/Bedrock (a raw-JSON system instruction with code-fence stripping for `{"type": "json_object"}`; `Stop` mapped to `stop_sequences`). Wired into both `ChatCompletion` and `ChatCompletionStream`. Example: `&blockrun.ChatCompletionOptions{ResponseFormat: map[string]string{"type": "json_object"}, Stop: []string{"END"}}`.

## 0.10.2

Promotes Anthropic's latest flagship in SmartChat routing.

- **`anthropic/claude-opus-4.8` is now the default `premium` Complex model**, replacing `anthropic/claude-opus-4.5`. Opus 4.8 is Anthropic's most capable model with a 1M-token context window. Older Opus IDs remain available for clients pinned to their pricing.

## 0.10.1

Adopts Google's latest-generation Flash model in SmartChat routing.

- **`google/gemini-3.5-flash` is now the default Flash model in the routing table.** Gemini 3.5 Flash (built-in thinking mode, $0.50 in / $3.00 out, 1M context) supersedes `google/gemini-2.5-flash` and is now used by the **auto** Medium and **premium** Simple tiers. `gemini-2.5-flash` remains available for clients pinned to its pricing.

## 0.10.0

Adds image editing and multi-image fusion to `ImageClient`, closing a gap where the README already advertised `client.Edit()` but the method did not exist.

- **`ImageClient.Edit(ctx, prompt, images, *ImageEditOptions)` — image editing & fusion via x402.** Wraps `POST /v1/images/image2image`. Pass one source image for a standard edit, or up to the provider's limit (typically 4; Gemini ~3 anchors) to fuse them — e.g. a reference photo plus a brand logo. A single image is sent as an OpenAI-compatible string; multiple images are sent as an array. Each image must be a base64 data URI (`data:image/...`).
  - `ImageEditOptions{ Model, Mask, Size, N }` — default model `openai/gpt-image-2`. Edit-capable models: `openai/gpt-image-1`, `openai/gpt-image-2`, `google/nano-banana`, `google/nano-banana-pro`. `Mask` (OpenAI inpainting) cannot be combined with multiple source images.

## 0.9.0

Brings the Go SDK to feature parity with the Python SDK's media stack (music + face/character assets) and extends `VideoClient` with the Seedance 2.0 identity-consistency options.

- **`MusicClient` — full-length music generation via x402.** New file `music.go` wraps `POST /v1/audio/generations` (MiniMax Music 2.5+, $0.1575/track, ~3 min). API on `*MusicClient`:
  - `Generate(ctx, prompt, *MusicGenerateOptions)` — instrumental by default; pass `Instrumental: &false` with `Lyrics` for a vocal track. Returns a `*MusicResponse` of `AudioTrack`s (CDN URLs valid ~24h — download promptly).
  Construct with `NewMusicClient("")`; options `WithMusicAPIURL` / `WithMusicTimeout` / `WithMusicHTTPClient` (default timeout 210s for the 1-3 min generation).

- **`PortraitClient` — enroll Virtual Portraits (AI-generated characters) via x402.** New file `portrait.go` wraps `POST /v1/portrait/enroll` ($0.01 USDC, one-time, no KYC) and the free `GET /v1/wallet/<addr>/portraits` listing. Methods on `*PortraitClient`:
  - `Enroll(ctx, name, imageURL)` — register a character image, returns a `ta_xxxxxxxx` asset id. Settles only after the portrait is registered upstream, so a failed enrollment (HTTP 502) doesn't charge.
  - `ListPortraits(ctx, walletAddress)` — list a wallet's portraits (empty address = own wallet).

- **`RealFaceClient` — enroll a real person's likeness via x402.** New file `realface.go` wraps the three-step flow: `POST /v1/realface/init` (free) → on-phone liveness check → `POST /v1/realface/enroll` ($0.01 USDC, no KYC), plus `GET /v1/realface/status` and the free `GET /v1/wallet/<addr>/realfaces` listing. Methods on `*RealFaceClient`:
  - `Init(ctx, name, groupID)` — start (or refresh) enrollment; returns a `group_id` + `h5_link` the person scans on their phone.
  - `Status(ctx, groupID)` / `WaitForActive(ctx, groupID, *WaitForActiveOptions)` — poll until the liveness check completes.
  - `Enroll(ctx, name, imageURL, groupID)` — finalize, returns a `ta_xxxxxxxx` asset id. Failures (425 not-active / 422 face-mismatch / 502 upstream) don't charge.
  - `ListRealFaces(ctx, walletAddress)` — list a wallet's RealFaces.

- **`VideoClient` — Seedance 2.0 identity consistency.** `VideoGenerateOptions` gains `RealFaceAssetID` (a `ta_` Virtual Portrait or RealFace asset, Seedance 2.0 fast/pro only — mutually exclusive with `ImageURL`), `Resolution` (`360p`/`480p`/`720p`/`1080p`/`4K`), and `GenerateAudio *bool` (nil defers to the model default). Passing both `ImageURL` and `RealFaceAssetID`, or a `RealFaceAssetID` without the `ta_` prefix, now returns a `ValidationError`.

## 0.8.0

- **`PhoneClient` — Twilio-backed phone lookup + number provisioning via x402.** New file `phone.go` wraps the backend's `/v1/phone/*` partner endpoints. Methods on `*PhoneClient`:
  - `Lookup(ctx, phoneNumber)` — carrier + line-type ($0.01)
  - `LookupFraud(ctx, phoneNumber)` — adds SIM-swap / call-forwarding signals ($0.05)
  - `BuyNumber(ctx, BuyNumberOptions{Country, AreaCode})` — provision a US/CA number with a 30-day lease bound to your wallet ($5.00). Payment is settled only after Twilio confirms the purchase, so failed buys never charge your wallet.
  - `RenewNumber(ctx, phoneNumber)` — extend by 30 days ($5.00)
  - `ListNumbers(ctx)` — list your active numbers ($0.001)
  - `ReleaseNumber(ctx, phoneNumber)` — return a number to the pool (free, still flows through x402 for wallet-identity verification)
  Construct with `NewPhoneClient("")` (env-key fallback to `BLOCKRUN_WALLET_KEY` / `BASE_CHAIN_WALLET_KEY`); functional options `WithPhoneAPIURL` / `WithPhoneTimeout` / `WithPhoneHTTPClient`. After buying a number, use it as the `From` caller-ID in `VoiceClient.Call`.

- **`SurfClient` — asksurf.ai crypto-data gateway via x402.** New file `surf.go` wraps `/v1/surf/*` and exposes ~83 endpoints covering exchange data, on-chain SQL, prediction markets (Polymarket + Kalshi), wallet/social analytics, and project intelligence. Tiered pricing matches the backend: tier 1 / 2 / 3 → $0.001 / $0.005 / $0.020. API:
  - `SurfEndpoints()` — full discovery catalog as `[]SurfEndpoint`
  - `SurfEndpointInfo(path)` / `SurfPrice(path)` — single-endpoint metadata
  - `client.Get(ctx, path, params)` / `client.Post(ctx, path, body)` — direct callers; 402-payment handling reuses the existing `doGetWithPayment` / `doRequest` helpers, so paid GETs are signed transparently
  - `client.Call(ctx, path, SurfCallOptions{Params, Body})` — auto-routes GET vs POST from the catalog
  Required-param validation runs client-side before the network round trip.

- **`VoiceClient.CallOptions.From` docs reflect new backend resolution rules.** When `From` is omitted, the backend now auto-picks if your wallet owns exactly one active number; returns 403 `no_active_number` (zero owned) or 400 `ambiguous_from` (two or more) otherwise. The SDK already forwarded `From` correctly — only the doc comment was updated.

## 0.7.0

- **`VoiceClient` — AI-powered outbound phone calls via x402.** New file `voice.go` wraps the backend's `POST /v1/voice/call` (paid, $0.54/call) and `GET /v1/voice/call/{callId}` (free polling). The AI agent dials a US/Canada E.164 number and conducts a real-time conversation following your `Task` instructions; STT + LLM + TTS are handled upstream by Bland.ai. Full pass-through for `From`, `Voice` (7 presets via `VoiceNat`/`VoiceJosh`/`VoiceMaya`/`VoiceJune`/`VoicePaige`/`VoiceDerek`/`VoiceFlorian` + custom Bland IDs), `MaxDuration` (1–30 min), `Language`, `FirstSentence`, `WaitForGreeting`, `InterruptionThreshold`, and `Model` tier (`CallModelBase` / `CallModelEnhanced` / `CallModelTurbo`). Construct with `NewVoiceClient("")` (env-key fallback to `BLOCKRUN_WALLET_KEY` / `BASE_CHAIN_WALLET_KEY`); functional options `WithVoiceAPIURL` / `WithVoiceTimeout` / `WithVoiceHTTPClient`. Status polling returns the full Bland call record including any fields BlockRun adds on the gateway via `CallStatusResponse.Extra`. See README "Voice Calls" section for usage.

## 0.6.0

- **Predexon v2 endpoints exposed via typed helpers.** All v2 endpoints went live in production on 2026-05-07 (`blockrun-web-00451-cnw`). The generic `PM()` / `PMQuery()` passthrough already routed them, but call sites can now discover the new shape from method names + godoc. Ten new methods on `*LLMClient` — each is a thin wrapper, no breaking changes:
  - **Canonical cross-venue (Tier 1):** `PMMarkets`, `PMListings`, `PMOutcome`. Predexon's unified data layer with cross-venue IDs across Polymarket, Kalshi, Limitless, Opinion, Predict.Fun.
  - **Polymarket keyset pagination (Tier 1):** `PMPolymarketMarketsKeyset`, `PMPolymarketEventsKeyset` — cursor-based for stable traversal of large result sets.
  - **Sports markets (Tier 1):** `PMSportsCategories`, `PMSportsMarkets`.
  - **Wallet identity & clustering (Tier 2):** `PMWalletIdentity` (GET), `PMWalletIdentities` (POST, validates ≤200 addresses), `PMWalletCluster` (GET on-chain relationship graph).
- `PMQuery` godoc example updated from the retired `polymarket/search` POST endpoint to the live `polymarket/wallet/identities` bulk-identity POST.

## 0.5.0

- **DeepSeek V4 family in paid catalog.** Backend added `deepseek/deepseek-v4-pro` (1.6T MoE / 49B active, 1M context — strongest open-weight reasoner; **$0.50 in / $1.00 out per 1M under the 75% promo through 2026-05-31**, list $2.00/$4.00). The legacy `deepseek/deepseek-chat` and `deepseek/deepseek-reasoner` IDs are now V4 Flash non-thinking / thinking modes — repriced to **$0.20 in / $0.40 out per 1M, 1M context** (was $0.28/$0.42, 128K). `deepseek-chat` and `deepseek-reasoner` are still the eco-mode `TierMedium` and `TierReasoning` defaults; cost-sensitive callers automatically benefit from the new pricing without code changes.
- **Smart router: free `TierMedium` repointed from hidden v3.2 → visible v4-flash.** `routingTable[RoutingFree][TierMedium]` was pinned to `nvidia/deepseek-v3.2`, which is now `hidden: true` in the catalog because NVIDIA's NIM deployment is hung — backend MODEL_REDIRECTS auto-forwards calls to `nvidia/deepseek-v4-flash`. Calls were succeeding but `SmartChatResponse.Model` reported the redirected name. Repointed `TierMedium` → `nvidia/deepseek-v4-flash` so reported model matches the model that actually answered. `TierSimple` keeps `nvidia/gpt-oss-120b` — it was briefly delisted 2026-04-28 then re-enabled 2026-04-30 with `available: true` + `hidden: true` (hidden from `/v1/models` for privacy but direct calls still work, which is fine for the Go SDK because routingTable doesn't consult `/v1/models`).
- README refresh: DeepSeek section gains a V4 family note explaining the chat/reasoner repricing and the new V4 Pro flagship; routing-table caveat documents the hidden-but-callable status of `gpt-oss` and the auto-redirect chain for `v4-pro` / `v3.2` / `glm-4.7`.
- **X/Twitter methods deprecated.** BlockRun's `/v1/x/*` (AttentionVC-partnered) integration was removed from the backend on 2026-04-30 (blockrun commit 80dcf52). All `LLMClient.X*` methods (`x_twitter.go`) now carry `// Deprecated:` doc comments so `go vet` and IDEs flag use sites — calls return HTTP 404 until a replacement X data upstream is wired up. Methods stay in the package so existing call sites keep compiling.
- **DeepSeek V4 thinking + tool-call multi-turn now works.** Backend commit `f8a2d44` (2026-05-03) preserves `reasoning_content` on assistant messages with `tool_calls` for DeepSeek V4 thinking-mode (`deepseek-reasoner` / `deepseek-v4-pro`). `ChatMessage.ReasoningContent` already had `json:"reasoning_content,omitempty"`, so multi-turn DeepSeek with tools now round-trips correctly through `BuildAnthropicTextContent` and the OpenAI-compatible chat path. Server-side fix; this entry exists so users seeing past 5xx-retry-loop failures know they're resolved.

## 0.4.1

- **Smart router: Eco / Auto `TierSimple` promoted from `moonshot/kimi-k2.5` → `moonshot/kimi-k2.6`** (Moonshot's flagship — 256K context, vision + `reasoning_content`, $0.95 in / $4.00 out per 1M). The catalog now hides `kimi-k2.5` as superseded; routing the SmartChat default at the new flagship keeps the SDK aligned with the canonical catalog. README routing table updated to match.

## 0.4.0

- **New flagship model: `openai/gpt-5.5`** (released 2026-04-23, first fully retrained base since GPT-4.5). 1M context, 128K output, native agent + computer use. Pricing $5.00 / $30.00 per 1M tokens.
- **Smart router: `RoutingPremium.TierMedium` now points at `openai/gpt-5.5`** (was `gpt-5.4`). Free / Eco / Auto profiles unchanged. README routing table updated to match.

## 0.3.0

- **New image model: `openai/gpt-image-2`** (ChatGPT Images 2.0). Reasoning-driven generation with multilingual text rendering + character consistency. Pricing: $0.06 (1024²), $0.12 (1536×1024 / 1024×1536). Also supported by `client.Edit()` alongside `gpt-image-1`.
- **New video models: 3 ByteDance Seedance variants** on `VideoClient`:
  - `bytedance/seedance-1.5-pro` — $0.03/sec, 720p, 5s default (up to 10s).
  - `bytedance/seedance-2.0-fast` — $0.15/sec, ~60-80s generation, sweet-spot price/quality.
  - `bytedance/seedance-2.0` — $0.30/sec, 720p Pro quality.
  All support text-to-video and image-to-video. No SDK surface change — pass the model ID via `VideoGenerateOptions.Model`.
- `video.go`: `VideoClient` doc comment no longer claims xAI-only.
- `image.go` + `video.go`: header comments enumerate available models + pricing for discoverability.
- README: Image line mentions gpt-image-2; Video section now a proper table listing all 4 models; new Video feature row in the capability matrix.

## 0.2.1

- **NVIDIA free-tier refresh (backend 2026-04-21).** Retired `nvidia/nemotron-*`, `nvidia/mistral-large-3-675b`, `nvidia/devstral-2-123b`, `nvidia/qwen3.5-397b-a17b`, and paid `nvidia/kimi-k2.5`. The routing table in `router.go` now references the survivors + the two new models:
  - `RoutingFree`: Simple → `nvidia/gpt-oss-120b`, Medium → `nvidia/deepseek-v3.2`, Complex / Reasoning → `nvidia/qwen3-next-80b-a3b-thinking`.
  - `RoutingEco` / `RoutingAuto` Simple: `nvidia/kimi-k2.5` → `moonshot/kimi-k2.5` (canonical target of the backend redirect).
- Backend still redirects the old IDs, so upgrading is non-breaking; `SmartChat` just now names the right model in its output.
- README Smart Routing table refreshed.

## 0.2.0

- **New market-data client** (`market.go`) — `LLMClient.Price`, `.History`, and `.ListSymbols` over the Pyth-backed endpoints. Crypto, FX and commodity are fully free (price + history + list); stocks across 12 markets (us/hk/jp/kr/gb/de/fr/nl/ie/lu/cn/ca) and the `usstock` legacy alias charge for price + history, list stays free. The client handles x402 transparently on either path.
- New types: `PricePoint`, `PriceBar`, `PriceHistoryResponse`, `SymbolListResponse`, `PriceCategory`, `PriceOptions`, `HistoryOptions`, `ListOptions`.
- `ChatMessage` gains optional `ReasoningContent` and `Thinking` fields for reasoning-capable upstreams (DeepSeek Reasoner, Grok 4 / 4.20 reasoning).
- `Usage` gains optional `CacheReadInputTokens` / `CacheCreationInputTokens` for Anthropic prompt-caching telemetry.
- `Model` gains optional `BillingMode` (`paid`/`flat`/`free`), `FlatPrice`, `Categories`, `Hidden` so the `/v1/models` payload can round-trip.
- Internal: `baseClient.doGetWithPayment` + `handleGetPaymentAndRetry` support x402 on GET endpoints (previously only POST was handled).
- First tracked `VERSION` / `CHANGELOG.md`.
