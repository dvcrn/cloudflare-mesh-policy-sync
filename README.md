# Cloudflare Mesh -> Gateway DNS Sync

Small worker to give every Cloudflare Mesh node a private hostname, answered only by Gateway DNS for your own WARP devices

Eg: Mesh node `alpha (100.96.0.1, 2606:4700:cf1:1000::1)` -> `alpha.mesh.mydomain.com`, resolvable only through WARP

## Why though?

Private DNS zones in Cloudflare One need Enterprise. Gateway DNS policies with the Override action don't, so this creates one override policy per Mesh node and keeps them in sync with the node's Mesh IPs. There is no public DNS record, so the names only exist while you're on WARP.

## Setup

- Create a Cloudflare API token on the account with `Zero Trust: Edit` (Gateway rules and device registrations) and `Cloudflare One Connectors: Read` (Mesh nodes). Without the latter the node list comes back empty rather than failing, and the sync stops
- Devices that should resolve the names must use Gateway DNS. For Mesh nodes that means the Mesh device profile in **Traffic + DNS** mode
- Don't add the domain (e.g. `mesh.mydomain.com`) as a local domain fallback in those profiles, or the queries never reach Gateway

### Environment variables

| Variable | Where | Description |
| --- | --- | --- |
| `CLOUDFLARE_API_TOKEN` | secret | Cloudflare API token |
| `CLOUDFLARE_ACCOUNT_ID` | `wrangler.jsonc` vars | Account that owns the Mesh |
| `DOMAIN` | `wrangler.jsonc` vars | Domain to sync under, e.g. `mesh.mydomain.com`. Every Gateway rule named `<DOMAIN>: ...` is managed by the sync, and ones that don't match a current node are deleted |
| `ALLOWED_IDENTITIES` | `wrangler.jsonc` vars | Comma-separated emails whose DNS queries get the private answers. Include the Mesh identity (`warp_connector@<team>.cloudflareaccess.com`) so nodes can resolve each other |
| `STALE_AFTER` | `wrangler.jsonc` vars | Delete the rule of a node that has been disconnected for this long. Optional, defaults to `168h` |

## Running

### Deploy to Cloudflare Workers

Set your `account_id` and the vars in `wrangler.jsonc`, then:

```sh
bun install
bunx wrangler deploy          # creates the Worker and the 5-minute cron trigger
bunx wrangler secret put CLOUDFLARE_API_TOKEN
```

If you keep the secret in [fnox](https://github.com/jdx/fnox), `mise run secrets` uploads it.

### Run locally

With all variables from the table exported:

```sh
go run ./cmd/sync -n   # dry run: print planned changes
go run ./cmd/sync      # apply
```

`mise run sync` does the same with secrets loaded from fnox.

### How it works

Every run lists the Mesh nodes (`/warp_connector`) and uses the node name without `.mesh` as the DNS label (`delta.mesh` -> `delta`). A connected node reports its registration ID as the connection's `client_id`, which gives its Mesh IPs from the device registrations (`/devices/registrations`).

A disconnected node has no connection, so its IPs can't be looked up. Its existing rule is left as is until the node has been disconnected for `STALE_AFTER`, then deleted.

Each connected node gets a Gateway DNS policy named `<DOMAIN>: <label>`:

- Traffic: `any(dns.domains[*] == "<label>.<DOMAIN>")`
- Identity: `identity.email in {<ALLOWED_IDENTITIES>}`
- Action: Override with the node's Mesh IPv4 and IPv6

Both addresses are always set. With only an A answer, the AAAA lookup would fall through to public DNS and return NXDOMAIN.

When a WARP device asks Gateway for `alpha.mesh.mydomain.com`, the policy matches and Gateway answers with alpha's Mesh IPs. Anyone else, including an unauthenticated query to the Gateway DoH endpoint, gets no answer.

## License

MIT
