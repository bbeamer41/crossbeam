# Crossbeam

Crossbeam is a packet-radio node and a bulletin bridge, by K5LEB. One article is stored once. It can be posted as a plain-text packet bulletin and as a NomadNet page, and a bulletin or a page can be read back into the same article.

This is not the Rust concurrency library of the same name.

The repository is GPL-3.0-or-later. See `LICENSE`.

## This build

Version 0.1.0 is the article format only. It does not open a radio, speak AX.25, serve a web page, or start NomadNet. Those come in later goals. The format is `docs/article.md`. The tests are in `internal/article`.

```bash
go test ./...
go run ./cmd/crossbeam version
```

## Radio

The radio modem is [Graywolf](https://github.com/chrissnell/graywolf), on 1200 baud AFSK, attached later over KISS. 9600 baud is not a supported mode. Crossbeam does not modulate audio.

## Status

The plan is `docs/PLAN.md`. The first slice, the article format, is the code in `internal/article`.
