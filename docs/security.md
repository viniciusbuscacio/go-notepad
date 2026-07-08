# Security

The control plane lets a client operate the app, so it is guarded and, above
all, **off by default**. Nothing listens until you start it from **Settings →
REST API Server**.

## Threat model

The server is meant for **local automation** — an agent or script on the same
machine (or, deliberately, a trusted host on your LAN) driving the app. It is
**not** a public-facing API and should never be exposed to the open internet.

Two layers gate every request:

1. **API key.** Every request must carry `X-API-Key: <key>`. A missing or wrong
   key returns `401 unauthorized`. The key is a 48-character random hex string
   (`crypto/rand`), generated on first run, shown (copyable) in the panel, and
   **rotatable** — rotating immediately invalidates the old key.
2. **IP allowlist.** The client IP must match a CIDR in the allowlist. Anything
   else returns `403 forbidden`. The default is `127.0.0.1/32` (this machine
   only). Entries are normalized to a canonical CIDR; a bare IP gets a host mask
   (`/32` or `/128`).

## Bind address — no needless firewall prompt

The server binds narrowly on purpose:

- If **every** allowlisted entry is loopback, it binds to `127.0.0.1`. The port
  is not reachable from the network and Windows shows **no firewall prompt**.
- Only when the allowlist contains a **non-loopback** IP does it bind to
  `0.0.0.0` — because you have explicitly asked for LAN access. That is when the
  firewall prompt appears, and it should.

So the network exposure always matches the allowlist you configured; enabling LAN
access is a deliberate act, not a side effect.

## Transport — HTTP or HTTPS (your choice)

The **Use HTTPS** toggle in the panel picks the transport directly, independent
of the bind address: off = plain **HTTP**, on = **TLS**. It defaults to HTTP for
zero client friction on loopback. Turn it on whenever the connection leaves the
machine (LAN access), so the key and commands are encrypted on the wire — over a
plain HTTP LAN connection they would be readable by anyone sniffing the network.

The certificate is **self-signed** and generated locally (there is no public CA
for `127.0.0.1` or a LAN IP). The private key is stable — stored `0600` as
`server.key` next to `settings.json` — while the certificate is re-minted in
memory on each start with the current SANs. Because the key never changes, a
client can **pin the public key**, and that pin survives certificate
regeneration (new LAN IP, restart, …):

```bash
curl --pinnedpubkey "sha256//<fingerprint>" \
  -H "X-API-Key: <key>" https://<host>:<port>/v1/ax
```

The panel shows the fingerprint to pin. Pinning gives both encryption **and**
authentication (protection against an active man-in-the-middle) without
installing a CA. For loopback there is no wire to protect, so HTTP is the
sensible default there.

## Persistence

Preferences — including the API key, port, auto-start flag and allowlist — live
in a JSON file in the OS config dir:

- Windows: `%AppData%\go-notepad\settings.json`
- macOS: `~/Library/Application Support/go-notepad/settings.json`
- Linux: `$XDG_CONFIG_HOME/go-notepad/settings.json` (or `~/.config/...`)

The file is written with `0600` permissions. It contains the API key in plain
text, so treat it like any other local credential file. Rotate the key from the
UI if you suspect it leaked.

## What the API can and cannot do

- It can read and edit the document text and operate the visible UI (press buttons, send
  keys, type into fields, read the screen).
- Controls are labelled with a **risk level** in `/v1/ax`
  (`safe` → `destructive`); a well-behaved agent checks it before pressing.
  Note this is *advisory metadata for the client*, not an enforcement boundary —
  the server does not refuse a `destructive` press. Enforcement, if you want it,
  belongs in the agent's policy.
- It cannot read arbitrary files, run shell commands, or reach anything the app
  itself does not expose. The surface is exactly the endpoints in
  [agent-api.md](agent-api.md).

## Recommendations

- Keep the default loopback-only allowlist unless you truly need LAN access.
- Start the server only while you need it; leave auto-start off for interactive
  use.
- If you add LAN IPs, scope them tightly (a single host `/32`, not a broad
  range) and remember the firewall prompt is expected at that point.
