# Changelog

## 0.10.0

- An AI assistant on the station prompt. A connected packet caller types `AI` to chat with a model on Ollama, which Crossbeam talks to directly, or on an OpenAI-compatible server such as Open WebUI. The defaults are Ollama on this computer and `llama3.2:3b`. `/EXIT` returns to the prompt, and `BYE` signs off and disconnects. Each chat keeps its last six turns. Replies are plain ASCII, cut to `ai.reply` characters, and paged for 1200 baud. A dropped link or `/STOP` cancels a question.
- The assistant is off by default. It is open to radio callers whose license was checked, or to the callsigns in `ai.users`. It is limited per caller per hour, filtered for profanity and encoded text, and every question and reply is logged to `ai-log.jsonl` for the control operator.
- `ai.mesh` answers a MeshCore direct message that starts with `AI`, with a short history for each contact.
- `ai.reticulum` answers an LXMF message that starts with `AI`, through the separate Crossbeam LXMF bridge in `contrib/reticulum`. Crossbeam itself still does not speak Reticulum. Each sender keeps a short history.
- `crossbeam ai key`, `crossbeam ai test`, and `crossbeam ai log` save the API key, check the server, and show the log. `ai test` says what to fix when Ollama cannot be reached or the model is not pulled. See `docs/ai.md`.

## 0.9.18

- Closing the packet radio after a failed license check no longer races the KISS connection.
- The install guide covers a backup of `/var/lib/crossbeam` and adding operators back to the `crossbeam` group after a purge. A Crossbeam 0.9.2 plain checksum is not accepted.

## 0.9.17

- A MeshCore companion radio announces the station callsign. A MeshCore user messages it like any other contact. LIST and READ use the same bulletin list, and a plain sentence gets a short reply in that conversation.
- A packet-radio transmission is sent only when the FCC lists both stations as active amateur licenses. That check runs again for each radio session.

## 0.9.16

- The MCP server can preview an article without saving it, and it can report the panel address and the certificate fingerprint.

## 0.9.15

- Each setting on the panel has a plain name, an example in an empty box, and a longer explanation behind the i beside the box.

## 0.9.14

- The admin panel uses the Texas Packet Radio Society mark, colors, and type. The footer says the panel is presented by the society.

## 0.9.13

- Logging out returns to the sign-in page when the browser has not trusted the certificate.

## 0.9.12

- The bulletin board is at `/bulletins`. The old `/articles` address opens that page.

## 0.9.11

- Signing in, and opening the panel, shows the bulletin board.

## 0.9.10

- The bulletin list is a board of cards. A card opens that bulletin.

## 0.9.9

- Installing the package again gives an existing state directory, including the database and the TLS key, to the new `crossbeam` user. Links are not followed.
- `article list`, `status`, `mcp`, and `page publish` and `page ingest` do not create a missing state directory.
- `connect` and `bulletin send` say permission denied when the terminal socket cannot be opened.
- `docs/install.md` checks the signing key before the key is installed.

## 0.9.8

- The settings page can choose a plugged-in radio, its USB port, a frequency, and a mode. Hamlib sets the radio. Graywolf still sends the audio and keys the transmitter.

## 0.9.7

- A Unix socket is mode 0600 before its name is published. The mode can then widen to 0660 for the crossbeam group.
- The systemd service hides devices, kernel interfaces, and extra address families. It keeps the network and NETLINK.
- An unmarked stream cannot forward bulletins. Login is the only path that creates an admin session.
- An AX.25 SABM that the engine refuses does not keep that callsign off the radio. A later SABM does not extend the first hold.

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
