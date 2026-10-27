# NET/ROM routing

A listed neighbor is also a NET/ROM neighbor. This node learns routes from the node broadcasts those neighbors send, and `crossbeam connect` can reach a station that is not listed when a route to it exists. The frames ride on the AX/UDP link in `docs/link.md`. A radio connection is plain AX.25 and does not carry these routes. See `docs/radio.md`. Routing has no new setting.

## Neighbors

`link.peers` is the neighbor list. A broadcast from a station that is not listed is ignored. A circuit frame, and a frame that would be relayed, is accepted only from a listed neighbor. A frame for a circuit this node is part of is accepted only from the neighbor that circuit uses, and only when the network origin is the station at the other end. This node does not add a neighbor from a broadcast, and it sends a route only toward a station it can already call.

Configured neighbor routes stay until the list changes. Learned routes are kept in memory. They are gone after `crossbeam serve` restarts, and the next broadcast fills them in again. When neighbors are listed, this node sends its own broadcast at startup and then about once an hour. An empty table still sends that broadcast, so neighbors learn this callsign.

A learned route that is not announced again is left out of this node's broadcast after about two hours and is deleted after about six hours.

## Quality

Quality runs from 255, best, to 0, worst. A route below 1 is not used. The direct path to a listed neighbor has quality 192, the published default for a 1200 baud channel.

A route heard in a neighbor's broadcast is recorded via that neighbor. The station named inside the record is used to detect a loop. Its quality is the integer value of

```text
(broadcast quality * path quality + 128) / 256
```

A broadcast quality of 192 heard over a path of quality 192 becomes 144. A destination that is this station, the sending neighbor, or NODES is not stored. A record whose best neighbor is this station is not stored.

Up to three routes are kept for one destination, highest quality first, and up to 50 destinations. The table is not edited with a NODES command.

## Connect

`crossbeam connect CALL` uses a direct AX.25 session when CALL is listed. Those bytes are the normal text session, including FBB bulletins. A callsign in `telnet.peers` is dialed over telnet before a route is used. When CALL is not listed that way and a route exists, the command opens a NET/ROM circuit over the next hop. A direct session that fails stays failed. It is not retried as a circuit.

`crossbeam bulletin send` stays on a listed AX/UDP station, or on `fbb.peers` when the station is not in `link.peers`. Bulletins are not forwarded over a NET/ROM circuit.

One AX.25 session is open to a neighbor at a time. While a circuit is using that neighbor, a direct connect to the same callsign reports that it is already connected. The direct connect works after the circuit disconnects.

Connect by callsign. The six-character mnemonic in a broadcast is the callsign without an SSID. `K5LEB` and `K5LEB-1` remain different routes.

## Limits

A circuit carries at most 236 bytes of information in one frame. The window is 4. The time to live starts at 64, and a frame that would reach 0 is dropped. A request or information frame that is not acknowledged is sent up to 3 times, 60 seconds apart, and the circuit then fails. A quiet circuit disconnects after 15 minutes. At most 20 circuits are open at once.

The network frames use AX.25 PID CF. Node broadcasts are UI frames addressed to NODES. They do not open a session.
