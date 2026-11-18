# Settings

The admin panel and the `crossbeam` command change the same settings. A setting that is not in this list is rejected. The password is not a setting. It is stored only as a hash, and it is set with `crossbeam sysop password`.

The state directory is not a setting either. Pass it as `--state`. The database, the session list, and the TLS certificate live in that directory. The directory is mode `0700`. The database and the private key are mode `0600`.

A change to `admin.listen`, `admin.public`, `link.listen`, `link.peers`, `kiss.tnc`, `telnet.listen`, `telnet.peers`, `fbb.listen`, `fbb.peers`, or `mesh.device` is saved immediately and used the next time `crossbeam serve` starts. The running panel keeps the address it already bound. The running link keeps the peers, the TNC address, and the TCP ports it already resolved. The running MeshCore port stays the one opened at start.

A change to any `ai.` setting applies to the next AI question. The API key is not a setting. It is saved with `crossbeam ai key` in the file `ai-key` in the state directory, mode `0600`, and it is never shown on the panel, by `crossbeam settings`, or in a log.

A change to `radio.model`, `radio.device`, `radio.frequency`, or `radio.mode` is applied when it is saved. `crossbeam serve` applies it again at startup. An empty `radio.model` leaves the radio alone.

## station.callsign

Your amateur callsign. Compose uses it when the author box is empty. The packet link, the radio modem, telnet, and bulletin forwarding use it as the address of this station. When one of those is turned on, it has to be an AX.25 callsign, with an optional SSID such as K5LEB-1.

One line, at most 64 runes. It may be empty until you set it. When the link, the radio, telnet, and FBB TCP are off, the value is not checked against a callsign pattern. When `link.listen`, `kiss.tnc`, `telnet.listen`, or `fbb.listen` is set, it must be an AX.25 callsign, with an optional SSID such as `K5LEB-1`.

## station.name

A short name for this station. It is shown in the header of this panel so you can tell which node you are using.

One line, at most 80 runes. It may be empty.

## admin.listen

The address of this web page, written as host:port, such as 192.168.1.20:8443. Leave it empty and Crossbeam chooses a private network address on port 8443. A saved address is used the next time Crossbeam starts. This page keeps the address it already opened.

An empty value chooses the first private IPv4 address on a non-loopback interface, on port 8443. If the machine has none, the panel listens on `127.0.0.1:8443`. The automatic choice is never a public address and never `0.0.0.0` or `::`.

While `admin.public` is false, a saved address must be a loopback address, a private address, a link-local address, or an IPv6 local address. `localhost` is allowed. A hostname is saved without looking it up, so this works with no network. At start, the name is resolved once. Every address must be loopback, private, or link-local. The panel binds those addresses and does not look the name up again. The certificate includes the hostname and those addresses, so the browser can open either. One port is used for every address. If DNS fails, the panel does not start. An IP address is the reliable choice when the machine must boot with no DNS.

Port 0 is rejected. A zone id, such as `%eth0`, is rejected.

`--listen` on `crossbeam serve` binds this start to another address and does not change the saved setting. A public address still requires `admin.public`.

## admin.public

Allow this page to listen on a public internet address. Leave this off to keep the panel on your own network. The sysop password is still required, and the browser warns until the certificate is trusted.

The only values are `true` and `false`.

Leave this false unless you intend to put the admin panel on a public address. The panel still requires the sysop password, and the browser will warn until you trust the certificate.

## link.listen

The address other packet stations use to reach this one, written as host:port. Port 10093 is the usual choice. Leave it empty to keep the link off. Each UDP packet carries one AX.25 frame. A saved address is used the next time Crossbeam starts.

10093 is the usual port. The payload is one AX.25 frame, as described in `docs/link.md`. An empty value does not open a socket. A saved address may be a public address: this is the packet port, and `admin.public` does not apply to it. `0.0.0.0:10093` listens on every IPv4 interface. The bytes are readable text.

A hostname is saved without looking it up. At start it must resolve to one address. Port 0 is rejected. A zone id, such as `%eth0`, is rejected.

`--link` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## link.peers

Stations this node will call. Write each one as CALL@host:port, separated by spaces or commas, such as W1AW-1@192.168.1.20:10093. Bulletin forwarding on the packet link is accepted only from a station in this list. A station that is not listed can still reach the keyboard prompt. The list is read the next time Crossbeam starts.

Each callsign is stored in uppercase. SSID 0 is stored without a suffix. At most 32 stations. A hostname in an entry is resolved when the link starts, and it must resolve to one address. This node answers the prompt for a station that is not listed. Bulletin forwarding on AX/UDP is accepted only from a station in this list. The list is also who this node will call. While a session with that station is open, the reply goes to the address the request came from. The next call uses the port in this list again.

A listed station is also a NET/ROM neighbor. Routes learned from those neighbors stay in memory until `crossbeam serve` stops. They are described in `docs/netrom.md`.

`--peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## kiss.tnc

The address of the KISS modem, such as 127.0.0.1:6700 for a modem on this computer. Leave it empty to keep the radio off. Crossbeam sends 1200 baud packet to that modem. The modem turns the text into tones and keys the transmitter. The address is used the next time Crossbeam starts.

An empty value does not open a connection. The value is `host:port`. Port 0 is rejected. A zone id, such as `%eth0`, is rejected. A multicast address is rejected. `0.0.0.0` and `::` are rejected. This value is the address Crossbeam dials. An IP address is stored in canonical form. A hostname is saved without looking it up. At start it must resolve to one address. The radio does not send a connected frame until the FCC lists this station, and the station it is calling, as an active amateur license. The behavior of the port is described in `docs/radio.md`.

`--kiss` on `crossbeam serve` uses another address for this start and does not change the saved setting.

## radio.model

The radio plugged into this computer. Choose it from the list, or leave it off. Saving a choice sets the frequency and mode on that radio. The modem still sends the audio and keys the transmitter.

The settings page lists the radios reported by `rigctl -l`. Hamlib dummy radios are omitted. An empty value does not start the controller. A saved model opens the radio when the settings are saved and again when `crossbeam serve` starts. A failure is shown on the settings page. The rest of the node keeps running. Graywolf still sends the audio and keys the transmitter. The behavior is described in `docs/radio.md`.

## radio.device

The USB port of the radio. Choose the stable name from the list. That name lives under /dev/serial/by-id and still matches after a reboot. Required when a radio is selected.

The name is the stable USB name, such as `/dev/serial/by-id/usb-Icom_Inc._IC-7300_02001234-if00`. A bare name is stored with the `/dev/serial/by-id/` prefix. The service can open USB serial ports. Disks and other hardware stay closed.

## radio.frequency

The frequency to send to the radio, in MHz. For example, 145.030 is 145.030 MHz. Required when a radio is selected. Crossbeam does not pick a packet frequency for you.

The value is stored in megahertz. `145.030` is 145030000 hertz. The range is 0.1 MHz to 6000 MHz. There is no default packet frequency.

## radio.mode

The mode to send to the radio. FM is the usual choice for packet. USB, LSB, and AM are also available. An empty value uses FM. Choosing a mode does not key the transmitter.

The controller does not switch PTT. The modem keys the transmitter.

## telnet.listen

The address for a keyboard session over telnet, written as host:port, such as 192.168.1.20:8770. Leave it empty to keep telnet off. The session is readable text, with no encryption. The address is used the next time Crossbeam starts.

An empty value does not open a socket. A saved address may be a public address: this is a packet port, and `admin.public` does not apply to it. `0.0.0.0:8770` listens on every IPv4 interface. The session is described in `docs/peers.md`. A proposal on this session exchanges bulletins only when the caller address is listed in `telnet.peers`.

A hostname is saved without looking it up. At start it must resolve to one address. Port 0 is rejected. A zone id, such as `%eth0`, is rejected. A multicast address is rejected.

`--telnet` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## telnet.peers

Stations this node will call over telnet. Write each one as CALL@host:port, such as W1AW-1@192.168.1.20:8770. Anyone who can open the port can reach the keyboard prompt. Bulletin forwarding on telnet is accepted only from a station in this list. The list is read the next time Crossbeam starts.

Each callsign is stored in uppercase. SSID 0 is stored without a suffix. At most 32 stations. A hostname is resolved when the node starts, and it must resolve to one address. This list is who this node will call. Answering the telnet prompt does not require the caller to be listed. Bulletin forwarding is accepted only from an address in this list.

`--telnet-peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## fbb.listen

The address for bulletin forwarding over TCP, written as host:port, such as 192.168.1.20:6300. Leave it empty to keep this forwarding off. The session is the FBB bulletin exchange as readable text, without telnet commands. The address is used the next time Crossbeam starts.

An empty value does not open a socket. The address rules match `telnet.listen`. The bytes are the bulletin exchange in `docs/bulletin.md`, with no telnet commands. 6300 is the port FBB documents for a TCP forward.

`--fbb` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## fbb.peers

Stations this node will forward bulletins to over TCP. Write each one as CALL@host:port, such as W1AW-1@192.168.1.20:6300. Crossbeam uses this list when the station is not already in the packet-link list. An incoming forward is accepted only from a station in this list. The list is read the next time Crossbeam starts.

The entry rules match `telnet.peers`. `crossbeam bulletin send` uses this list only when the station is not in `link.peers`. An incoming forward is accepted only from an address in this list. The author stored from an accepted proposal is still the callsign written in the proposal.

`--fbb-peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## bulletin.to

The category other stations see on a bulletin this station forwards. ALL is the usual category. Use letters and digits, at most 12. An empty value is stored as ALL. The next forward uses a saved change.

This is the `TO` field of an FBB bulletin. `ALL` is the usual category. The value is stored in uppercase. The next forward uses a saved change. Restarting is not required.

## bulletin.at

The distribution other stations see on a bulletin this station forwards. WW is the widest distribution. A hierarchical address such as K5LEB.#DFW.TX is also valid. Use letters, digits, dots, hashes, and hyphens, at most 40. An empty value is stored as WW. The next forward uses a saved change.

This is the `@` field. `WW` reaches the widest set of bulletin areas. A hierarchical address such as `K5LEB.#DFW.TX` is also valid. The value is stored in uppercase. The next forward uses a saved change.

## nomad.pages

A folder where each bulletin is also written as a NomadNet page, such as /var/lib/nomadnetwork/storage/pages. Leave it empty to keep bulletins in Crossbeam only. The folder must already exist. A page is an ordinary .mu file. Publishing and importing use the saved folder immediately.

The directory must already exist. An empty value leaves every article in the store. A relative path is rejected. A path with a `..` segment is rejected. The saved path is cleaned, so a trailing slash is removed. `crossbeam page publish` and `crossbeam page ingest` read this value each time they run. The commands are described in `docs/pages.md`.

## mesh.device

The serial port of a MeshCore companion radio, such as /dev/ttyACM0. Leave it empty to keep MeshCore off. The radio stays in companion firmware. The station announces its callsign, and a MeshCore user messages it like any other contact. Crossbeam answers LIST and READ from the same bulletin list over a direct message. The port is used the next time Crossbeam starts.

The path is `/dev/ttyACM` or `/dev/ttyUSB` followed by a number, or a name under `/dev/serial/by-id/`. A relative path, a parent directory, and any other device name are rejected. An empty value leaves MeshCore off. The service opens the port the next time it starts. The commands and the cable are described in `docs/meshcore.md`.

## ai.enabled

Let callers chat with an AI model that runs on a computer on your own network. A caller types AI at the station prompt to start, and /EXIT to come back. It is off until you turn it on. You are still the control operator for every reply this station sends, and each question and reply is written to the AI log so you can review it.

The only values are `true` and `false`. The default is `false`. While it is false, AI at the station prompt answers that the assistant is not turned on, and nothing is sent to the AI server. When it is true, `ai.url` and `ai.model` must be set. See `docs/ai.md`.

## ai.backend

The kind of AI server. Choose Ollama for an Ollama computer. Choose OpenAI-compatible for Open WebUI, LM Studio, llama.cpp, or another server that speaks the OpenAI chat format.

`ollama` or `openai`. An empty value is stored as `ollama`. `ollama` posts to `/api/chat`. `openai` posts to `/v1/chat/completions`, or to the full address in `ai.url` when it already has a path, such as Open WebUI's `/api/chat/completions`.

## ai.url

The web address of the AI server, such as http://192.168.1.50:11434 for Ollama or http://192.168.1.50:3000/api/chat/completions for Open WebUI. Use an address on your own network. Required when the assistant is on.

An `http` or `https` address, at most 256 runes, with no user name, password, query, or fragment. A trailing slash is removed. For Ollama a bare `http://host:11434` is enough. For an OpenAI-compatible server, a bare host gets `/v1/chat/completions`, an address ending in `/v1` or `/api` gets `/chat/completions`, and any other path is used as-is. Crossbeam never uses an HTTP proxy from the environment for this address. An API key is not part of the address; save it with `crossbeam ai key`.

## ai.model

The name of the model to ask, exactly as the AI server lists it, such as llama3.2:3b. A small model answers faster. Required when the assistant is on.

One line, at most 128 runes, with no spaces. It is sent as the model name. Crossbeam does not download or check the model.

## ai.users

Callsigns allowed to use the assistant, separated by spaces or commas. Leave it empty to allow anyone who connects over the radio, where Crossbeam has already checked their license with the FCC. A listed callsign may also use it over the packet link or NET/ROM. Telnet callers have no callsign, so they cannot use it.

Each entry must be an AX.25 callsign. Entries are stored in uppercase without the SSID, because the license belongs to the base callsign, and duplicates are dropped. At most 256 callsigns. An empty list allows only sessions heard over the KISS radio. A MeshCore message is governed by `ai.mesh` instead.

## ai.hourly

How many questions one callsign may ask in an hour. This keeps the channel and the AI computer from being tied up by one station. Use a number from 1 to 600. An empty value is stored as 20.

A whole number from 1 to 600. The count is per base callsign, over the last hour, and it is kept in memory until `crossbeam serve` stops.

## ai.reply

The most characters one reply may use. At 1200 baud, 600 characters take about five seconds of air time. A longer reply is cut short. Use a number from 80 to 4000. An empty value is stored as 600.

A whole number from 80 to 4000. The model is asked for about that length, and a longer answer is cut at a word with ` [cut]`. A MeshCore reply is at most 400 characters. A connected session shows a long reply in pages of 12 lines.

## ai.timeout

How many seconds to wait for the AI server before giving up on a question. Use a number from 5 to 600. An empty value is stored as 90.

A whole number from 5 to 600. A MeshCore question waits at most 45 seconds, because the companion radio waits with it.

## ai.instructions

Optional instructions added to what the assistant is always told. Crossbeam already tells it to answer briefly in plain English, with no profanity, no codes, and no business. Use this for anything about your station or club.

One line, at most 500 runes. It is added after the station's own instructions, which cannot be turned off.

## ai.mesh

Also answer a MeshCore direct message that starts with AI, such as AI what is a repeater. Each message is one question with no memory of the last one. Leave this off to keep the assistant on packet only.

The only values are `true` and `false`. The default is `false`. A MeshCore contact is identified by its key prefix, not a callsign, so `ai.users` does not apply. The question limit is counted per contact.

## Command

Flags may come before or after the key.

```bash
crossbeam settings --state /var/lib/crossbeam
crossbeam settings get station.callsign --state /var/lib/crossbeam
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
```

When `crossbeam serve` is running and `--state` is omitted, the command uses the Unix socket. `--socket PATH` selects the socket explicitly. `--state` selects the database file. Both paths accept and reject the same values.

`settings get` and `settings` read a directory that already exists. They do not create it. `settings set` creates a missing directory.
