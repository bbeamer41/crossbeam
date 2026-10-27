# MCP

`crossbeam mcp` serves the article store and the NomadNet pages over the Model Context Protocol. The session uses standard input and standard output. It does not open a network port, and it does not start NomadNet.

The operating system user who starts the process is the boundary. There is no password on this channel. The state directory is the `--state` flag. The default is `crossbeam-state`. With the service layout, that directory is mode `0700`, so the `crossbeam` user is the one that can open it.

```bash
crossbeam mcp --state /var/lib/crossbeam
```

Messages are JSON-RPC 2.0, one JSON object per line. Standard output is only those messages. A message larger than 2 MiB ends the session.

The protocol versions `2024-11-05`, `2025-03-26`, and `2025-06-18` are accepted. Any other version is answered with `2025-06-18`.

The tools read and write articles, publish pages, and ingest pages. They use the same store and the same page files as `crossbeam article` and `crossbeam page`. They do not change settings, dial a station, or send a bulletin.

`station` reports the callsign, the program version, and whether pages, the link, the radio, telnet, and FBB TCP are configured. A path counts as configured when its listen address or its peer list is set. The radio counts as configured when `kiss.tnc` is set. The report does not include an address, the pages directory, or the admin URL.

A tool that cannot finish returns a short sentence. The sentence does not include a filesystem path or a database message. A missing article is `article not found`.
