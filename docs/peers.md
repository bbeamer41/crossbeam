# Telnet and FBB TCP

Telnet and FBB TCP are cleartext. They carry the same readable session as an AX.25 connection. Neither one is encrypted, and neither one asks for a password. Leave the address empty to keep the port off. Bind a LAN address. Anyone who can open the port can use the station. Telnet shows the prompt. A proposal on either port exchanges bulletins, and the author saved from that proposal is the callsign written in it.

A change to these settings is used the next time `crossbeam serve` starts.

## Telnet

Telnet is the prompt, with the small option exchange from RFC 854 removed before the session sees the bytes. This station does not echo. The client shows what you type. Lines still end with a carriage return, and a following line feed or NUL is ignored. A data byte of 255 is sent as two IAC bytes.

```bash
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
crossbeam settings set telnet.listen 192.168.1.20:8770 --state /var/lib/crossbeam
crossbeam settings set telnet.peers W1AW-1@192.168.1.21:8770 --state /var/lib/crossbeam
```

`crossbeam connect W1AW-1` dials a station in `telnet.peers` when that station is not in `link.peers`. The order is in `docs/radio.md`. A failed telnet dial is not tried as a NET/ROM circuit and not tried on the radio.

The remote prompt looks the same as an AX.25 session:

```text
[CROSSBEAM-0.8.0-FHM$]
Crossbeam K5LEB
K5LEB>
```

`?` lists the commands. `BYE` disconnects. A session with nothing passing disconnects after 15 minutes. Each port accepts 8 sessions. The next one is told the station is busy and then closed.

`--telnet` and `--telnet-peers` on `crossbeam serve` override one start and are not saved.

## FBB TCP

FBB TCP is a raw TCP session. It does not use telnet commands. The bytes are the ASCII bulletin exchange in `docs/bulletin.md`: the station id, the prompt, the proposals, and the messages. Compressed forwarding is not offered. 6300 is the port FBB documents for this kind of TCP forward. The port here can be any port you set.

```bash
crossbeam settings set fbb.listen 192.168.1.20:6300 --state /var/lib/crossbeam
crossbeam settings set fbb.peers W1AW-1@192.168.1.21:6300 --state /var/lib/crossbeam
```

`crossbeam bulletin send W1AW-1` uses AX/UDP when the station is in `link.peers`. A failure stays on that path. When the station is not in `link.peers` and it is in `fbb.peers`, the same exchange runs over FBB TCP. A failed TCP dial is not tried on the radio and not tried as a NET/ROM circuit. Bulletins are not sent over the telnet port.

The idle limit and the cap of 8 sessions match telnet. `--fbb` and `--fbb-peers` override one start and are not saved.
