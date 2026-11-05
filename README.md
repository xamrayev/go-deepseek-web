# go-deepseek-web

A Go library that talks to the DeepSeek **web** interface (`chat.deepseek.com`) and re-exposes it behind OpenAI-, Anthropic- and Ollama-compatible HTTP endpoints.

Two packages, usable separately:

| Package | What it is |
|---|---|
| `deepseek` | The web client: cookie/token auth, the Proof-of-Work solver, SSE streaming, sessions, and a multi-account pool with cooldowns and 429 failover. |
| `server` | A local HTTP proxy built on that client, serving `/v1/chat/completions`, `/v1/messages`, `/v1/responses` and the Ollama `/api/*` routes. |

This is a web-interface bridge, not a client for DeepSeek's official paid API — it needs a browser session, and it is subject to whatever the web interface does. For the official API, use an ordinary OpenAI-compatible SDK against `api.deepseek.com`.

```
go get github.com/xamrayev/go-deepseek-web
```

Requires Go 1.24.

## Running the proxy

```go
package main

import (
	"log"

	"github.com/xamrayev/go-deepseek-web/server"
)

func main() {
	srv := server.NewServer(server.ServerOptions{
		Host:        "127.0.0.1",
		Port:        9655,
		AccountsDir: "./accounts",
	})
	log.Fatal(srv.Start())
}
```

Any OpenAI-compatible client can then point at it:

```bash
curl http://localhost:9655/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"Hello"}]}'
```

`ProxyAPIKey` turns on authentication, accepted as either `Authorization: Bearer <key>` or `x-api-key: <key>`. Left empty, the proxy serves every request — bind it to loopback.

### Endpoints

| Route | Wire format |
|---|---|
| `POST /v1/chat/completions` | OpenAI Chat Completions |
| `POST /v1/responses` | OpenAI Responses |
| `POST /v1/messages` | Anthropic Messages |
| `POST /api/chat`, `POST /api/generate` | Ollama |
| `GET /v1/models`, `GET /api/tags` | Model listings |
| `GET /health`, `GET /readyz` | Liveness |

Streaming works on all three chat formats. Model names are resolved loosely, so `claude-sonnet-4`, `gpt-4o` and `o3` all map onto a DeepSeek model rather than erroring — which is what lets existing clients connect unchanged.

### Sessions and stateless mode

By default the proxy keeps one upstream DeepSeek chat per agent (`x-agent-session` header, else the caller's address) and continues it on every request. That suits clients that send only the new message, but OpenAI, Anthropic and Ollama clients resend the whole conversation each turn: in a continued chat the model then sees every earlier request again and can mistake old turns for things it already did.

Send `x-session-mode: stateless` to run a request in a fresh upstream chat that sees only that request's messages; nothing is stored on the proxy. `ServerOptions.Stateless` makes that the default, and `x-session-mode: session` opts a single request back into the agent's chat.

```bash
curl http://localhost:9655/v1/chat/completions \
  -H 'Content-Type: application/json' -H 'x-session-mode: stateless' \
  -d '{"model":"deepseek-chat","messages":[...]}'
```

Each stateless request still creates a chat in the DeepSeek account's web history.

### Tool calling is emulated

The web interface has no function-calling API. The proxy serializes the client's tool definitions into the system prompt, asks the model to answer in DeepSeek's DSML markup (or a JSON fallback), and parses the reply back into `tool_use` / `tool_calls` blocks.

It works, including multi-line string arguments, but it rests on the model following an instruction rather than on a protocol guarantee. Expect it to be less reliable than native function calling, and more so the longer an agent loop runs.

## Using the client directly

```go
client := deepseek.NewClient(deepseek.ClientOptions{})

chunks, errs := client.ChatStream(ctx, deepseek.ChatStreamRequest{
	Prompt: "Explain Proof-of-Work in two sentences",
	Model:  deepseek.ModelReasoner,
})

for {
	select {
	case chunk, ok := <-chunks:
		if !ok {
			return
		}
		fmt.Print(chunk.Content) // chunk.ReasoningContent carries the thinking trace
	case err := <-errs:
		if err != nil {
			log.Fatal(err)
		}
	}
}
```

### Continuing a conversation

DeepSeek threads a conversation through two values: the chat session, and the ID of the last message in it. Both live on the `Client`, so a conversation outlives the process only if you save them:

```go
sessionID, parent := client.Conversation() // after a reply
// ...persist them, then in a later process:
client.SetConversation(sessionID, parent)
```

`SetSessionID` is not a substitute — it clears the parent, so the next message starts a new branch of the session and the model loses what came before.

Keep one `Client` per conversation. The parent message is per-client state, so two conversations interleaved on one client will thread each other's messages.

By default, a completion rejected with 400/404/500 is retried once on a fresh session, which drops the context without telling you. Set `ClientOptions.KeepSession` to get the rejection back instead, and branch on its status:

```go
switch deepseek.StatusCode(err) {
case 429:
	// rate limited: try later, or another account
case 404:
	// the session is gone: start a new one, replaying your own transcript
}
```

Every non-200 reply is a `*deepseek.StatusError`, carrying the endpoint, status and the start of the body.

### Authentication

`AuthManager` reads a token and cookie from, in order: an explicit path, `$DEEPSEEK_TOKEN` / `$DEEPSEEK_COOKIE`, then a list of candidate files — `./deepseek-auth.json`, `./auth.json`, `~/.deepseek-web/auth.json` and a couple of legacy locations. To add your own application's config directory without forking:

```go
deepseek.ExtraAuthDirs = []string{".mytool"}
```

`ChromeAuthHelper` can drive a local Chrome over the DevTools protocol to capture a session, which is the practical way to obtain those values.

### Account pool

Point `AccountPool` at a directory of JSON credential files and it rotates between them:

```go
pool := deepseek.NewAccountPool("./accounts")
client.SetPool(pool)
```

An account is picked round-robin from those currently ready. On `429` it goes into cooldown for `Retry-After` or at least 3 minutes; on `401`/`403` for 30 minutes. If every account is cooling down, the one whose cooldown expires soonest is returned rather than failing outright.

Failover is single-shot: a request that fails is retried once on the next account, and a second failure surfaces to the caller instead of walking the whole pool.

Note that the pool only engages where you wire it in — a bare `NewClient` with no pool uses the single default credential.

## Embedding

`ServerOptions.ServiceName` labels the proxy in `/health` and in the default log lines. `OnListen` replaces those log lines entirely, so a host application can print its own:

```go
server.NewServer(server.ServerOptions{
	ServiceName: "my-proxy",
	OnListen: func(info server.ListenInfo) {
		log.Printf("ready on %s with %d accounts", info.Addr, info.ReadyAccounts)
	},
})
```

`Server.Handler()` returns the routing table as a plain `http.Handler`, for mounting inside an existing server or exercising the routes in tests.

## Status

Extracted from [nexus-cli](https://github.com/xamrayev/nexus-cli), which uses it as its proxy layer. The API is not frozen; pin a tag.

MIT licensed.
