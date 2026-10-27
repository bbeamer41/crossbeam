# FBB bulletins

Crossbeam forwards stored articles as ASCII FBB bulletins on the AX.25 session from `docs/link.md`. One forward publishes every article this station holds and ingests the bulletins the other station offers. The text is readable. It is not compressed and it is not encrypted.

Private mail and NTS traffic are refused. Only bulletin type `B` is stored.

## Send

The link has to be running. A station in `link.peers` is dialed on AX/UDP. A station that is not in that list is dialed on FBB TCP when it is in `fbb.peers`. See `docs/peers.md`. The radio and NET/ROM are not used.

```bash
crossbeam bulletin send W1AW-1 --state /var/lib/crossbeam
```

With the systemd layout the terminal socket is `/run/crossbeam/terminal.sock`. `--socket` selects that path. The command waits until the exchange finishes and prints `sent N received N`. Stopping the command closes the forward and releases the callsign, including while the link is still connecting. A second run sends the same articles again. The peer answers `-` for a bulletin id it already has, and nothing new is stored.

`bulletin.to` and `bulletin.at` set the category and the distribution. The defaults are `ALL` and `WW`.

## What is on the wire

The called station sends a station id, the `Crossbeam CALL` line, and the `CALL>` prompt. The caller sends its own station id and then the proposals. A person can still send `?` or `BYE` if the first line is not a forwarding command.

The station id looks like `[CROSSBEAM-0.8.0-FHM$]`. The version is the program version. `F` is batch forwarding, `H` is a hierarchical address, `M` is a message id, and `$` is a bulletin id. Compressed forwarding is not offered.

A proposal is one or more lines, then a checksum line:

```text
FB B K5LEB WW ALL NIGHT 23
F> 50
```

That size is the subject `Net tonight`, a carriage return, the body `Meet at 7.`, and the final `0x1A`. The checksum `50` is the 8-bit sum of the proposal line and its carriage return.

The fields are the command, the type (`B`), the sender, the distribution, the category, the bulletin id, and the size in bytes. At most five proposals are sent together. A block stays near 10 KB unless a single message is larger, in which case that message is its own block.

The answer is `FS` and one mark per proposal. `+` accepts it. `-` rejects it or means the peer already has that id. `=` and `H` defer it to a later forward. `R` and `E` are treated as rejects. Crossbeam sends only `+` and `-`.

An accepted message is the subject, a carriage return, the body with carriage returns, and a final byte `0x1A`. The size counts all of those bytes. Lines may use a carriage return or a carriage return and a line feed. The line feed is not part of the checksum. In a message, a carriage return and line feed is one line ending, not a blank line.

After the bodies, the peer offers its own bulletins, or sends `FF` when it has nothing more, or `FQ` to close. `FS` may be followed immediately by the next proposal when every proposal was rejected.

A line that starts with `;` is a comment and is ignored.

## Article fields

| Article | Bulletin |
| --- | --- |
| Title | Subject, at most 80 characters |
| Author | Sender, when the author is an AX.25 callsign. Otherwise this station's callsign. |
| ID | Bulletin id, when it is 1 to 12 letters, digits, hyphens, or underscores. A longer or empty id is replaced by a stable id of at most 12 characters. |
| Body | Message text. A carriage return, or a carriage return and line feed, becomes one newline, then the article rules in `docs/article.md` apply. Routing lines that begin with `R:` stay in the body. |
| Created | Not sent. The receiver stores the time it accepted the bulletin. |

The receiver stores the bulletin id as the article id and the sender as the author. A title, author, id, and body therefore come back in that form. Created becomes the receive time. A title longer than 80 characters is not offered. The other station's bulletins are still accepted, and this station sends `FF`.

## Checksum

`F>` may stand alone. A peer that sends two hex digits is checked. The sum is an 8-bit total of every proposal line, including the carriage return at the end of each line, and not including the `F>` line. It is written as two uppercase hex digits. A following line feed is ignored and is not added. A wrong checksum, a missing field, or a bad character ends the session. Nothing from that proposal is stored. Two Crossbeam nodes use this sum.

## Limits

One framed message, including the subject, the carriage returns, the body, and the final `0x1A`, may be at most 256 KiB. An article whose framed message is larger stops `crossbeam bulletin send` before it dials and names that article. The other station's bulletins are still accepted, and this station sends `FF`. A session accepts at most 100 new bulletins and 2 MiB of message text. A larger offer ends the session before the `FS` line, so the peer retries it later. Bulletins already stored stay stored.

A peer bulletin is deferred with `=` when the store already holds 2000 articles or 32 MiB of body text. It is offered again on a later forward. The admin panel can still save an article. `crossbeam article delete ROW --state DIR` removes one article and frees that room. Shortening a body in the admin panel frees bytes as well.

A line that starts with `***` is an error from the peer. The forward stops and reports that text. A bad message after `+` is not stored, so the peer can offer it again.
