# d75winlink page protocol

How `web/winlink` talks to `winlinkweb`. Protocol version 1. Everything is
same-origin on localhost, behind the guard every page server here shares
(`localweb`): loopback Host only, `Sec-Fetch-Site` and `Origin` must say
same origin, a strict Content-Security-Policy. POST bodies must be
`application/json`, so a plain form post from another site is refused.

## Event stream: `GET /events`

Server-sent events. Every event's data is one line of JSON.

| Event | Data | When |
| --- | --- | --- |
| `hello` | `{info, snapshot}` | First, on every connect and reconnect |
| `mailbox` | `{mailbox, counts}` | Any change to the mailbox |
| `session` | session state | A session starts, changes state or ends |
| `log` | log line | Each session log line |
| `progress` | progress | Each block of a message sent or received |

A page that falls 512 events behind is dropped; it reconnects and starts
again from `hello`. A comment line every 15 s keeps the stream open.

**info**: `protocol`, `version`, `myCall`, `demo`, `rfTransmit`,
`maxAttachments`, `maxMessage` (bytes).

**snapshot**: `mailbox` (summaries, newest first), `counts`, `session`,
`log` (the last 400 lines of the current or last session), `routes`
(`[{id, label}]`).

**summary**: `mid`, `folder` (`in`, `out`, `sent`, `archive`), `from`,
`to`, `cc`, `subject`, `date` (RFC 3339; the zero time when the message has
none), `size` (bytes as sent), `compressed` (outbox only: bytes on the air),
`files` (count), `read`, `broken` (received but would not parse).

**counts**: `in`, `out`, `sent`, `archive`, `unread`.

**session**: `state` (`idle`, `connecting`, `exchanging`, `done`, `failed`,
`stopped`), `route`, `started`, `ended`, `detail` (the error, or what was
moved), `stats` (`Sent`, `Rejected`, `Received`, `Deferred`: lists of
message IDs), `progress`.

**log line**: `t`, `dir` (`tx`, `rx` or `info`), `text`. The secure login
answer is always `;PR: ********`.

**progress**: `mid`, `sending`, `done`, `total` (compressed bytes).

## Requests

Errors come back as `{"error": "…"}` with a 4xx or 5xx status.

| Request | Body | Answer |
| --- | --- | --- |
| `GET /api/message/{mid}` | | The summary plus `body`, `mbo`, `type`, `files` (`[{name, size}]`), and `raw` instead of `body` when the message would not parse. Opening an unread message marks it read. |
| `GET /api/message/{mid}/file/{n}` | | Attachment `n` as a download: `application/octet-stream`, `Content-Disposition: attachment` with the name cut to its last path element. Never shown in the page. |
| `POST /api/estimate` | draft | `{size, compressed, secs1200, secs9600, smtp}`, or 422 with what is wrong. Queues nothing. |
| `POST /api/queue` | draft | The new message's summary, in `out`; 422 when the draft is not valid. |
| `POST /api/move` | `{mid, to}` | `{}`. Allowed: `in`→`archive`, `sent`→`archive`, `archive`→`in`; others are 409. |
| `POST /api/delete` | `{mid}` | `{}`. Only from `out` (unsending) and `archive`, and not during a session (409). |
| `POST /api/connect` | `{route}` | `{}` once the session has started; 409 if one is running. |
| `POST /api/stop` | `{}` | `{}` at once; the session ends and a `session` event follows. |

**draft**: `to` and `cc` (addresses separated by commas, semicolons or
spaces: callsigns, `CALL@winlink.org` or internet addresses), `subject`,
`body`, `files` (`[{name, data}]`, data in base64). Limits: 120 kB of
attachments, 150 kB for the whole message, a subject, a body in ISO-8859-1.
The request may be at most the message limit as base64 plus 64 kB.

Drafts are kept by the page in `localStorage` (without attachments) until
they are queued or discarded; the server holds no drafts.
