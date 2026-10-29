# Instructions for agents and contributors

This repository is the macOS front end for Outline: a Go provider that owns the
tunnels and a native AppKit window that talks to it. If you are an AI agent
working here, read this first. Rules marked **must not** are not stylistic
preferences — they are what keeps the project legal and safe to hand to a
stranger.

## 1. Legal invariants — do not weaken these

The project adopts the Bitseeker LLC software disclaimer and licensing standard
published at <https://github.com/cjtsh/software-disclaimer>. That page is the
authority. This repository copies its artefacts at a **pinned tag** so builds are
stable and auditable.

| File | Origin | Rule |
|---|---|---|
| `LICENSE` | `software-disclaimer` `v1.2.2` (`LICENSE`) | **Must not** be replaced with a different license, and the copyright line must not change without the LLC's instruction. |
| `DISCLAIMER.md` | `software-disclaimer` `v1.2.2` (`DISCLAIMER.md`) | **Must not** be edited by hand. Update by re-fetching a newer tag. |
| `app/Resources/FIRST-RUN-NOTICE.txt` | `software-disclaimer` `v1.2.2` (`templates/FIRST-RUN-NOTICE.txt`) | Ships inside the app bundle. **Must not** be removed from `app/build.sh`. |
| `app/Sources/Legal.swift` | this repo | Presents the notice and records acceptance. |

**Must not** do any of the following:

* Remove, gate-bypass, pre-set, or auto-accept the first-run terms screen.
  `LegalGate.ensureAccepted` must complete with an explicit user decision before
  `MainWindowController` is created. Do not add a `--skip-legal`, a debug flag,
  a `UserDefaults` seed, or an "accept by default" path.
* Remove the `Disclaimer and License…` menu item or the bundled
  `DISCLAIMER.md` / `LICENSE` resources.
* Publish a binary, DMG, or release archive without those resources inside it.
* Replace `LICENSE`/`DISCLAIMER.md` with a "simplified" or "friendlier" version.

When the upstream standard changes: re-fetch the pinned files (section 4 of that
repo's README), bump `Legal.termsVersion` in `app/Sources/Legal.swift`, bump the
version badges in this README, and mention it in the commit message. Bumping
`termsVersion` deliberately re-prompts every user, which is the point.

## 2. Secrets and privacy — do not commit

* **Must not** commit access keys, `ss://` URLs with real passwords, tokens,
  private keys, certificates, or `servers.json` / `settings.json` from a live
  install.
* Tests use documentation address ranges only: `198.51.100.0/24` and
  `203.0.113.0/24` (RFC 5737), with placeholder passwords such as
  `test-password`.
* **Must not** commit build output. `app/build/`, `provider/provider` and
  `*.icns` are ignored; keep it that way.
* Before pushing, run:
  `git grep -nE 'ss://[A-Za-z0-9+/=]{20,}|BEGIN [A-Z ]*PRIVATE KEY|[0-9a-f]{24}'`

## 3. Architecture — do not quietly change

* `provider/` is **Go standard library only**. Adding a module dependency is a
  design decision, not a convenience.
* The cipher is **never reimplemented**. `sslocal` (socks mode) and `sing-box`
  (tunnel mode) are external binaries discovered at runtime and supervised as
  child processes. Do not inline a Shadowsocks implementation.
* `app/` is **AppKit, no SwiftUI** — the project builds with the command line
  tools only, no Xcode, no Swift macro plugins. Do not introduce SwiftUI,
  `@State`, or a Swift package dependency.
* The provider exposes a small JSON API on `127.0.0.1` and is the only thing that
  keeps state. The GUI must stay stateless: reopening the window must show the
  daemon's state, not the window's.

## 4. Security boundary — do not widen

* No API accepts a command, argv, executable path, or client-chosen filesystem
  path. The only spawnable binaries are `sslocal` and `sing-box`, with arguments
  built from parsed key fields and a whitelisted cipher list.
* Bind to `127.0.0.1` only. Never `0.0.0.0`.
* The bearer token stays in a `0600` file; `servers.json` and `settings.json`
  stay `0600`.
* The privileged daemon must never expose a route that can change its own
  privileges, install software, or run shell.
* Keep the relay exclusion: a VPN that can only be repaired *through* the VPN is
  a trap.

## 5. Working on the code

```sh
cd provider && go vet ./... && go test ./...   # provider must stay green
./app/build.sh                                 # universal app + provider + icon
sudo ./install/install-tunnel.sh --accept-terms --exclude-host <relay> ...
```

* Match the existing style: small files by concern, no frameworks, comments that
  explain *why*.
* Commit messages: one line, imperative, then a short paragraph of rationale.
* Do not force-push shared history, and never rewrite history that could still
  contain a secret without telling the owner — a leaked key must be rotated, not
  merely deleted.
