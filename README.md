# Crossbeam

Crossbeam is a packet-radio node and a bulletin bridge, by K5LEB. One article is stored once. It can be posted as a plain-text packet bulletin and as a NomadNet page, and a bulletin or a page can be read back into the same article.

A new operator starts with [docs/start.md](docs/start.md). That hour saves a callsign and one bulletin. Turning the radio on is [docs/graywolf.md](docs/graywolf.md). The rest of this page is the full system.

This is not the Rust concurrency library of the same name.

The repository is GPL-3.0-or-later. See `LICENSE`. Changes are listed in `CHANGELOG.md`.

## Install

The Debian package installs Crossbeam by itself. It does not upgrade the rest of the system.

```bash
sudo apt update
sudo apt install crossbeam
```

A newer Crossbeam can be installed by itself:

```bash
sudo apt update
sudo apt install --only-upgrade crossbeam
sudo systemctl restart crossbeam
```

`sudo apt upgrade` upgrades Crossbeam and every other pending package. The package does not restart the service. The signing key and the rest of the setup are in `docs/install.md`.

## MCP

The package includes a local MCP server. It uses standard input and standard output. It does not open a network port. On a machine where the service is installed, start it as the user who owns the state directory:

```bash
sudo -u crossbeam crossbeam mcp --state /var/lib/crossbeam --socket /run/crossbeam/crossbeam.sock
```

It can:

- List stored articles, without their bodies.
- Read one article, including its body.
- Show the packet bulletin and the NomadNet page for an article before it is saved.
- Save a new article, or update an existing one.
- Delete one article.
- Write the stored articles into the NomadNet pages directory.
- Read NomadNet pages back into the article store.
- Report the callsign, the program version, and whether pages, the packet link, the radio, telnet, and bulletin forwarding are turned on.
- Report the panel address and the certificate fingerprint. With the socket above, a running service reports the address it opened.

It does not change settings, dial a station, or send a bulletin. The operating system user who starts it is the only access control. Details are in `docs/mcp.md`.

## AI assistant

A caller on the station prompt can type `AI` and chat with a model on a computer on your own network. Crossbeam talks to Ollama directly, and an OpenAI-compatible server such as Open WebUI also works. It is off until `ai.enabled` is turned on. By default only callers heard over the radio, whose licenses the FCC lists, may use it. Replies are short, plain text, paged for 1200 baud, filtered for Part 97, and logged for the control operator. `/EXIT` returns to the prompt, and `BYE` disconnects. MeshCore direct messages and Reticulum LXMF messages that start with `AI` can be answered too, each with its own switch, and those senders need sysop approval first.

```bash
ollama pull llama3.2:3b
sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
```

The defaults are Ollama on this computer at `http://127.0.0.1:11434` and the model `llama3.2:3b`. Set `ai.url` for Ollama on another computer. Setup for Ollama, Open WebUI, and the Reticulum bridge, the safeguards, and the limits are in `docs/ai.md`.

## This build

Version 0.10.3 is the admin panel, the article store, an AX/UDP link, ASCII FBB bulletins, NomadNet pages, NET/ROM routing, a KISS radio, telnet, and FBB TCP. The settings page can choose a plugged-in radio and set its frequency and mode. Each setting has a plain name, an example in an empty box, and a longer explanation beside the box. It serves HTTPS on a LAN address, with one sysop login and no default password. The same settings can be changed in the browser and with the `crossbeam` command. Compose shows a packet bulletin and a Micron page for one article. When the link is configured, `crossbeam connect` reaches another node's prompt over UDP port 10093, and `crossbeam bulletin send` publishes stored articles and ingests the peer's bulletins. A listed station is dialed directly. A station in `telnet.peers` is dialed over telnet. A station that a listed neighbor has announced is reached over NET/ROM, across more than one hop. When `kiss.tnc` is set, a station with no listing and no route is dialed over KISS at 1200 baud. `crossbeam bulletin send` uses FBB TCP when the station is in `fbb.peers` and not in `link.peers`. `crossbeam page publish` writes each stored article into a NomadNet pages directory, and `crossbeam page ingest` reads a page back. The article stays in the store when that directory is unavailable. `crossbeam mcp` serves that store and those pages over standard input and output. It can preview an article and report the panel address. It does not open a network port. When `ai.enabled` is on, a caller can chat with a local AI model at the station prompt. MeshCore and Reticulum messages can reach it too, after the sysop approves each sender. The Debian package is published in the apt archive and installs `/usr/bin/crossbeam`. It does not enable the service.

It does not modulate audio, and it does not start NomadNet or Graywolf. When `mesh.device` is set, a MeshCore companion radio announces the station callsign. A MeshCore user messages it like any other contact, and `LIST` and `READ` use the same article store. The article format is `docs/article.md`. The link is `docs/link.md`. Routing is `docs/netrom.md`. The radio is `docs/radio.md`. Bulletins are `docs/bulletin.md`. Telnet and FBB TCP are `docs/peers.md`. Pages are `docs/pages.md`. MeshCore is `docs/meshcore.md`. MCP is `docs/mcp.md`. The AI assistant is `docs/ai.md`. Install steps are `docs/install.md`. Settings are `docs/settings.md`.

```bash
go test ./...
go run ./cmd/crossbeam version
```

## Radio

The radio modem is [Graywolf](https://github.com/chrissnell/graywolf), on 1200 baud AFSK, over plain KISS. 9600 baud is not a supported mode. Crossbeam does not modulate audio and does not start Graywolf. Hamlib sets the frequency and mode of a plugged-in radio. Graywolf still sends the audio and keys the transmitter. The radio stays quiet until the FCC lists both stations as active amateur licenses. The first hour with Graywolf is `docs/graywolf.md`. The KISS port is `docs/radio.md`.

## Status

The plan is `docs/PLAN.md`. The article format is `internal/article`. The admin program is `internal/admin`, with settings in `internal/settings` and state in `internal/store`. The manual service file is `deploy/crossbeam.service`. The apt archive is `apt/`. `docs/install.md` adds that archive. Cloning the repository does not install the program.
