# codex-quota-keeper

[![Go Report Card](https://goreportcard.com/badge/github.com/jimyag/codex-quota-keeper)](https://goreportcard.com/report/github.com/jimyag/codex-quota-keeper)
[![codecov](https://codecov.io/gh/jimyag/codex-quota-keeper/branch/main/graph/badge.svg)](https://codecov.io/gh/jimyag/codex-quota-keeper)
[![License](https://img.shields.io/github/license/jimyag/codex-quota-keeper)](LICENSE)
[![Release](https://img.shields.io/github/v/release/jimyag/codex-quota-keeper)](https://github.com/jimyag/codex-quota-keeper/releases)

A small local daemon-friendly CLI for two opt-in Codex quota automations:

1. Open a new five-hour usage window after the previously observed window expires.
2. Redeem a reset credit shortly before it expires so that it is not wasted.

Both automations are disabled by default. `run` is also a dry-run unless `--apply` is passed.

## Safety model

- Reads usage and the reset-credit count through the local `codex app-server`.
- Fetches detailed reset-credit inventory only when reset-credit automation is enabled.
- Never performs an action unless its feature is enabled in the config.
- Requires `--apply` in addition to the per-feature config switch.
- Does not automatically redeem based on quota usage; normal redemption remains manual.
- Redeems only when an available credit has less than `redeem_before_expiry` remaining.
- Stores only a hash of each credit ID in local state.
- Uses atomic state writes with mode `0600`.

Reset-credit redemption is different from normal usage monitoring: it calls an undocumented ChatGPT backend endpoint and consumes a finite account credit. The endpoint can change without notice. Keep dry-run enabled until you have reviewed several decisions.

## Requirements

- Go 1.26 or later
- A working `codex` CLI login
- macOS for native notifications and the included `launchd` example
- File-based Codex credentials in `~/.codex/auth.json` for reset-credit redemption

Usage monitoring and window starting still work when automatic redemption is disabled.

## Install

Download a binary for Linux, macOS, or Windows from
[GitHub Releases](https://github.com/jimyag/codex-quota-keeper/releases), or install
from source:

```bash
go install github.com/jimyag/codex-quota-keeper/cmd/codex-quota-keeper@latest
```

## Build

```bash
go build -o bin/codex-quota-keeper ./cmd/codex-quota-keeper
```

Version information is provided by
[`jimmicro/version`](https://github.com/jimmicro/version):

```bash
bin/codex-quota-keeper version
bin/codex-quota-keeper --version
```

Pushing a `v*` tag runs GoReleaser and publishes archives for Linux, macOS, and
Windows on amd64 and arm64.

## Initialize

```bash
bin/codex-quota-keeper init
```

This creates `~/.config/codex-quota-keeper/config.json` with both automations disabled.

Review the config:

```json
{
  "window_starter": {
    "enabled": false,
    "working_hours": "09:00-23:00"
  },
  "reset_credit": {
    "enabled": false,
    "redeem_before_expiry": "10m"
  }
}
```

The complete example is in [`config.example.json`](config.example.json).

## Run

Dry-run:

```bash
bin/codex-quota-keeper run
```

Machine-readable dry-run:

```bash
bin/codex-quota-keeper run --json
```

Perform eligible actions:

```bash
bin/codex-quota-keeper run --apply
```

Enabling a feature in the config is not sufficient by itself. The process must also receive `--apply`.

## Window starter

The program remembers the latest primary-window reset timestamp reported by Codex. After that timestamp passes, it runs the configured command once if the current time is inside `working_hours`.

The default command is:

```bash
codex exec --skip-git-repo-check --ephemeral "Reply exactly OK."
```

The command is configurable as an argument array. Overnight working hours such as `22:00-02:00` are supported. Equal start and end times mean all day.

This automation only starts a rolling window. It does not increase the weekly quota.

## Reset-credit redemption

A credit is automatically redeemed only when all of these conditions are true:

- its status is `available`;
- it has an expiration time;
- it has less than `redeem_before_expiry` remaining;
- the same credit hash has not already been redeemed;
- reset-credit automation is enabled and the process receives `--apply`.

The default `redeem_before_expiry` is `10m`. Quota usage does not affect this
decision: credits with more time remaining are left for manual use, while
credits in their final ten minutes are redeemed to avoid wasting them.

## launchd

Copy [`deploy/com.jimyag.codex-quota-keeper.plist.example`](deploy/com.jimyag.codex-quota-keeper.plist.example), replace the username and binary path, then install it under `~/Library/LaunchAgents/`.

The example intentionally omits `--apply`, so scheduled runs remain dry-run. Add `--apply` to `ProgramArguments` only after reviewing the logs and enabling the desired config sections.

The job runs once every five minutes. Keep that interval shorter than
`redeem_before_expiry` so an expiring credit is observed in time.

## Authentication boundary

Normal usage reads use the Codex app-server protocol and do not parse access tokens. The app-server currently reports the reset-credit count but may omit the credit IDs and expiration timestamps.

When reset-credit automation is enabled, the program reads `tokens.access_token` and `tokens.account_id` from the configured Codex auth file to fetch the detailed credit inventory. It uses the same credentials for redemption and sends them only to the configured `base_url`, which defaults to `https://chatgpt.com/backend-api`. Tokens are never printed or persisted by this program.

If Codex stores credentials in the macOS Keychain instead of `auth.json`, automatic redemption is unavailable in this initial version.
