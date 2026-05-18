# Changelog

All notable changes to blockrun-llm-go will be documented in this file.

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
