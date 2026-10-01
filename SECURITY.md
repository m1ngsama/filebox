# Security

Report a vulnerability privately through [GitHub's private vulnerability reporting](https://github.com/m1ngsama/filebox/security/advisories/new). Please do not open a public issue for it.

Include the filebox commit, how to reproduce it, and what an attacker gains. You will get a reply within a week, and a fix is released before the advisory is published.

## Scope

In scope:

- Reading, writing or deleting anything outside a share, a volume or a read-only app password's reach.
- Running script on the filebox origin, for example through an uploaded file, a preview or a book.
- Signing in, unlocking a share or keeping access without the password, passkey or token.
- Taking down the server or filling its disk through a public link.

Out of scope:

- Anything that needs the admin password or shell access to the server.
- Denial of service that needs more requests than the rate limits allow.
