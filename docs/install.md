# Install

Crossbeam runs as one program on Debian. This installs the admin panel. It does not install a modem, and it does not change any other packet service on the machine.

The commands below use `sudo`. Run them from a checkout of this repository.

## Build

```bash
go build -o crossbeam ./cmd/crossbeam
sudo install -m 0755 crossbeam /usr/local/bin/crossbeam
```

A later goal packages this for Debian. Until then, `/usr/local/bin/crossbeam` is the path the service file expects.

## User and directories

```bash
sudo useradd --system --home /var/lib/crossbeam --shell /usr/sbin/nologin crossbeam
sudo mkdir -p /var/lib/crossbeam
sudo chown crossbeam:crossbeam /var/lib/crossbeam
sudo chmod 0700 /var/lib/crossbeam
```

## Password

Set a sysop password before the service starts. There is no default. The password is at least 12 characters, it is read from the terminal, and it is not stored in the command line.

```bash
sudo -u crossbeam crossbeam sysop password --state /var/lib/crossbeam
```

If the service starts without a password, it exits and tells you to run that command. It does not keep restarting.

## Service

```bash
sudo cp deploy/crossbeam.service /etc/systemd/system/crossbeam.service
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

## Offline

The article store, the settings, and the admin panel on the LAN do not need a default route. A hostname in `admin.listen` is resolved once when the service starts, and the panel binds those addresses. If that lookup fails, the service does not start. Use an IP address when the machine must boot with no DNS.

## Files

| Path | What it is |
| --- | --- |
| `/usr/local/bin/crossbeam` | The program |
| `/var/lib/crossbeam` | State directory, mode `0700` |
| `/var/lib/crossbeam/crossbeam.db` | Articles, settings, and the password hash |
| `/var/lib/crossbeam/tls/` | The panel certificate and private key |
| `/run/crossbeam/crossbeam.sock` | Local command socket |
| `/etc/systemd/system/crossbeam.service` | The service |

Settings are listed in `docs/settings.md`. Article text is described in `docs/article.md`.
