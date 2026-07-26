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

- Automatic redemption is disabled by default.
- Both `reset_credit.enabled` and `run --apply` are required.
- Credits are eligible only when they have less than
  `redeem_before_expiry` remaining.
- A hash of each redeemed Credit ID is stored locally to avoid duplicate
  requests if the backend is slow to update.
- State is written atomically with mode `0600`.
- Logs and JSON output contain hashes instead of raw Credit IDs.

Reset Credit redemption calls an undocumented ChatGPT backend endpoint and
consumes a finite account credit. The endpoint can change without notice. Keep
dry-run enabled until you have reviewed several scans.

## Requirements

- Go 1.26 or later when building from source
- File-based Codex credentials in `~/.codex/auth.json`
- macOS when using the included `launchd` example

If Codex stores credentials in the macOS Keychain instead of `auth.json`,
automatic redemption is unavailable.

## Install

Download a binary for Linux, macOS, or Windows from
[GitHub Releases](https://github.com/jimyag/codex-quota-keeper/releases), or
install from source:

```bash
go install github.com/jimyag/codex-quota-keeper/cmd/codex-quota-keeper@latest
```

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
  "state_file": "~/.local/state/codex-quota-keeper/state.json",
  "reset_credit": {
    "enabled": false,
    "redeem_before_expiry": "10m",
    "auth_file": "~/.codex/auth.json",
    "base_url": "https://chatgpt.com/backend-api",
    "request_timeout": "20s"
  }
}
```

Set `enabled` to `true` only after reviewing dry-run output.

## Run

Scan without redeeming:

```bash
codex-quota-keeper run
```

Scan with JSON output:

```bash
codex-quota-keeper run --json
```

Redeem eligible Credits:

```bash
codex-quota-keeper run --apply
```

With the default threshold:

- 10 minutes or more remaining: leave the Credit for manual use.
- Less than 10 minutes remaining: redeem it when `--apply` is present.

## launchd

Copy
[`deploy/com.jimyag.codex-quota-keeper.plist.example`](deploy/com.jimyag.codex-quota-keeper.plist.example),
replace the username and binary path, and install it under
`~/Library/LaunchAgents/`.

The example runs every five minutes and writes logs to:

```text
/tmp/codex-quota-keeper.log
/tmp/codex-quota-keeper.err.log
```

The example intentionally omits `--apply`, so it remains a dry-run. Add
`--apply` to `ProgramArguments` only after enabling the feature and reviewing
the logs.

Keep the scan interval shorter than `redeem_before_expiry` so an expiring
Credit is observed in time.

## Release

Pushing a `v*` tag runs GoReleaser and publishes archives for Linux, macOS, and
Windows on amd64 and arm64.
