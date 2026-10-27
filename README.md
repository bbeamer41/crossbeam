# Crossbeam

Crossbeam is a packet-radio node and a bulletin bridge, by K5LEB. One article is stored once. It can be posted as a plain-text packet bulletin and as a NomadNet page, and a bulletin or a page can be read back into the same article.

This is not the Rust concurrency library of the same name.

The repository is GPL-3.0-or-later. See `LICENSE`.

## This build

Version 0.9.0 is the admin panel, the article store, an AX/UDP link, ASCII FBB bulletins, NomadNet pages, NET/ROM routing, a KISS radio, telnet, and FBB TCP. It serves HTTPS on a LAN address, with one sysop login and no default password. The same settings can be changed in the browser and with the `crossbeam` command. Compose shows a packet bulletin and a Micron page for one article. When the link is configured, `crossbeam connect` reaches another node's prompt over UDP port 10093, and `crossbeam bulletin send` publishes stored articles and ingests the peer's bulletins. A listed station is dialed directly. A station in `telnet.peers` is dialed over telnet. A station that a listed neighbor has announced is reached over NET/ROM, across more than one hop. When `kiss.tnc` is set, a station with no listing and no route is dialed over KISS at 1200 baud. `crossbeam bulletin send` uses FBB TCP when the station is in `fbb.peers` and not in `link.peers`. `crossbeam page publish` writes each stored article into a NomadNet pages directory, and `crossbeam page ingest` reads a page back. The article stays in the store when that directory is unavailable. `crossbeam mcp` serves that store and those pages over standard input and output. It does not open a network port. The Debian package is published in the apt archive and installs `/usr/bin/crossbeam`. It does not enable the service.

It does not modulate audio, and it does not start NomadNet or Graywolf. The article format is `docs/article.md`. The link is `docs/link.md`. Routing is `docs/netrom.md`. The radio is `docs/radio.md`. Bulletins are `docs/bulletin.md`. Telnet and FBB TCP are `docs/peers.md`. Pages are `docs/pages.md`. MCP is `docs/mcp.md`. Install steps are `docs/install.md`. Settings are `docs/settings.md`.

```bash
go test ./...
go run ./cmd/crossbeam version
```

## Radio

The radio modem is [Graywolf](https://github.com/chrissnell/graywolf), on 1200 baud AFSK, over plain KISS. 9600 baud is not a supported mode. Crossbeam does not modulate audio and does not start Graywolf. Setup is `docs/radio.md`.

## Status

The plan is `docs/PLAN.md`. The article format is `internal/article`. The admin program is `internal/admin`, with settings in `internal/settings` and state in `internal/store`. The manual service file is `deploy/crossbeam.service`. The apt archive is `apt/`. `docs/install.md` adds that archive. Cloning the repository does not install the program.
