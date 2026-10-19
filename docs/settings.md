# Settings

The admin panel and the `crossbeam` command change the same settings. A setting that is not in this list is rejected. The password is not a setting. It is stored only as a hash, and it is set with `crossbeam sysop password`.

The state directory is not a setting either. Pass it as `--state`. The database, the session list, and the TLS certificate live in that directory. The directory is mode `0700`. The database and the private key are mode `0600`.

A change to `admin.listen` or `admin.public` is saved immediately and used the next time `crossbeam serve` starts. The running panel keeps the address it already bound.

## station.callsign

The station callsign. Compose uses it as the article author when the author box is empty.

One line, at most 64 runes. It may be empty until you set it. The value is not checked against a callsign pattern.

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

## Command

Flags come before the key.

```bash
crossbeam settings --state /var/lib/crossbeam
crossbeam settings get station.callsign --state /var/lib/crossbeam
crossbeam settings set station.callsign K5LEB --state /var/lib/crossbeam
```

When `crossbeam serve` is running and `--state` is omitted, the command uses the Unix socket. `--socket PATH` selects the socket explicitly. `--state` selects the database file. Both paths accept and reject the same values.
