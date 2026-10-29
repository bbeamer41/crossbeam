# Settings

The admin panel and the `crossbeam` command change the same settings. A setting that is not in this list is rejected. The password is not a setting. It is stored only as a hash, and it is set with `crossbeam sysop password`.

The state directory is not a setting either. Pass it as `--state`. The database, the session list, and the TLS certificate live in that directory. The directory is mode `0700`. The database and the private key are mode `0600`.

A change to `admin.listen`, `admin.public`, `link.listen`, `link.peers`, `kiss.tnc`, `telnet.listen`, `telnet.peers`, `fbb.listen`, or `fbb.peers` is saved immediately and used the next time `crossbeam serve` starts. The running panel keeps the address it already bound. The running link keeps the peers, the TNC address, and the TCP ports it already resolved.

## station.callsign

The station callsign. Compose uses it as the article author when the author box is empty. The AX/UDP link, the KISS radio, telnet, and FBB TCP use it as the station address.

One line, at most 64 runes. It may be empty until you set it. When the link, the radio, telnet, and FBB TCP are off, the value is not checked against a callsign pattern. When `link.listen`, `kiss.tnc`, `telnet.listen`, or `fbb.listen` is set, it must be an AX.25 callsign, with an optional SSID such as `K5LEB-1`.

## station.name

A short name for this station, shown in the admin panel.

One line, at most 80 runes. It may be empty.

## admin.listen

The host and port for the admin panel, such as 192.168.1.20:8443. Leave this empty to choose a private LAN address on port 8443. A change takes effect on the next start.

An empty value chooses the first private IPv4 address on a non-loopback interface, on port 8443. If the machine has none, the panel listens on `127.0.0.1:8443`. The automatic choice is never a public address and never `0.0.0.0` or `::`.

While `admin.public` is false, a saved address must be a loopback address, a private address, a link-local address, or an IPv6 local address. `localhost` is allowed. A hostname is saved without looking it up, so this works with no network. At start, the name is resolved once. Every address must be loopback, private, or link-local. The panel binds those addresses and does not look the name up again. The certificate includes the hostname and those addresses, so the browser can open either. One port is used for every address. If DNS fails, the panel does not start. An IP address is the reliable choice when the machine must boot with no DNS.

Port 0 is rejected. A zone id, such as `%eth0`, is rejected.

`--listen` on `crossbeam serve` binds this start to another address and does not change the saved setting. A public address still requires `admin.public`.

## admin.public

Allow the admin panel to listen on a public address. Leave this false to keep the panel on the LAN.

The only values are `true` and `false`.

Leave this false unless you intend to put the admin panel on a public address. The panel still requires the sysop password, and the browser will warn until you trust the certificate.

## link.listen

The host and port for the AX/UDP link, such as 192.168.1.20:10093. Leave this empty to keep the link off. A change takes effect on the next start.

10093 is the usual port. The payload is one AX.25 frame, as described in `docs/link.md`. An empty value does not open a socket. A saved address may be a public address: this is the packet port, and `admin.public` does not apply to it. `0.0.0.0:10093` listens on every IPv4 interface. The bytes are readable text.

A hostname is saved without looking it up. At start it must resolve to one address. Port 0 is rejected. A zone id, such as `%eth0`, is rejected.

`--link` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## link.peers

Stations this node can call. Separate entries with spaces or commas. Each entry is CALL@host:port, such as W1AW-1@192.168.1.20:10093. A change takes effect on the next start.

Each callsign is stored in uppercase. SSID 0 is stored without a suffix. At most 32 stations. A hostname in an entry is resolved when the link starts, and it must resolve to one address. This node answers the prompt for a station that is not listed. Bulletin forwarding on AX/UDP is accepted only from a station in this list. The list is also who this node will call. While a session with that station is open, the reply goes to the address the request came from. The next call uses the port in this list again.

A listed station is also a NET/ROM neighbor. Routes learned from those neighbors stay in memory until `crossbeam serve` stops. They are described in `docs/netrom.md`.

`--peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## kiss.tnc

The TCP address of the KISS TNC, such as 127.0.0.1:6700. Leave this empty to keep the radio off. The radio is 1200 baud. A change takes effect on the next start.

An empty value does not open a connection. The value is `host:port`. Port 0 is rejected. A zone id, such as `%eth0`, is rejected. A multicast address is rejected. `0.0.0.0` and `::` are rejected. This value is the address Crossbeam dials. An IP address is stored in canonical form. A hostname is saved without looking it up. At start it must resolve to one address. The behavior of the port is described in `docs/radio.md`.

`--kiss` on `crossbeam serve` uses another address for this start and does not change the saved setting.

## telnet.listen

The host and port for telnet sessions, such as 192.168.1.20:8770. Leave this empty to keep telnet off. Telnet is cleartext. A change takes effect on the next start.

An empty value does not open a socket. A saved address may be a public address: this is a packet port, and `admin.public` does not apply to it. `0.0.0.0:8770` listens on every IPv4 interface. The session is described in `docs/peers.md`. A proposal on this session exchanges bulletins only when the caller address is listed in `telnet.peers`.

A hostname is saved without looking it up. At start it must resolve to one address. Port 0 is rejected. A zone id, such as `%eth0`, is rejected. A multicast address is rejected.

`--telnet` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## telnet.peers

Stations this node can call over telnet. Separate entries with spaces or commas. Each entry is CALL@host:port, such as W1AW-1@192.168.1.20:8770. A change takes effect on the next start.

Each callsign is stored in uppercase. SSID 0 is stored without a suffix. At most 32 stations. A hostname is resolved when the node starts, and it must resolve to one address. This list is who this node will call. Answering the telnet prompt does not require the caller to be listed. Bulletin forwarding is accepted only from an address in this list.

`--telnet-peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## fbb.listen

The host and port for FBB TCP forwarding, such as 192.168.1.20:6300. Leave this empty to keep FBB TCP off. The session is cleartext ASCII. A change takes effect on the next start.

An empty value does not open a socket. The address rules match `telnet.listen`. The bytes are the bulletin exchange in `docs/bulletin.md`, with no telnet commands. 6300 is the port FBB documents for a TCP forward.

`--fbb` on `crossbeam serve` binds this start to another address and does not change the saved setting. Port 0 is allowed on that flag so a test can ask for a free port.

## fbb.peers

Stations this node can forward to over FBB TCP. Separate entries with spaces or commas. Each entry is CALL@host:port, such as W1AW-1@192.168.1.20:6300. A change takes effect on the next start.

The entry rules match `telnet.peers`. `crossbeam bulletin send` uses this list only when the station is not in `link.peers`. An incoming forward is accepted only from an address in this list. The author stored from an accepted proposal is still the callsign written in the proposal.

`--fbb-peers` on `crossbeam serve` uses another list for this start and does not change the saved setting.

## bulletin.to

The category used when this station forwards a bulletin, such as ALL. Letters and digits, at most 12. An empty value uses ALL.

This is the `TO` field of an FBB bulletin. `ALL` is the usual category. The value is stored in uppercase. The next forward uses a saved change. Restarting is not required.

## bulletin.at

The distribution used when this station forwards a bulletin, such as WW. Letters, digits, dots, hashes, and hyphens, at most 40. An empty value uses WW.

This is the `@` field. `WW` reaches the widest set of bulletin areas. A hierarchical address such as `K5LEB.#DFW.TX` is also valid. The value is stored in uppercase. The next forward uses a saved change.

## nomad.pages

The directory for NomadNet pages, such as /var/lib/nomadnetwork/storage/pages. Leave this empty to keep pages in the article store only. A page is a normal .mu file. Publishing uses the saved value immediately.

The directory must already exist. An empty value leaves every article in the store. A relative path is rejected. A path with a `..` segment is rejected. The saved path is cleaned, so a trailing slash is removed. `crossbeam page publish` and `crossbeam page ingest` read this value each time they run. The commands are described in `docs/pages.md`.

## Command

Flags may come before or after the key.

```bash
crossbeam settings --state /var/lib/crossbeam
crossbeam settings get station.callsign --state /var/lib/crossbeam
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
```

When `crossbeam serve` is running and `--state` is omitted, the command uses the Unix socket. `--socket PATH` selects the socket explicitly. `--state` selects the database file. Both paths accept and reject the same values.
