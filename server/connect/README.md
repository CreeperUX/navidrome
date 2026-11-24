# Sonus Connect

A user's players see and control each other, as with Spotify Connect: which device plays what, play / pause / seek /
skip / volume on any of them, and moving playback from one device to another. This fork of Navidrome
([CreeperUX/navidrome](https://github.com/CreeperUX/navidrome)) adds it; everything else is upstream Navidrome. It was
made for Sonus, a Windows player; any client can implement it from this page.

- Advertised as the OpenSubsonic extension `sonusConnect`, version 1 (`getOpenSubsonicExtensions`). A client
  that doesn't find it doesn't offer the feature, so it works with any other server.
- Nothing is stored: who is connected and what they play is kept in memory. The database is upstream's, untouched,
  so switching back to upstream Navidrome needs nothing.
- Upstream files touched: `server/subsonic/api.go` (the route) and `server/subsonic/opensubsonic.go` (the extension).
  Everything else is in this folder.

## The Docker image

`.github/workflows/creeperux-image.yml` builds this branch with upstream's Dockerfile into
`ghcr.io/creeperux/navidrome` (linux/amd64, linux/arm64) on every push and every Monday: tags `latest`, `0.64.2-sonus.<n>`
(`0.64.2-sonus.9`; SONUS.md) and `sha-<commit>`. In a compose file, only the image changes; the data folder stays as it is:

```yaml
services:
  navidrome:
    image: ghcr.io/creeperux/navidrome:latest   # was deluan/navidrome:latest
```

Going back to upstream is changing the image back: nothing here is stored in the database.

## Connecting

A WebSocket at `rest/connect` (or `rest/connect.view`), authenticated like every Subsonic request: `u` with `t` and
`s` (or `p`), `v` and `c`, in the query string. The device is the player Navidrome registers for the request
(Settings › Players in the web UI): its id is the player's, its first name the player's name.

```
wss://music.example.com/rest/connect.view?u=alice&t=…&s=…&v=1.16.1&c=Sonus&f=json
```

The server pings every 30 s (clients' WebSockets answer by themselves); a connection silent for 75 s is closed. A
second connection of the same player replaces the first (close code 4000). A user may have 32 devices connected; the
oldest goes when another arrives.

## Messages

JSON text frames, each with a `type`. Times are milliseconds since the Unix epoch on the server's clock.

### Server → client

| type | fields | when |
|---|---|---|
| `welcome` | `version`, `deviceId` (this connection's), `serverTime` | first, on connecting |
| `devices` | `serverTime`, `devices`: [Device] | on connecting and whenever any of the user's devices joins, leaves, says hello or reports a state |
| `command` | `from` (device id), `command`: Command | another device asked this one to do something |
| `error` | `code` (`offline`, `nothing`), `message` | a command or transfer could not be delivered |

A **Device** is `{ id, name, kind, client, canPlay, connectedAt, state? }`. `kind` is `desktop`, `web`, `phone`,
`speaker` or `other`.

A **State** is `{ trackId, title, artist, album, coverArt, durationMs, positionMs, playing, volume?, index, count,
context?: { kind, id, label }, at }`. `at` is when the server received it; while `playing`, the position now is
`positionMs + (serverTime − at)` (a client keeps the offset between `serverTime` and its own clock).

### Client → server

| type | fields | effect |
|---|---|---|
| `hello` | `name`, `kind`, `canPlay` (all optional) | names the device as the others see it |
| `state` | `state`: State (without `at`), `queue?`: `{ ids, index }` | what this device plays; send on every change (song, play / pause, seek, volume), not on every tick. The queue only when it changes: the server keeps it for transfers and does not broadcast it |
| `command` | `target` (device id), `command`: Command | passed to that device (`error` `offline` if it isn't connected) |
| `transfer` | `from` (default: this device), `to`, `play?` | `to` gets a `load` command with `from`'s queue, index, context and position now (playing if `from` was, or as `play` says); `from` gets `stop` |

A **Command** is `{ action, … }`:

| action | fields |
|---|---|
| `play`, `pause`, `toggle`, `next`, `previous`, `stop` | — |
| `seek` | `positionMs` |
| `volume` | `volume` (0–1) |
| `load` | `queue`: `{ ids, index }`, `positionMs`, `play`, `context?`, `source?` (set by a transfer: the device the playback came from) |

## Tests

`go test ./server/connect/` (no database involved: runs with `CGO_ENABLED=0` too).
