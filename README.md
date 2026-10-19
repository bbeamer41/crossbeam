# Crossbeam

Crossbeam is a packet-radio node and a bulletin bridge, by K5LEB. One article is stored once. It can be posted as a plain-text packet bulletin and as a NomadNet page, and a bulletin or a page can be read back into the same article.

This is not the Rust concurrency library of the same name.

The repository is GPL-3.0-or-later. See `LICENSE`.

## This build

Version 0.2.0 is the admin panel and the article store. It serves HTTPS on a LAN address, with one sysop login and no default password. The same settings can be changed in the browser and with the `crossbeam` command. Compose shows a packet bulletin and a Micron page for one article.

It does not open a radio, speak AX.25, or start NomadNet. The article format is `docs/article.md`. Install steps are `docs/install.md`. Settings are `docs/settings.md`.

```bash
go test ./...
go run ./cmd/crossbeam version
```

## Radio

The radio modem is [Graywolf](https://github.com/chrissnell/graywolf), on 1200 baud AFSK, attached later over KISS. 9600 baud is not a supported mode. Crossbeam does not modulate audio.

## Status

The plan is `docs/PLAN.md`. The article format is `internal/article`. The admin program is `internal/admin`, with settings in `internal/settings` and state in `internal/store`. The service file is `deploy/crossbeam.service`. It is not installed by cloning the repository.
