# Sonus (this fork)

Upstream Navidrome, on the `creeperux` branch, with what Sonus (a Windows player) needs, built into
`ghcr.io/creeperux/navidrome` (`.github/workflows/creeperux-image.yml`). Everything else is upstream's, and upstream
releases are merged in.

- **Sonus Connect** (`server/connect`, see its README): a user's players see and control each other.
- **Sonus's look** in the web UI: name, icons, and the default Sonus theme (`ui/src/themes/sonus.js`).
- **Two-step verification and app passwords** (`core/twofactor`, `server/sonus_twofactor.go`, `ui/src/sonus`), below.
- **API keys and signing in through the browser** (`server/subsonic/sonus_auth.go`, `server/sonus_device.go`,
  `ui/src/sonus/DeviceApproval.jsx`), below: an app gets a key of its own without ever seeing the password.

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

## API keys and signing in through the browser

The Subsonic API takes OpenSubsonic's **`apiKeyAuthentication`** (v1): `apiKey=<key>` in place of `u` with
`p` or `t`/`s`. Sending `u` (or a password, a token) with it is error 43, a key that is not known (or revoked) is
error 44, and `tokenInfo` answers whose key it is. Failed keys count against the address as failed passwords do. A key
belongs to one user and is one of their app passwords: listed with them in Personal settings and revoked there. Keys
are kept as app passwords are (encrypted), and found by a hash of the key.

Apps get one by **signing in through the browser** (`sonusDeviceLogin` v1, as OAuth's device authorization grant,
RFC 8628), so the app never handles the account password nor needs to ask for a two-step code:

1. The app asks: `POST /auth/device/start` with `{"client": "Sonus", "device": "书房电脑"}` →
   `{deviceCode, userCode, verificationPath, expiresIn, interval}`.
2. It shows `userCode` (`ABCD-EFGH`) and opens `verificationPath` (`/app/#/device?code=ABCD-EFGH`, under the base
   path) in the browser. There the user signs in (with two-step verification), checks the page shows the same code,
   and allows the app or refuses it. A page opened without a code asks for it.
3. Meanwhile the app polls: `POST /auth/device/poll` with `{"deviceCode": …}` every `interval` seconds →
   `pending`, `denied`, `expired`, or `approved` with `apiKey` and `username`, handed over once. Allowing it makes the
   key, named `client · device` ("Sonus · 书房电脑").
4. Signing out, the app gives the key back: `POST /auth/device/revoke` with `{"apiKey": …}` (204; 404 when not
   known). It stops working at once.

A sign-in waits 10 minutes at most (200 at a time, kept in memory: a restart drops them and the app starts again).
`start` and `revoke` are rate-limited per address as `/auth/login` is (`AuthRequestLimit` per `AuthWindowLength`).
Both extensions are listed by `getOpenSubsonicExtensions`, which needs no sign-in, so an app can tell whether to offer
the browser sign-in.
