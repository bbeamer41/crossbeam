# Changelog

## 0.9.6

- The forward log names the bulletin id and author of each bulletin this station stores and each bulletin the peer accepts. A refusal names the sender from the proposal, including when the station id arrived first.
- The station that runs `crossbeam bulletin send` logs that forward and a failure to connect.
- `settings get` and `settings` do not create a missing state directory.
- Purging the Debian package removes the `crossbeam` user and group. The state directory stays.
- `docs/install.md` shows `gpg --show-keys` for the archive signing key.

## 0.9.5

Review fixes for forwarding, link sessions, sign-in, and the package check. The binary is built from a clean source commit.

## 0.9.4

A learned UDP return port lasts only for that AX.25 session. Forward logs name a bad checksum, a refusal, and a listed callsign from the wrong address.

## 0.9.3

Bulletin forwarding is accepted only from a listed peer. Command flags may follow the arguments.

## 0.9.2

Admin panel layout.

## 0.9.1

A browser that has not trusted the self-signed certificate can sign in.

## 0.9.0

Local MCP server and the signed apt archive.

## 0.8.0

Station prompt over telnet. Bulletins over FBB TCP.

## 0.7.0

Connected AX.25 over a KISS radio at 1200 baud.

## 0.6.0

NET/ROM routing across more than one hop.

## 0.5.0

NomadNet pages.

## 0.4.0

ASCII FBB bulletin forwarding.

## 0.3.0

AX/UDP link on UDP 10093.

## 0.2.0

Admin panel, shared settings, and the article store.
