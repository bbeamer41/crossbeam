# Turn on the radio

This page is the hour after your callsign is saved in [start.md](start.md). It ends when Graywolf is listening on port 6700 and Crossbeam is dialing it. The board you already wrote stays on this computer either way.

The full KISS port, the license check, and the timing bytes are in `docs/radio.md`. Leave every Graywolf switch you do not recognize as you found it.

## What you need

The Crossbeam package already installed Graywolf. Its service is enabled and stays stopped until you start it.

```bash
sudo systemctl start graywolf
```

Graywolf's page is port 8080 on this computer. The first visit asks you to create a password. Set that password. The page is reachable from other computers on your network until you do. Use the KISS page there. The AGWPE port is a different protocol, and Crossbeam does not use it.

You need a radio Graywolf can hear and key, and a sound interface for that radio, such as a USB sound card. The radio should already be on the packet frequency you intend to use, in FM. Use your local packet frequency. Leave the APRS frequency alone. Crossbeam does not choose that frequency, and it does not change Graywolf's switches.

## The channel

Add a channel and choose **Modem-backed**. That means Graywolf's software modem supplies the radio.

| Setting | Value |
| --- | --- |
| Modem type | AFSK |
| Bit rate | 1200 |
| Mark | 1200 Hz |
| Space | 2200 Hz |

Choose the audio input and the audio output for the radio. On the channel card, set PTT so Graywolf can key the transmitter. A channel whose PTT line says it is not configured drops outgoing frames. The PTT choices are on the Graywolf [PTT](https://chrissnell.com/software/graywolf/ptt.html) page.

On that channel, leave the digipeater off and leave the iGate off. APRS stays in Graywolf.

The on-air mode is 1200 baud. A serial rate of 9600 in the Graywolf handbook is the cable rate to a hardware TNC.

## The KISS port

Add a KISS interface. The handbook for that page is <https://chrissnell.com/software/graywolf/kiss.html>.

| Setting | Value |
| --- | --- |
| Type | TCP server |
| Mode | Modem |
| Listen address | `127.0.0.1:6700` |

Modem mode means Graywolf's software modem supplies the radio and Crossbeam dials in. TNC mode expects a hardware TNC on the other end of the cable. The wrong mode reports `txbackend: no backend registered for channel`.

Port 6700 is the usual KISS port. A listen address of `0.0.0.0:6700` also accepts Crossbeam at `127.0.0.1:6700`. `127.0.0.1` keeps the port on this computer.

In that same interface editor, turn on `allow_connected_mode`. Graywolf names the setting that way. It is off by default, and Graywolf then drops the connected frames Crossbeam sends.

Leave `gate_tx_to_is` off. Leave **Allow digipeater/beacon/iGate to transmit on this interface** off. That box is for TNC mode.

Save the interface. The channel card should show a modem backing it.

## Point Crossbeam at it

Open Settings in the Crossbeam panel. Set Modem address (`kiss.tnc`) to `127.0.0.1:6700` and save. Then restart the service. The saved address is used the next time Crossbeam starts.

```bash
sudo -u crossbeam crossbeam settings set kiss.tnc 127.0.0.1:6700 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
```

Leave the Radio dial boxes empty when the radio is already on the packet frequency. Choosing a radio, a USB port, a frequency, and a mode is in `docs/radio.md`. Graywolf still sends the audio and keys the transmitter.

## A working path

Crossbeam dials `127.0.0.1:6700` and sends 1200 baud KISS. Another station can connect to your callsign and type `LIST`, then `READ` and a number, then `BYE`. From this computer, with the service socket:

```bash
crossbeam connect W1AW-1 --socket /run/crossbeam/terminal.sock
```

Use the other station's callsign. A transmission goes out only when the FCC lists both stations as active amateur licenses. Until that check can be read, the radio stays quiet and the board on this computer still works. What the port ignores, including APRS, is in `docs/radio.md`.
