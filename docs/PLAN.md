# Crossbeam plan

Crossbeam is one installable program for Debian. It is a packet node that can talk to LinBPQ and older BBSes, and it is a bridge so one article is posted to packet radio and to NomadNet.

Packet.NET is not part of this project, and its source is not used. LinBPQ source is not used. Behavior for AX.25, NET/ROM, and FBB comes from the published descriptions and from tests against a running LinBPQ.

## License

GPL-3.0-or-later. The license file is `LICENSE`. Copyright is held by K5LEB.

NomadNet is a separate GPL-3.0 program. Reticulum stays in that separate program. Crossbeam writes and reads pages. It does not copy Reticulum into the Crossbeam binary.

On amateur packet, articles travel as readable text. The bridge does not put Reticulum ciphertext on AX.25.

## Radio

Graywolf is the only supported modem. The mode is 1200 baud AFSK. Crossbeam will speak KISS to Graywolf and will own the node, the mailbox, and the bridge.

9600 baud is not a supported mode. There is no 9600 port profile, test, or setup screen. The KISS port still does not contain a modem, so a different TNC can be attached later without rewriting the node. Until that exists, the documented radio path is Graywolf at 1200 baud.

APRS stays in Graywolf and is outside Crossbeam.

## Shape

The program is Go. One binary. SQLite holds articles, settings, and the sysop session. The browser and the `crossbeam` command change the same settings. Admin access is HTTPS on the LAN, with one login and no default password. The local command can also use a Unix socket.

The article is the center. The canonical fields and the two renderings are specified in `docs/article.md` and implemented in `internal/article`.

## Status

Goals 1, 2, 3, 4, and 5 are in this tree. Goal 6 has not been started.

## Goals

Each goal starts after the one before it is demonstrated.

1. **Article bridge, on disk.** License, README, Go module. One article renders to a wrapped packet bulletin and to a Micron page, and both renderings import back to the same article. Tests cover that. No radio and no NomadNet process.
2. **Admin program.** A Debian service. HTTPS on the LAN, one sysop login, and the same settings from the browser and the command. A compose screen shows both previews.
3. **LinBPQ link.** AX/UDP on UDP 10093. Each node lists the other, and a terminal on each side reaches the other node's prompt.
4. **Packet bulletins.** Publish an article as an FBB bulletin to LinBPQ, and ingest a LinBPQ bulletin into the article store.
5. **NomadNet pages.** Publish the same article as a Micron page to a local NomadNet, and ingest a NomadNet page back. An article waits on disk if NomadNet or the internet is down. A page Crossbeam writes is a normal file, not an executable, so NomadNet does not run it.
6. **NET/ROM routing.** Quality, node broadcasts, and a connection across more than one hop.
7. **Radio.** A KISS port aimed at Graywolf on 1200 baud AFSK. Graywolf's digipeater and iGate stay off on that channel.
8. **Remaining peers.** Telnet and FBB TCP, BPQ chat linking, and a Winlink RMS gateway.
9. **MCP, the Debian package, and a Pi 3 install.** Record RAM for the node alone and beside Graywolf.
