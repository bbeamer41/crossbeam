# Install

Crossbeam runs as one program on Debian. The package installs the admin panel. It does not install a modem, and it does not change any other packet service on the machine.

The archive is amd64. `apt update` reads it. `apt upgrade` installs a newer Crossbeam when one is published.

## Package

```bash
sudo install -d -m 0755 /etc/apt/keyrings
wget -O /tmp/crossbeam.asc https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt/crossbeam-archive-key.asc
wget -O /tmp/crossbeam.list https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt/crossbeam.list
sudo install -m 0644 /tmp/crossbeam.asc /etc/apt/keyrings/crossbeam.asc
sudo install -m 0644 /tmp/crossbeam.list /etc/apt/sources.list.d/crossbeam.list
sudo apt update
sudo apt install crossbeam
```

The signing key fingerprint is `E6B7A9B54B63A15E37C0979FC4B7FFE1AB0D6034`.

The package adds the `crossbeam` user, creates `/var/lib/crossbeam` at mode `0700`, and installs `/usr/lib/systemd/system/crossbeam.service`. It does not enable the service and it does not start it. Set the sysop password before you start it. Removing the package leaves the state directory in place.

The unit in the package uses `/usr/bin/crossbeam`. Do not also copy `deploy/crossbeam.service` to `/etc/systemd/system`, because that file would override the packaged unit and would still expect `/usr/local/bin/crossbeam`.

When a newer package is in the archive:

```bash
sudo apt update
sudo apt upgrade
```

## Password

Set a sysop password before the service starts. There is no default. The password is at least 12 characters, it is read from the terminal, and it is not stored in the command line. The command asks for it a second time and does not save it when the two entries differ.

```bash
sudo -u crossbeam crossbeam sysop password --state /var/lib/crossbeam
```

If the service starts without a password, it exits and tells you to run that command. It does not keep restarting.

## Service

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now crossbeam
```

The service listens on the saved address, or on a private LAN address at port 8443 when `admin.listen` is empty. It keeps the state directory at mode `0700` and creates files with umask `0077`. The Unix socket is `/run/crossbeam/crossbeam.sock`, mode `0660`, so a user in the `crossbeam` group can use the command without reading the database.

```bash
sudo usermod -aG crossbeam "$USER"
```

Group membership applies to new logins.

## Open the panel

`systemctl status crossbeam` and the service log show the panel URL and the certificate fingerprint. The same lines are printed by:

```bash
crossbeam status --socket /run/crossbeam/crossbeam.sock
```

Open the `https://` URL in a browser. The certificate is self-signed, so the browser warns until you trust that fingerprint. Then sign in with the sysop password.

To choose the address instead of the automatic one:

```bash
sudo -u crossbeam crossbeam settings set admin.listen 192.168.1.20:8443 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
```

Use an address of this machine. A public address is rejected until you set `admin.public` to `true` and restart.

## AX/UDP link

The link stays off until you set it. Use an address of this machine. 10093 is the usual port. List the other station, and have that station list this one. Then a terminal on either side can reach the other prompt. The frames are readable text. Details are in `docs/link.md`.

```bash
sudo -u crossbeam crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set link.listen 192.168.1.20:10093 --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set link.peers W1AW-1@192.168.1.21:10093 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
crossbeam connect W1AW-1 --socket /run/crossbeam/terminal.sock
```

`?` lists the commands. `BYE` disconnects. The terminal socket is mode `0660`, next to the admin socket, so a user in the `crossbeam` group can connect without reading the state directory.

## Radio

The radio stays off until `kiss.tnc` is set. Crossbeam dials a KISS TNC. It does not modulate audio and it does not start the modem. Graywolf in modem mode is the supported TNC, on 1200 baud AFSK, with its digipeater and iGate off. The usual address on the same machine is `127.0.0.1:6700`. Details are in `docs/radio.md`.

```bash
sudo -u crossbeam crossbeam settings set kiss.tnc 127.0.0.1:6700 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
```

`--kiss` on `crossbeam serve` uses another address for one start and does not change the saved setting.

## Telnet and FBB TCP

Both stay off until you set them. Telnet reaches the station prompt. FBB TCP carries the ASCII bulletin exchange on a raw TCP session. The bytes are cleartext. Details are in `docs/peers.md`.

```bash
sudo -u crossbeam crossbeam settings set telnet.listen 192.168.1.20:8770 --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set telnet.peers W1AW-1@192.168.1.21:8770 --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set fbb.listen 192.168.1.20:6300 --state /var/lib/crossbeam
sudo -u crossbeam crossbeam settings set fbb.peers W1AW-1@192.168.1.21:6300 --state /var/lib/crossbeam
sudo systemctl restart crossbeam
```

## Bulletins

With the link up, forward every stored article to a listed station and take the bulletins that station offers back:

```bash
crossbeam bulletin send W1AW-1 --socket /run/crossbeam/terminal.sock
```

The exchange is ASCII FBB. Details, including the bulletin id and the checksum, are in `docs/bulletin.md`.

## Pages

NomadNet keeps its own pages directory. Point `nomad.pages` at that directory when it exists. An empty value leaves articles in the store. Crossbeam writes one normal `.mu` file per article and reads pages back. The service umask is `0077`. A page written by Crossbeam is still mode `0644`, so NomadNet can read it.

```bash
sudo -u crossbeam crossbeam settings set --state /var/lib/crossbeam nomad.pages /var/lib/nomadnetwork/storage/pages
sudo -u crossbeam crossbeam page publish --state /var/lib/crossbeam
sudo -u crossbeam crossbeam page ingest --state /var/lib/crossbeam
```

Details are in `docs/pages.md`.

## MCP

`crossbeam mcp` reads and writes the article store, and it publishes and ingests NomadNet pages. The session is standard input and standard output. It does not open a port. The state directory is mode `0700`, so run it as the user who owns that directory. Details are in `docs/mcp.md`.

```bash
sudo -u crossbeam crossbeam mcp --state /var/lib/crossbeam
```

## Offline

The article store, the settings, and the admin panel on the LAN do not need a default route. A hostname in `admin.listen` is resolved once when the service starts, and the panel binds those addresses. If that lookup fails, the service does not start. Use an IP address when the machine must boot with no DNS.

## From a checkout

These steps install a binary you built yourself. They do not add the apt archive, so `apt upgrade` will not move that binary forward.

```bash
go build -o crossbeam ./cmd/crossbeam
sudo install -m 0755 crossbeam /usr/local/bin/crossbeam
sudo useradd --system --home /var/lib/crossbeam --shell /usr/sbin/nologin crossbeam
sudo mkdir -p /var/lib/crossbeam
sudo chown crossbeam:crossbeam /var/lib/crossbeam
sudo chmod 0700 /var/lib/crossbeam
sudo cp deploy/crossbeam.service /etc/systemd/system/crossbeam.service
```

`deploy/crossbeam.service` expects `/usr/local/bin/crossbeam`. Set the password, then start the service with the commands above.

## Files

| Path | What it is |
| --- | --- |
| `/usr/local/bin/crossbeam` | The program, for the manual install |
| `/usr/bin/crossbeam` | The program, for the Debian package |
| `/etc/apt/keyrings/crossbeam.asc` | The apt signing key |
| `/etc/apt/sources.list.d/crossbeam.list` | The apt archive |
| `/var/lib/crossbeam` | State directory, mode `0700` |
| `/var/lib/crossbeam/crossbeam.db` | Articles, settings, and the password hash |
| `/var/lib/crossbeam/tls/` | The panel certificate and private key |
| `/run/crossbeam/crossbeam.sock` | Local command socket |
| `/etc/systemd/system/crossbeam.service` | The service, for the manual install |
| `/usr/lib/systemd/system/crossbeam.service` | The service, for the Debian package |

Settings are listed in `docs/settings.md`. Article text is described in `docs/article.md`.
