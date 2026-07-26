# codex-quota-keeper

A small local daemon-friendly CLI for two opt-in Codex quota automations:

1. Open a new five-hour usage window after the previously observed window expires.
2. Redeem a reset credit shortly before it expires, but only after a usage threshold and grace period are satisfied.

Both automations are disabled by default. `run` is also a dry-run unless `--apply` is passed.

## Safety model

- Reads usage and the reset-credit count through the local `codex app-server`.
- Fetches detailed reset-credit inventory only when reset-credit automation is enabled.
- Never performs an action unless its feature is enabled in the config.
- Requires `--apply` in addition to the per-feature config switch.
- Defaults reset-credit redemption to the secondary/weekly window at 90% used.
- Starts a grace period the first time an expiring credit becomes eligible.
- Stores only a hash of each credit ID in local state.
- Uses atomic state writes with mode `0600`.
- Provides `cancel` to permanently skip every currently pending reset credit.

Reset-credit redemption is different from normal usage monitoring: it calls an undocumented ChatGPT backend endpoint and consumes a finite account credit. The endpoint can change without notice. Keep dry-run enabled until you have reviewed several decisions.

## Requirements

- Go 1.26 or later
- A working `codex` CLI login
- macOS for native notifications and the included `launchd` example
- File-based Codex credentials in `~/.codex/auth.json` for reset-credit redemption

Usage monitoring and window starting still work when automatic redemption is disabled.

## Build

```bash
go build -o bin/codex-quota-keeper ./cmd/codex-quota-keeper
```

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
    "expires_within": "2h",
    "grace_period": "10m",
    "usage_window": "secondary",
    "minimum_used_percent": 90
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

Skip pending reset-credit actions:

```bash
bin/codex-quota-keeper cancel
```

## Window starter

The program remembers the latest primary-window reset timestamp reported by Codex. After that timestamp passes, it runs the configured command once if the current time is inside `working_hours`.

The default command is:

```bash
codex exec --skip-git-repo-check --ephemeral "Reply exactly OK."
```

The command is configurable as an argument array. Overnight working hours such as `22:00-02:00` are supported. Equal start and end times mean all day.

This automation only starts a rolling window. It does not increase the weekly quota.

## Reset-credit redemption

A credit becomes eligible only when all of these conditions are true:

- its status is `available`;
- it has an expiration time;
- it expires within `expires_within`;
- the configured usage window meets `minimum_used_percent`;
- it has remained eligible for the full `grace_period`;
- the same credit hash has not already been redeemed.
- the user has not skipped it with `cancel`.

When a credit first becomes eligible, the program records it as pending and sends a macOS notification. `cancel` moves every pending credit into the skipped set, so later scheduled runs do not arm it again.

`usage_window` accepts:

- `primary`: five-hour usage;
- `secondary`: weekly usage;
- `either`: the higher available percentage.

`secondary` is the safer default because it avoids spending a finite reset credit only to reset a short five-hour window.

## launchd

Copy [`deploy/com.jimyag.codex-quota-keeper.plist.example`](deploy/com.jimyag.codex-quota-keeper.plist.example), replace the username and binary path, then install it under `~/Library/LaunchAgents/`.

The example intentionally omits `--apply`, so scheduled runs remain dry-run. Add `--apply` to `ProgramArguments` only after reviewing the logs and enabling the desired config sections.

The job runs once every five minutes. That interval should be shorter than the configured reset-credit grace period.

## Authentication boundary

Normal usage reads use the Codex app-server protocol and do not parse access tokens. The app-server currently reports the reset-credit count but may omit the credit IDs and expiration timestamps.

When reset-credit automation is enabled, the program reads `tokens.access_token` and `tokens.account_id` from the configured Codex auth file to fetch the detailed credit inventory. It uses the same credentials for redemption and sends them only to the configured `base_url`, which defaults to `https://chatgpt.com/backend-api`. Tokens are never printed or persisted by this program.

If Codex stores credentials in the macOS Keychain instead of `auth.json`, automatic redemption is unavailable in this initial version.
