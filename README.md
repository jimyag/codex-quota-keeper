# codex-quota-keeper

[![Go Report Card](https://goreportcard.com/badge/github.com/jimyag/codex-quota-keeper)](https://goreportcard.com/report/github.com/jimyag/codex-quota-keeper)
[![codecov](https://codecov.io/gh/jimyag/codex-quota-keeper/branch/main/graph/badge.svg)](https://codecov.io/gh/jimyag/codex-quota-keeper)
[![License](https://img.shields.io/github/license/jimyag/codex-quota-keeper)](LICENSE)
[![Release](https://img.shields.io/github/v/release/jimyag/codex-quota-keeper)](https://github.com/jimyag/codex-quota-keeper/releases)

A small scheduled CLI that redeems Codex Reset Credits shortly before they
expire so that they are not wasted.

Normal Reset Credit use remains manual. The program does not inspect quota
usage and does not start Codex usage windows.

## Safety model

- Automatic redemption requires `run --apply`.
- Running without `--apply` only inspects Credits and reports decisions.
- Credits are eligible only when they have less than
  `redeem_before_expiry` remaining.
- A hash of each redeemed Credit ID is stored locally to avoid duplicate
  requests if the backend is slow to update.
- State is written atomically with mode `0600`.
- Logs and JSON output contain hashes instead of raw Credit IDs.

Reset Credit redemption calls an undocumented ChatGPT backend endpoint and
consumes a finite account credit. The endpoint can change without notice. Run
without `--apply` until you have reviewed several scans.

## Requirements

- Go 1.26 or later when building from source
- File-based Codex credentials in `~/.codex/auth.json`

If Codex stores credentials in the macOS Keychain instead of `auth.json`,
automatic redemption is unavailable.

## Install

Download a binary for Linux, macOS, or Windows from
[GitHub Releases](https://github.com/jimyag/codex-quota-keeper/releases), or
install from source:

After downloading on Linux or macOS, make the binary executable with
`chmod +x codex-quota-keeper_*`.

```bash
go install github.com/jimyag/codex-quota-keeper/cmd/codex-quota-keeper@latest
```

For a local development build, install
[`Task`](https://taskfile.dev/installation/) and run:

```bash
task build
./bin/codex-quota-keeper version
```

Use `task release-snapshot` to build all release targets locally without
publishing them.

Version information is provided by
[`jimmicro/version`](https://github.com/jimmicro/version):

```bash
codex-quota-keeper version
codex-quota-keeper --version
```

## Configure

Create the default configuration:

```bash
codex-quota-keeper init
```

This writes `~/.config/codex-quota-keeper/config.json`:

```json
{
  "scan_interval": "10m",
  "state_file": "~/.local/state/codex-quota-keeper/state.json",
  "reset_credit": {
    "redeem_before_expiry": "10m",
    "auth_file": "~/.codex/auth.json",
    "base_url": "https://chatgpt.com/backend-api",
    "request_timeout": "20s"
  }
}
```

## Run continuously

Run in the foreground without redeeming:

```bash
codex-quota-keeper run
```

The process scans immediately, then continues scanning every `scan_interval`
until it receives an interrupt or termination signal. Configuration is reloaded
before each scan. Text output uses zerolog's human-readable console format with
one line per scan:

```text
2026-07-26T23:43:46+08:00 INF scan action=none available=4 mode=dry-run next_expiry=2026-07-27T01:00:00+08:00
```

Run continuously and redeem eligible Credits:

```bash
codex-quota-keeper run --apply
```

Use `once` for a single manual scan:

```bash
codex-quota-keeper once
codex-quota-keeper once --json
codex-quota-keeper once --apply
```

With the default threshold:

- 10 minutes or more remaining: leave the Credit for manual use.
- Less than 10 minutes remaining: redeem it when `--apply` is present.

## Process management

Start the continuous worker:

```bash
codex-quota-keeper run --apply
```

`run` remains in the foreground and exits cleanly on Ctrl-C or SIGTERM. Use an
external process manager such as launchd, systemd, or supervisord for
background execution, startup, restart policy, and log rotation.

For a simple temporary background run:

```bash
nohup codex-quota-keeper run --apply \
  >> ~/.local/state/codex-quota-keeper/codex-quota-keeper.log 2>&1 &
```

Keep the scan interval shorter than `redeem_before_expiry` so an expiring
Credit is observed in time.

## Release

Pushing a `v*` tag runs GoReleaser and publishes uncompressed binaries for
Linux, macOS, and Windows on amd64 and arm64.
