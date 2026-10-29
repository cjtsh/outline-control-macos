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
                                                          └── supervises sslocal
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
its API. [`sslocal`](https://github.com/shadowsocks/shadowsocks-rust) is started
as a supervised child process and stopped when you disconnect.

## Install

```sh
git clone https://github.com/cjtsh/outline-control-macos.git
cd outline-control-macos
./app/build.sh            # builds the app, the provider and the icon
./install/install.sh      # /Applications + LaunchAgent
open "/Applications/Outline Control.app"
```

`install.sh` puts the provider in
`~/Library/Application Support/Outline Control/` and keeps it running with the
LaunchAgent `sh.cjt.outline-control` (restarted on crash, started at login).

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
* Underneath, this is a **SOCKS5 proxy**, not a system-wide VPN. Apps must point
  at the SOCKS address shown in the footer (default `127.0.0.1:11080`). If that
  port is busy the provider takes the next free one and reports it in `/status`,
  so a stale client can never wedge the feature.

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
* **System-wide tunnel** — a `NETransparentProxy` or `utun` helper so traffic is
  captured without per-app proxy settings. That needs a root helper and an
  Apple-granted entitlement, which is a different project.

## License

MIT.
