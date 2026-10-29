# Outline Control for macOS

A native macOS front end for [Outline](https://getoutline.org) that can be driven
by an API instead of by clicking.

![Outline Control](docs/screenshot.png)

## Why this exists

Outline's desktop client is an Electron app with no CLI and no local API. That
is fine when you are sitting in front of the Mac — and useless when you are not.
If you want a phone, a script, or another program to **connect, disconnect, or
switch exits** on a machine across the internet, the client has to be quovable
from the outside.

So this project splits the job in two:

```
  Outline Control.app  ──HTTP (127.0.0.1:9911)──▶  outline-controld
   (Swift/AppKit, stateless)                        (Go, launchd, owns everything)
                                                          │
                                                          ├── tunnel mode: supervises sing-box
                                                          │   (utun + default route + DNS)
                                                          └── socks mode: supervises sslocal
                                                              (shadowsocks-rust)
```

The **provider** (`outline-controld`) owns the saved exits and the tunnel, and
answers a small JSON API. The **app** is a client of that API and holds no state
of its own — close the window and the tunnel keeps running, which is exactly
what you want if the window is not the thing that matters.

## Requirements

* macOS 13 or newer (Apple silicon or Intel — the provider is a universal binary)
* [shadowsocks-rust](https://github.com/shadowsocks/shadowsocks-rust):
  `brew install shadowsocks-rust`

This project deliberately **does not implement the Shadowsocks cipher**. A
proven binary owns the wire format; we own its lifecycle, its state machine and
its API.

## Two modes

| | **tunnel** | **socks** |
|---|---|---|
| What it captures | every packet on the Mac | only apps pointed at a local SOCKS proxy |
| Engine | [sing-box](https://sing-box.sagernet.org) (`utun`, default route, DNS through the exit) | [shadowsocks-rust](https://github.com/shadowsocks/shadowsocks-rust) |
| Privileges | root LaunchDaemon | your login agent |
| Install | `sudo ./install/install-tunnel.sh` | `./install/install.sh` |

**Tunnel mode is the one that behaves like a VPN.** macOS will not hand out a
`NEPacketTunnelProvider` without an Apple-granted entitlement, so instead of a
Network Extension this mode does what every `utun`-based client does: a
privileged daemon creates the tunnel interface, takes the default route and
forwards packets through the exit. That is why it needs root — and why it
survives logout and reboot.

### Exclusions: the whole point

A VPN you can only fix *through* the VPN is a trap. The tunnel therefore always
keeps a list of destinations that bypass it:

```sh
sudo ./install/install-tunnel.sh \
  --exclude-host relay.example.com \
  --exclude-cidr 203.0.113.10/32
```

Excluded hosts resolve locally and dial directly, so the control channel that
starts and stops this VPN keeps working while the VPN is down, wrong, or being
switched to another exit. Private address space is always routed directly as
well.

## Install

```sh
git clone https://github.com/cjtsh/outline-control-macos.git
cd outline-control-macos
./app/build.sh                                  # app, provider, icon
sudo ./install/install-tunnel.sh                # full-device VPN (recommended)
# or, for the per-app SOCKS proxy:
./install/install.sh
open "/Applications/Outline Control.app"
```

The tunnel installer puts the provider in
`/Library/Application Support/Outline Control/` and runs it as the root
LaunchDaemon `sh.cjt.outline-control` (restarted on crash, started at boot), so
the VPN is available before anyone logs in. Your data stays in
`~/Library/Application Support/Outline Control/`.

Uninstall: `./install/uninstall.sh`.

## Use

* **＋ Add exit** — paste an `ss://` access key from your Outline server. The
  name comes from the key's `#fragment` and can be changed later.
* **CONNECT / DISCONNECT** — one tunnel at a time, like the original client.
* **⋯ → Rename / Forget** — relabel an exit, or delete it from this Mac.
* The footer shows the SOCKS address while connected and the client binary in use.

Exits are checked every few seconds with a TCP probe, so a dead server shows as
**Unreachable** before you click it rather than after.

## The local API

Everything the app does is available on `http://127.0.0.1:9911`:

| Method | Path | Meaning |
|---|---|---|
| `GET` | `/status` | tunnel state, active exit, SOCKS address, pid, last error |
| `GET` | `/servers` | saved exits (never includes the password) |
| `POST` | `/servers` | `{"key":"ss://…","name":"optional"}` |
| `PATCH` | `/servers/{id}` | `{"name":"Tokyo"}` |
| `DELETE` | `/servers/{id}` | forget an exit (stops the tunnel if it is live) |
| `POST` | `/connect` | `{"id":"…"}` |
| `POST` | `/disconnect` | stop the tunnel |
| `GET` | `/health` | last TCP probe per exit |

Every request needs the token from
`~/Library/Application Support/Outline Control/token`:

```sh
TOKEN=$(cat "$HOME/Library/Application Support/Outline Control/token")
curl -s -H "X-Outline-Token: $TOKEN" http://127.0.0.1:9911/status
```

## Security notes

* The API listens on **127.0.0.1 only** and requires a bearer token stored in a
  `0600` file. Nothing is exposed to the LAN.
* `servers.json` is `0600` because it holds access-key passwords. Back it up like
  a secret.
* There is no command execution surface: the provider runs exactly one binary
  (`sslocal`, or `$OUTLINE_CONTROL_SSLOCAL`) with an argument list built from
  parsed key fields. The cipher is whitelisted; unsupported ones are refused at
  parse time.
* In tunnel mode the daemon runs as root, because taking the default route
  requires it. It never accepts a command to run: the only two binaries it can
  spawn are a fixed `sing-box run -c <config it wrote itself>` and, in socks
  mode, `sslocal` with flags derived from parsed key fields.
* The tunnel is IPv4-only. IPv6 traffic is neither captured nor forwarded.
* Tunnel mode is as strong as your exit: if the exit dies, traffic stops rather
  than falling back to the open network (no silent leak), but there is no `pf`
  kill-switch either.

## Development

```sh
cd provider && go test ./...    # parsing, store permissions, API, tunnel lifecycle
./app/build.sh                  # universal app + provider + generated icon
```

Deliberately dependency-free: the provider is Go standard library only, and the
app is AppKit compiled by the command line tools — no Xcode project, no
SwiftUI macro plugins, no package manager.

## Roadmap

* **Module for Mac Remote** — let the phone start, stop and switch exits through
  the same relay that already reaches this Mac.
* **Network Extension variant** — a `NEPacketTunnelProvider` build for anyone
  with an Apple entitlement: no root, system VPN UI, on-demand rules.
* **Kill switch** — optional `pf` rules so a crash of the tunnel engine cannot
  leak a single packet.

## License

MIT.
