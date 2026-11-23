# Guides

Read the first three pages in order. The others are for a station that is already running. Leave every setting you do not recognize empty. An empty setting is off.

Examples use `N0CALL` as a placeholder. Type the callsign on your license.

## The first three hours

| Hour | Page | You are done when |
| --- | --- | --- |
| 1. Install | [install.md](install.md) | The panel is open and you are signed in |
| 2. The board | [start.md](start.md) | Your callsign is saved and one bulletin is on the board |
| 3. The radio | [graywolf.md](graywolf.md) | Graywolf is listening and Crossbeam is dialing it |

The package installs Graywolf with Crossbeam. The radio hour starts Graywolf, gives it a channel, and then points Crossbeam at it.

A station that connects types `?`, then `LIST`, then `READ 1`, then `BYE`.

## Words

| Word | Meaning |
| --- | --- |
| Callsign | The address printed on your amateur license. It is the address of this station. |
| Bulletin | A note stored on the board. |
| Prompt | The line a connected station sees. It ends with `>`. |
| Sysop | You, after you sign in to the panel. |
| Modem | The program that turns text into packet tones and keys the radio. Graywolf is the supported modem. |
| KISS | The plain connection Crossbeam uses to talk to the modem. Crossbeam does not make the tones itself. |
| AX.25 | The packet connection. `LIST` and `READ` travel on it. |
| SSID | A dash and a number after the callsign, such as `-1`. It is a second address for the same license. |
| TNC | Another name for the modem. |

## When you want more

| When you want it | Page |
| --- | --- |
| Every setting, with the explanation from the panel | [settings.md](settings.md) |
| How a bulletin is stored, as text | [article.md](article.md) |
| Timing, the FCC check, and what the radio ignores | [radio.md](radio.md) |
| A direct connection to one other Crossbeam or LinBPQ node | [link.md](link.md) |
| Callers who connect with a telnet program, and FBB over TCP | [peers.md](peers.md) |
| Sending the bulletins to another BBS | [bulletin.md](bulletin.md) |
| A route through a neighbor station | [netrom.md](netrom.md) |
| The same bulletin as a NomadNet page | [pages.md](pages.md) |
| A MeshCore radio that answers LIST and READ | [meshcore.md](meshcore.md) |
| An AI assistant on the station prompt | [ai.md](ai.md) |
| A local MCP server for the article store | [mcp.md](mcp.md) |
| The build plan | [PLAN.md](PLAN.md) |
