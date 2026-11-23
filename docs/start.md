# Start here

Crossbeam is a bulletin board for an amateur radio station. You write a note on this computer. Another station can call yours and read it. The same note can later become a page for NomadNet. None of that has to be set up before the board exists.

This page is the first hour. It ends when your callsign is saved and one bulletin is on the board. The other documents are for a station that is already running. Leave every setting you do not recognize empty. An empty setting is off.

## What you need

You need a computer running Debian or Ubuntu, and the amateur callsign on your license. You do not need a radio, a modem, or another station for this hour.

A Technician license is enough. The callsign on that license is the address of this station.

## Install

Install the package by the steps in `docs/install.md`. Then set a sysop password and start the service. The password is at least 12 characters. The program asks for it twice and does not print it.

```bash
sudo -u crossbeam crossbeam sysop password --state /var/lib/crossbeam
sudo systemctl enable --now crossbeam
```

## Open the panel

The service log prints the panel address and the certificate fingerprint. The same lines come from:

```bash
crossbeam status --socket /run/crossbeam/crossbeam.sock
```

Open the `https://` address in a browser. The certificate is self-signed, so the browser warns once. Compare the fingerprint it shows with the one in the log, then continue. Sign in with the sysop password.

If the log says `https://127.0.0.1:8443`, the panel is only on this computer. That happens when the service started before the network address was ready. The address to use on your own network is in `docs/settings.md` under `admin.listen`. Saving it and restarting the service opens the panel there.

## Save your callsign

Open Settings. The first box is your callsign. Type the callsign on your license, such as `K5LEB`. Save settings.

A dash and a number, such as `K5LEB-1`, is an extra address for the same license. You can leave that off until you know you want it.

The rest of the page can stay as it is. Each box has an example in faint text and an i that explains the setting. Those are there for later.

## Write one bulletin

Open Compose. Type a title and a message. Leave the other boxes empty. Save.

Open Bulletins. The note is there. That is a working board. It is on this computer, and it stays there if the network or the radio is off.

## What a caller types

When another station connects, it sees your callsign and a prompt that ends with `>`. The caller types one of these and presses Enter:

```text
?
LIST
READ 1
BYE
```

`?` lists the commands. `LIST` lists the bulletins. `READ` and a number opens one. `BYE` disconnects.

You cannot type those from the panel. The panel is where you write and edit. The prompt is what a connected station sees. Connecting that station is the next step, and it can wait.

## Turn the radio on later

The radio stays quiet until you set a modem. The supported modem is Graywolf, on 1200 baud, on the same computer. The setting is `kiss.tnc`, and the usual value is `127.0.0.1:6700`. Crossbeam does not start Graywolf. The hour that turns the radio on is `docs/graywolf.md`. The KISS port itself is `docs/radio.md`.

A transmission goes out only when the FCC lists both stations as active amateur licenses. A brand-new callsign has to be in that list first. Until then the board still works on this computer.

## The rest can wait

These are off while their settings are empty. Each one has its own document. You do not need any of them for the first hour.

| When you want it | Setting | Document |
| --- | --- | --- |
| A direct connection to one other Crossbeam or LinBPQ node | `link.listen` and `link.peers` | `docs/link.md` |
| Callers who connect with a telnet program | `telnet.listen` | `docs/peers.md` |
| Sending the bulletins to another BBS | `fbb.listen` or `fbb.peers` | `docs/bulletin.md` |
| A route through a neighbor station | the NET/ROM settings | `docs/netrom.md` |
| The same bulletin as a NomadNet page | `nomad.pages` | `docs/pages.md` |
| A MeshCore radio that answers LIST and READ | `mesh.device` | `docs/meshcore.md` |
| An AI assistant on the station prompt | `ai.enabled` | `docs/ai.md` |

The settings page lists every one of these. Saving a callsign does not turn them on.
