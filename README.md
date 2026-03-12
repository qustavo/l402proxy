# l402proxy

Drop an L402 payment gate in front of any HTTP service in one command.

No database, no ceremony — single binary + Lightning node.

## How it works

1. Request arrives with no auth → proxy responds `402 Payment Required` with a BOLT-11 invoice and a token in `WWW-Authenticate` and the JSON body
2. Client pays the invoice, receives the preimage
3. Client retries with `Authorization: L402 <token>:<preimage>` → proxy verifies payment → forwards to upstream

## Installation

```sh
go install github.com/qustavo/l402proxy/cmd/l402proxy@latest
```

## Usage

```sh
l402proxy \
  --upstream http://localhost:3000 \
  --price 10sat \
  --lnd-host localhost:10009 \
  --lnd-macaroon ~/.lnd/data/chain/bitcoin/mainnet/admin.macaroon \
  --lnd-cert ~/.lnd/tls.cert \
  --secret-key $(openssl rand -hex 32)
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--upstream` | required | URL of the backend service |
| `--price` | required | Price per request (`10sat`, `1000msat`) |
| `--listen` | `:8080` | Address to listen on |
| `--lnd-host` | `localhost:10009` | LND gRPC host:port |
| `--lnd-macaroon` | `~/.lnd/data/chain/bitcoin/mainnet/admin.macaroon` | Path to LND admin macaroon |
| `--lnd-cert` | `~/.lnd/tls.cert` | Path to LND TLS cert |
| `--service-name` | `l402proxy` | Label used in invoice memos |
| `--secret-key` | auto-generated | Hex-encoded 32-byte HMAC secret (tokens won't survive restarts if omitted) |

## L402 flow (curl example)

**Step 1 — first request, no auth:**

```sh
$ curl -i http://localhost:8080/api/data

HTTP/1.1 402 Payment Required
WWW-Authenticate: L402 token="eyJ...", invoice="lnbc..."

{"token":"eyJ...","invoice":"lnbc...","amount_msat":10000}
```

**Step 2 — pay the invoice, get the preimage:**

```sh
$ lncli payinvoice lnbc...
# → preimage: aabbccdd...
```

**Step 3 — retry with credentials:**

```sh
$ curl -H "Authorization: L402 eyJ...:aabbccdd..." http://localhost:8080/api/data

HTTP/1.1 200 OK
...
```

## Token format

`base64url(json_payload).hex(hmac_sha256)` — stateless, no database required. Token TTL is 24 hours.

## Comparison with Aperture

[Aperture](https://github.com/lightninglabs/aperture) is the reference L402 proxy by Lightning Labs.

| | Aperture | l402proxy |
|---|---|---|
| Database | etcd / Postgres / SQLite | None — stateless |
| Config | Complex YAML | CLI flags |
| LN backends | LND only | Pluggable interface |
| Deployment | Multi-service setup | Single binary |
| Use as library | No | Yes (Go middleware) |
| Target audience | Production infra teams | Any developer, AI builders |

l402proxy is the zero-friction entry point — the thing you use to get started in 5 minutes.
