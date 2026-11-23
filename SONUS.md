# Sonus (this fork)

Upstream Navidrome, on the `creeperux` branch, with what Sonus (a Windows player) needs, built into
`ghcr.io/creeperux/navidrome` (`.github/workflows/creeperux-image.yml`). Everything else is upstream's, and upstream
releases are merged in.

- **Sonus Connect** (`server/connect`, see its README): a user's players see and control each other.
- **Sonus's look** in the web UI: name, icons, and the default Sonus theme (`ui/src/themes/sonus.js`).
- **Two-step verification and app passwords** (`core/twofactor`, `server/sonus_twofactor.go`, `ui/src/sonus`), below.

## Two-step verification

With it on, the account password alone opens nothing:

- The web UI asks for a code from an authenticator app (TOTP, RFC 6238) after the password. Ten recovery codes, shown
  once at setup, each sign in once in place of a code.
- The Subsonic and Jellyfin APIs take **app passwords** instead of the account password: clients cannot ask for a
  code. Each user makes them in the web UI (Personal settings), one per client or device, and revokes them there.
  They work as `p=` or as `t=`/`s=`, with or without their dashes. The web UI signs its own Subsonic calls with its
  session (`jwt=`), so no password-derived token is kept in the browser.

The policy is `Sonus.TwoFactor` (`ND_SONUS_TWOFACTOR`):

| Value | |
|---|---|
| `required` (default) | Everyone. A user without it is asked to set it up at their next web sign-in. Until then their account password still works in the APIs, so clients keep working until it is set up. |
| `optional` | Each user turns it on (and off) in Personal settings. |
| `off` | Neither asked nor checked; what was set up is kept. The way back in when codes and recovery codes are lost: start with it, sign in, and have an admin reset the user (or turn it on again). |

An admin can reset someone's two-step verification (`POST /auth/2fa/users/{id}/reset`): they set it up again at their
next sign-in, and their app passwords keep working.

Storage: tables `sonus_two_factor`, `sonus_recovery_code` and `sonus_app_password`, created by `db.Init` after
upstream's migrations and outside them (`db/sonus.go`). Upstream Navidrome, switched back to, starts and ignores them.
Secrets are encrypted as Navidrome encrypts passwords (`PasswordEncryptionKey`; set your own); recovery codes are
kept only as hashes. Each authenticator code is taken once.
