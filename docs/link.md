# AX/UDP link

Crossbeam speaks AX.25 connected mode over UDP so a terminal can reach another node's prompt. The usual port is 10093. This is the encapsulation in RFC 1226, carried in UDP instead of IP protocol 93: one AX.25 frame per datagram, without HDLC flags or bit stuffing, with the 16-bit frame check sequence. The frame is readable text. It is not encrypted.

The link is off until `link.listen` is set. Each node lists the stations it will call in `link.peers`. A node answers a station that calls it. A listed station is accepted only from the IP address in that list. While that session is open, the reply uses the UDP port the packet came from. The next call uses the port in `link.peers` again. A listed callsign from any other address is dropped, and that drop is logged. Any other caller is answered at the address it used and can use the prompt. Bulletin forwarding on AX/UDP is accepted only from a listed station. A station on the KISS port can still forward, because that port has no peer list. A NET/ROM circuit does not accept a forward. The log records the callsign and address of every session.

## Settings

```bash
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
crossbeam settings set link.listen 192.168.1.20:10093 --state /var/lib/crossbeam
crossbeam settings set link.peers W1AW-1@192.168.1.21:10093 --state /var/lib/crossbeam
```

`station.callsign` is this station's AX.25 address. An SSID is optional: `K5LEB-1` is a different address from `K5LEB`. Restart `crossbeam serve` after changing the link settings. `--link` and `--peers` override them for one start and are not saved.

On a LinBPQ node, the matching piece is an AX/UDP port on UDP 10093 and a `MAP` line for this station's callsign and address. This station lists the LinBPQ callsign in `link.peers`. The two programs do not share code.

## Terminal

```bash
crossbeam connect W1AW-1 --state /var/lib/crossbeam
```

A callsign in `link.peers` is dialed directly. A callsign in `telnet.peers` is dialed over telnet when it is not in `link.peers`. See `docs/peers.md`. Any other callsign is dialed as a NET/ROM circuit when a route exists. See `docs/netrom.md`. When none of those paths is available, and `kiss.tnc` is set, the callsign is dialed on the radio. See `docs/radio.md`. A failed dial is not tried on the next path.

With the systemd layout the terminal socket is `/run/crossbeam/terminal.sock`. The command prints nothing until the session is up, then it shows the remote bytes. At a keyboard it edits a line locally and sends it when you press Enter. `?` lists the remote commands. `BYE` disconnects. Ctrl-C closes the local terminal.

The prompt from a Crossbeam node looks like this:

```text
[CROSSBEAM-0.9.5-FHM$]
Crossbeam K5LEB
K5LEB>
```

The first line is the FBB station id. Its version is the program version. `?` still lists the commands, and `BYE` still disconnects. Bulletin forwarding is described in `docs/bulletin.md`.

Lines on the air end with a carriage return.

## Limits

The link is modulo 8. A modulo-128 request is refused. Frames with digipeaters are ignored. One session is open to a given callsign at a time. A session with nothing passing disconnects after 15 minutes. Closing `crossbeam bulletin send` releases that callsign, including while the link is still connecting. One IP address can hold four sessions from callsigns that are not listed. ASCII FBB bulletins use this session when the other station is listed. A station reached through NET/ROM is described in `docs/netrom.md`. The radio port is described in `docs/radio.md`.
