# Crossbeam

Crossbeam is a bulletin board for an amateur radio station, by K5LEB. You write a note once. A station that connects can read it. The same note can later become a NomadNet page.

A Technician license is enough to begin. You can have a working board on this computer before a radio is plugged in.

This is not the Rust library of the same name. The license is GPL-3.0-or-later. See `LICENSE`. Changes are listed in `CHANGELOG.md`. This page describes version 0.10.4.

## Start here

1. [Install Crossbeam](docs/install.md) and open the panel.
2. [Save your callsign and write one bulletin](docs/start.md).
3. [Turn the radio on](docs/graywolf.md) when you are ready. The supported modem is Graywolf, at 1200 baud.

A station that connects types `?`, then `LIST`, then `READ 1`, then `BYE`.

Examples in these pages use `N0CALL` as a placeholder. Type the callsign on your license.

The full list of guides is [docs/README.md](docs/README.md).

## Install

The Debian package installs Crossbeam and Graywolf on a 64-bit PC or on 64-bit Raspberry Pi OS. It does not start Crossbeam. Graywolf's service is enabled and stays stopped until you start it. Run these commands on each computer before the first install. The password step is in [docs/install.md](docs/install.md).

```bash
sudo install -d -m 0755 /etc/apt/keyrings
wget -O /tmp/crossbeam.asc https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt/crossbeam-archive-key.asc
wget -O /tmp/crossbeam.list https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt/crossbeam.list
gpg --show-keys /tmp/crossbeam.asc
sudo install -m 0644 /tmp/crossbeam.asc /etc/apt/keyrings/crossbeam.asc
sudo install -m 0644 /tmp/crossbeam.list /etc/apt/sources.list.d/crossbeam.list
sudo apt update
sudo apt install crossbeam
```

The fingerprint `gpg` prints is `E6B7A9B54B63A15E37C0979FC4B7FFE1AB0D6034`.

On a computer that already has the archive:

```bash
sudo apt update
sudo apt install --only-upgrade crossbeam
sudo systemctl restart crossbeam
```

That upgrade also installs Graywolf when it is not already installed. `sudo apt upgrade` also upgrades every other pending package. The package does not restart Crossbeam, so restart it yourself after that command.

## What you can add later

Every one of these stays off while its setting is empty. Saving a callsign does not turn them on.

| When you want it | Where to read |
| --- | --- |
| The panel, the password, and the first commands | [docs/install.md](docs/install.md), [docs/start.md](docs/start.md) |
| Graywolf on 1200 baud | [docs/graywolf.md](docs/graywolf.md), then [docs/radio.md](docs/radio.md) |
| A direct connection to one other node | [docs/link.md](docs/link.md) |
| Callers who use a telnet program | [docs/peers.md](docs/peers.md) |
| Sending the bulletins to another BBS | [docs/bulletin.md](docs/bulletin.md) |
| A route through a neighbor station | [docs/netrom.md](docs/netrom.md) |
| The same bulletin as a NomadNet page | [docs/pages.md](docs/pages.md) |
| A MeshCore radio that answers LIST and READ | [docs/meshcore.md](docs/meshcore.md) |
| An AI assistant on the station prompt | [docs/ai.md](docs/ai.md) |
| Every setting, with its explanation | [docs/settings.md](docs/settings.md) |

## Words on this page

| Word | Meaning |
| --- | --- |
| Callsign | The address on your amateur license. It is the address of this station. |
| Bulletin | A note stored on the board. |
| Prompt | The line a connected station sees. It ends with `>`. |
| Sysop | The operator who signs in to the panel. |
| Modem | The program that turns text into packet tones. Graywolf is the supported one. |
| KISS | The plain connection between Crossbeam and that modem. |
| AX.25 | The packet connection a station uses to reach the prompt. |

## What this version includes

The panel is HTTPS, on an address of this computer, with one sysop login and no default password. The same settings can be changed in the browser and with the `crossbeam` command. An empty box leaves that part off. Faint text in a box is an example. The i beside a box explains it.

Compose shows the packet bulletin and the NomadNet page for one article before you save it.

| Piece | What it does |
| --- | --- |
| Article store | One copy of each note. The format is [docs/article.md](docs/article.md). |
| AX/UDP link | `crossbeam connect` reaches another node's prompt. Port 10093 is the usual choice. A listed station is dialed directly. |
| Telnet | A station in `telnet.peers` is dialed over telnet. |
| NET/ROM | A station a listed neighbor has announced is reached across more than one hop. |
| KISS radio | When `kiss.tnc` is set, a station with no listing and no route is dialed at 1200 baud. |
| FBB bulletins | `crossbeam bulletin send` publishes stored articles and takes the peer's bulletins. It uses FBB TCP when the station is in `fbb.peers` and not in `link.peers`. |
| NomadNet pages | `crossbeam page publish` writes each article into a pages directory. `crossbeam page ingest` reads a page back. The article stays in the store when that directory is unavailable. |
| MeshCore | When `mesh.device` is set, a companion radio announces the station callsign. A MeshCore user messages it like any other contact. `LIST` and `READ` use the same store. |
| AI assistant | When `ai.enabled` is on, a caller types `AI` at the prompt and chats with a model on your own network. MeshCore and Reticulum can reach it after you approve each sender. |
| MCP | `crossbeam mcp` serves the store over standard input and output. It can preview an article and report the panel address. It does not open a network port. |
| Radio dial | The settings page can choose a plugged-in radio and set its frequency and mode. Hamlib does that. Graywolf still sends the audio and keys the transmitter. |

Crossbeam does not modulate audio, and it does not start NomadNet or Graywolf. The package installs Graywolf beside it. The radio stays quiet until the FCC lists both stations as active amateur licenses. 9600 baud is not a supported mode. The package installs `/usr/bin/crossbeam` and does not enable the Crossbeam service.

## AI assistant

A caller types `AI` and chats with Ollama, or with an OpenAI-compatible server such as Open WebUI. It is off until you turn `ai.enabled` on. By default only callers heard over the radio, whose licenses the FCC lists, may use it. Replies are short, plain text, paged for 1200 baud, filtered for Part 97, and logged for the control operator. `/EXIT` returns to the prompt. `BYE` disconnects.

```bash
ollama pull llama3.2:3b
sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
```

The defaults are Ollama on this computer at `http://127.0.0.1:11434` and the model `llama3.2:3b`. Setup, safeguards, and limits are in [docs/ai.md](docs/ai.md).

## MCP

The package includes a local MCP server. It uses standard input and standard output. On a machine where the service is installed:

```bash
sudo -u crossbeam crossbeam mcp --state /var/lib/crossbeam --socket /run/crossbeam/crossbeam.sock
```

It can list and read articles, preview the packet bulletin and the NomadNet page, save or delete an article, publish and ingest pages, and report the callsign, the version, which parts are on, and the panel address. It does not change settings, dial a station, or send a bulletin. The operating system user who starts it is the only access control. Details are in [docs/mcp.md](docs/mcp.md).

## From a checkout

Cloning the repository does not install the program. The apt archive is `apt/`. [docs/install.md](docs/install.md) adds that archive. The plan is [docs/PLAN.md](docs/PLAN.md). The manual service file is `deploy/crossbeam.service`.

```bash
go test ./...
go run ./cmd/crossbeam version
```
