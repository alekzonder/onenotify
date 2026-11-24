---
title: mail module
sidebar:
  label: mail
---

Sends a message as a plain text e-mail through an SMTP server.

```yaml
channels:
  - name: admin-mail
    module: mail
    params:
      host: smtp.example.com
      port: 587
      tls: starttls
      username: alerts@example.com
      password: "${SMTP_PASSWORD}"
      from: "Alerts <alerts@example.com>"
      to:
        - admin@example.com
```

## Params

| Key | Required | Default | Description |
|---|---|---|---|
| `host` | yes | | host name or address of the SMTP server |
| `port` | no | by `tls`: `587` for `starttls`, `465` for `tls`, `25` for `none` | port of the SMTP server |
| `tls` | no | `starttls` | `starttls`: connect in plain text and upgrade with STARTTLS; `tls`: TLS from the first byte; `none`: plain text |
| `username` | no | | user name; the module authenticates only when it is set |
| `password` | no | | password; requires `username` |
| `from` | yes | | sender address, plain (`alerts@example.com`) or with a name (`Alerts <alerts@example.com>`) |
| `to` | yes | | list of recipient addresses, at least one |
| `helo` | no | the system host name | the name the module introduces itself with (EHLO) |
| `insecure_skip_verify` | no | `false` | do not verify the server's TLS certificate |

Pass the password through the environment, see
[Configuration](/configuration#environment-variables-in-params).

## How a notification looks

The title is the subject, the text is the body. A mail as the server
receives it, with the body shown decoded:

```
From: "Alerts" <alerts@example.com>
To: <admin@example.com>
Subject: [web-01] FAIL main/api-health
Date: Tue, 24 Nov 2026 08:13:29 +0000
Message-ID: <20261124T081102Z-main-api-health-fail@example.com>
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable
X-Onenotify-Level: error
X-Onenotify-Host: web-01

module: http
headers.Authorization: Bearer ${API_TOKEN}
url: https://example.com/health
tags: api, web
at: 2026-11-24 08:11:02 UTC
failed 3 runs in a row:
- body json status: expected equals "ok", got "degraded"
- status: expected equals 200, got 503
```

- The mail is `text/plain` in UTF-8. There is no HTML part and there are no
  attachments.
- The level does not change the subject or the body. It is in the header
  `X-Onenotify-Level` (`error`, `ok`, `warning`, `info`), and the host name
  is in `X-Onenotify-Host`: use them in mail filters. The title already says
  `FAIL` or `RECOVERED` for onecheck events.
- `Message-ID` is built from the message id and the domain of `from`, so a
  message that happens to be delivered twice has the same `Message-ID` both
  times.
- `Date` is the time of sending, not the time of the event; the time of the
  event is the `at:` line of the text.
- A title with non-ASCII characters is encoded as the standards require
  (RFC 2047). A title cannot add headers of its own: line breaks in it become
  spaces.
- A message without text is a mail with an empty body.

All recipients of `to` get one mail, sent in one SMTP conversation.

## Limits

The module does not shorten anything: a mail carries the whole text. The
text of `onenotify send` is bounded by `limits.text` (`64KB` by default), the
text of a onecheck event by the [limits of a message](/messages#limits).

## Connection and security

- With `tls: starttls` a server that does not offer STARTTLS gets nothing:
  the module never falls back to plain text.
- With `starttls` and `tls` the server's certificate is checked against
  `host`; TLS 1.2 is the minimum. `insecure_skip_verify: true` turns the
  check off, for a server with a self-signed certificate.
- Credentials are sent only over an encrypted connection, unless you chose
  `tls: none` yourself. The module uses `AUTH PLAIN` when the server offers
  it and `AUTH LOGIN` otherwise.
- The whole conversation is limited by the channel's `timeout`.

## Errors

| What happened | Outcome |
|---|---|
| `552` in reply to `MAIL FROM`, `DATA` or the message itself (message too large) | **permanent**: the server rejected this very message; it goes to the failed store |
| any other SMTP reply: `4xx`, authentication failed, sender or recipient rejected, rejected by policy | temporary |
| no connection, DNS, TLS handshake or certificate error, no STARTTLS, timeout | temporary |

Temporary means the message stays in the queue and is tried again by the next
run, see [Queue and delivery](/queue-and-delivery#temporary-and-permanent-errors).
A rejected recipient is temporary on purpose: you fix the address and the
waiting messages go out. If one of several recipients is rejected, nobody
gets the mail: a partial delivery would be repeated for the others on every
retry.

Error texts name the step of the conversation and quote the server:

```
mail: connect: dial tcp 10.0.0.5:587: connect: connection refused
mail: STARTTLS: the server does not offer STARTTLS; nothing was sent
mail: AUTH: the server does not offer authentication
mail: AUTH: 535 5.7.8 Error: authentication failed
mail: RCPT TO <admin@example.com>: 550 5.1.1 <admin@example.com>: Recipient address rejected: User unknown
mail: end of DATA: 552 5.3.4 Message size exceeds fixed limit
```

The password never appears in an error text.

## Setup hints

| Server | Params |
|---|---|
| a mail provider, submission port | `port: 587`, `tls: starttls` (the defaults), `username`, `password` |
| a mail provider, "SSL" port | `port: 465`, `tls: tls`, `username`, `password` |
| the local mail server of the host | `host: 127.0.0.1`, `tls: none` (port `25`), no `username` |

- Providers with two-factor authentication (Gmail, Yandex, Fastmail, ...) do
  not accept the account password over SMTP. Create an **app password** in
  the account's security settings and use it as `password`.
- Most providers require `from` to be the address of the authenticated
  account, or one of its aliases.
- A local mail server on port 25 usually wants neither TLS nor
  authentication:

  ```yaml
  params:
    host: 127.0.0.1
    tls: none
    from: onenotify@web-01.example.com
    to: [root@example.com]
  ```

Try the channel:

```sh
onenotify send --title test --channel admin-mail
```
