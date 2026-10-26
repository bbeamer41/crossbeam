# KISS radio

Crossbeam dials a KISS TNC and carries AX.25 connected mode. The framing is the KISS protocol published by K3MC and KA9Q. A frame starts and ends with FEND, and FEND or FESC inside the frame is escaped. The first byte after FEND is the port and the command. Port 0 is the only port this station uses. A data frame is an AX.25 frame without its frame check sequence. The TNC adds that check sequence. Crossbeam does not modulate audio.

The radio is off until `kiss.tnc` is set.

## Graywolf

Graywolf is the supported modem. Use modem mode, so Graywolf supplies the radio and Crossbeam is the TCP client. The conventional KISS port is 6700. On the same machine the address is `127.0.0.1:6700`. The channel is 1200 baud AFSK.

Turn on connected mode (`allow_connected_mode`). It is off by default, and Graywolf then drops connected frames. Leave `gate_tx_to_is` off. Leave the digipeater off on that channel. Leave the iGate off. Do not enable transmit for the digipeater, the beacon, or the iGate on this interface. Do not use TNC mode. That mode expects an external TNC to dial Graywolf.

Crossbeam does not start Graywolf and does not call its web API. The web UI is often port 8080, and AGWPE is a different protocol. Neither is used here.

9600 baud is not a supported radio mode. A serial rate of 9600 in the Graywolf handbook is the cable rate to a hardware TNC, not the on-air mode. This port is TCP only. A later serial TNC can use the same framing.

## Parameters

Each time the TCP connection comes up, including after it drops and is dialed again, Crossbeam sends the published half-duplex set:

| Command | Value | Meaning |
| --- | --- | --- |
| TX delay | 50 | 500 ms, in 10 ms units |
| Persistence | 63 | 63 of 256 |
| Slot time | 10 | 100 ms, in 10 ms units |
| TX tail | 0 | no extra key-up |
| Full duplex | 0 | half duplex |

There is no setting for these values and no baud setting. SetHardware is not sent, and the TNC is not asked to leave KISS. A software modem may ignore the timing bytes. They are still sent so a hardware TNC on the same stream gets the 1200 baud set.

## What is ignored

Frames on a KISS port other than 0 are ignored. Commands other than data are ignored. Digipeater paths are ignored. Unnumbered information is ignored, so APRS and node broadcasts stay with Graywolf and do not take a session here. A frame that is not addressed to this station is ignored. A frame sent by this station is ignored. A callsign listed in `link.peers` stays on AX/UDP and is not answered on the radio.

One session is open to a given callsign at a time.

## Connecting

`crossbeam connect CALL` tries these paths in order:

1. A callsign in `link.peers` is dialed on AX/UDP. A failure stays a failure.
2. Otherwise a NET/ROM route is used when one exists. A failure stays a failure. See `docs/netrom.md`.
3. Otherwise, when `kiss.tnc` is set, the callsign is dialed on the radio. It does not have to be listed.
4. Otherwise the command reports that the callsign is not listed.

`crossbeam bulletin send` stays on listed AX/UDP stations. It does not use the radio. NET/ROM broadcasts stay on AX/UDP.

When the TNC is down, or a write to it fails, Crossbeam closes that TCP connection and dials again. An AX.25 session retries its own frames across that gap.

## Settings

```bash
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
crossbeam settings set kiss.tnc 127.0.0.1:6700 --state /var/lib/crossbeam
```

`station.callsign` is the address on the radio. Restart `crossbeam serve` after changing `kiss.tnc`. `--kiss HOST:PORT` uses another address for one start and does not change the saved setting. Validation is described in `docs/settings.md`.
