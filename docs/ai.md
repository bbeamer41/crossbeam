# AI assistant

Crossbeam can let a caller chat with an AI model that runs on a computer on your own network. Crossbeam talks to [Ollama](https://ollama.com) directly, with its own `/api/chat` interface and nothing in between. A server that speaks the OpenAI chat format, such as [Open WebUI](https://openwebui.com), LM Studio, or llama.cpp, works too. Nothing goes to the internet. Crossbeam sends the question to the address you save and nowhere else, and it never uses an HTTP proxy from the environment.

The assistant is off until you turn on `ai.enabled`. With the defaults, it asks Ollama on this computer, `http://127.0.0.1:11434`, for the model `llama3.2:3b`. You are still the control operator. Every reply goes out under this station's callsign, and every question and reply is written to the AI log so you can review it.

## On the air

A caller connects to the station prompt over packet and types `AI`. That starts a chat. The caller types a question, gets a short answer, and can ask a follow-up. The assistant remembers the last six questions and answers in this chat, and it forgets them when the caller leaves or the link drops.

```text
N0CALL-1>
AI
AI chat on N0CALL-1 using llama3.2:3b.
Replies are machine-made and may be wrong. Questions and replies
are logged for the control operator. Keep it legal for amateur radio.
/EXIT goes back to the BBS, BYE disconnects, /? for help.
AI>
What does a TNC do?
Thinking...
A TNC, or terminal node controller, turns data from a computer into
packet radio frames and back. It handles AX.25 so the computer does not
have to.
AI>
/EXIT
Back to the BBS.
N0CALL-1>
AI
...
AI>
BYE
73 de N0CALL-1, disconnecting.
```

`AI` and a question, such as `AI what is APRS`, starts the chat and asks that question at once.

In chat:

| Line | What it does |
|------|--------------|
| any text | Asks the assistant. |
| `/NEW` | Forgets this conversation and starts over. |
| `/STOP` | Cancels a question that is still waiting, or skips the rest of a long reply. |
| `/EXIT` or `/MENU` | Back to the station prompt. |
| `BYE`, `EXIT`, `QUIT`, `/BYE`, `/QUIT`, `/Q` | Signs off with `73 de CALL, disconnecting.` and disconnects, the same as `BYE` at the station prompt. |
| `/?` | Lists these commands. |

`BYE` and `/EXIT` also work while a question is waiting and between pages of a long reply. A waiting question is cancelled.

While the server works, the caller sees `Thinking...`, and `Still thinking...` every 30 seconds so a slow model does not look like a dead link. A line typed while waiting, including one typed at the moment the answer arrives, gets `Still thinking. Send /STOP to cancel.` It is not taken as the next question. `/STOP`, `/EXIT`, and `BYE` typed then still cancel or leave. A dropped link cancels the question on the AI server.

A reply is plain ASCII text, wrapped at 76 columns, with CR line endings like the rest of the prompt. Markdown marks and emoji are removed. A reply longer than `ai.reply` characters is cut at a word and ends with `[cut]`. A reply longer than 12 lines is sent in pages. Between pages the caller presses Enter for more, or sends `/STOP` to skip the rest.

`?` at the station prompt lists `AI` while the assistant is on.

## Who may use it

Nothing has to be listed. With `ai.users` empty, the default, any caller heard over the KISS radio may use the assistant. Crossbeam already checks that callsign with the FCC before the radio session opens. `ai.users` is optional. A callsign listed there may also use the assistant over the AX/UDP packet link or NET/ROM. The SSID does not matter: `W5ABC-7` counts as `W5ABC`.

Telnet has no callsign, so a telnet caller cannot use the assistant.

Each caller may ask `ai.hourly` questions an hour. At most two questions wait on the AI server at once. A third caller is told the server is busy. A down server, a timeout, or an error still uses one of those questions, so a stuck server is not hammered.

## Approving MeshCore and Reticulum senders

`ai.require_approval` is on by default. A MeshCore contact or a Reticulum sender must be on the approved list before a question reaches the model. Radio callers are not on this list. The license check already covers them, and this switch does not change who may use the assistant over AX.25.

For a sender, the first `AI` message does not reach the model. The station replies once: "Your request is waiting for sysop approval." That reply is sent at most once a day. Another message the same day gets no reply. The sender stays in a pending list until you decide.

For you, the pending list is on the settings page under AI senders, and in `crossbeam ai senders list`. Each row shows the network, the address, when the sender was first seen, and the first message, cut short so you can judge it. Approve lets them through. Block means every later message is ignored: no model, no waiting reply. Revoke takes someone off the approved list, so the next message waits for approval again. A blocked sender can be unblocked by approving them.

The list holds at most 200 pending senders, in `ai-senders.json` in the state directory, mode `0600`. When it is full, a new sender is not added and gets no reply at all. The approved list and the blocked list are kept in that same file, so a block survives a restart.

Turning `ai.require_approval` off makes MeshCore and Reticulum behave as they do with only `ai.mesh` and `ai.reticulum`. The blocked list is not checked while it is off.

```text
crossbeam ai senders list [--state DIR]
crossbeam ai senders approve meshcore:ADDRESS [--state DIR]
crossbeam ai senders deny reticulum:ADDRESS [--state DIR]
crossbeam ai senders revoke meshcore:ADDRESS [--state DIR]
```

An id is the network, a colon, and the address. `list` shows pending, approved, and denied. Approving, blocking, and revoking are written to the AI log as control-operator events.

## MeshCore

When `ai.mesh` is on, a MeshCore direct message that starts with `AI`, such as `AI what is a repeater`, gets an answer in that conversation. Other messages work as before: `LIST`, `READ`, and `?` are the station prompt, and a plain sentence gets the short greeting, which then also says to send AI and a question.

The assistant remembers the last few questions from each contact for 30 minutes, so a follow-up such as `AI and on 2 meters?` makes sense. `AI /NEW` starts over, and `AI` alone explains how to use it. An answer is at most 400 characters, split the same way as a bulletin, and a question waits at most 45 seconds. The companion link handles one message at a time, so other MeshCore messages wait while a question is answered.

MeshCore contacts are identified by their key, not a callsign, so `ai.users` does not apply. The hourly limit counts per contact. With `ai.require_approval` on, the contact must be approved first. See above.

## Reticulum

When `ai.reticulum` is on, an LXMF message sent to the station that starts with `AI` gets an answer. LXMF is the message format of Reticulum. Sideband, NomadNet, and MeshChat send it. A message that does not start with `AI` is ignored. While `ai.reticulum` is off, every message is ignored and nothing is sent back.

Crossbeam itself does not speak Reticulum. It keeps Reticulum in a separate program, as `docs/PLAN.md` says. A small bridge in `contrib/reticulum/crossbeam-lxmf-ai` runs Reticulum and LXMF. It announces an LXMF address with a display name such as `N0CALL AI`, passes each message whose first word is `AI` to the running Crossbeam over the local socket `ai.sock`, and sends the reply back over LXMF. Crossbeam makes every decision: the switch, the hourly limit, the filter, the log, and the station instructions are the same as on packet. The bridge keeps only a few of those messages in flight, so a burst is told the server is busy or is dropped, instead of being answered later.

With `ai.require_approval` on, that sender must be approved first, the same way as a MeshCore contact. Until then the bridge sends back the one waiting sentence, or nothing if the sender is blocked, the pending list is full, or they already heard the waiting sentence today.

The bridge drops an LXMF message whose signature does not validate. That includes a message with a bad signature and a message that is unsigned or from a source the bridge cannot check. Only a validated signature is answered.

A Reticulum user:

1. Finds `N0CALL AI` in the announce list in Sideband or NomadNet, or adds its LXMF address.
2. Sends `AI what is a TNC?`
3. Gets one LXMF message back with the answer.
4. Can ask a follow-up, such as `AI how fast is it?`. The assistant remembers the last few questions from that sender for 30 minutes. `AI /NEW` starts over.

### Set up the bridge

1. Install Reticulum and LXMF for the bridge, for example in its own Python environment:

   ```bash
   sudo python3 -m venv /opt/crossbeam-lxmf
   sudo /opt/crossbeam-lxmf/bin/pip install rns lxmf
   sudo install -m 0755 contrib/reticulum/crossbeam-lxmf-ai /usr/local/bin/crossbeam-lxmf-ai
   ```
2. Give the `crossbeam` user a Reticulum configuration, or run `rnsd` as a shared instance on this computer. See the [Reticulum manual](https://reticulum.network/manual/). The bridge uses the usual `~/.reticulum`, or `--rnsconfig DIR`.
3. Turn it on:

   ```bash
   sudo -u crossbeam crossbeam settings set ai.reticulum true --state /var/lib/crossbeam
   ```
4. Start the bridge as the `crossbeam` user, so it can open `/run/crossbeam/ai.sock`:

   ```bash
   sudo -u crossbeam /opt/crossbeam-lxmf/bin/python /usr/local/bin/crossbeam-lxmf-ai --socket /run/crossbeam/ai.sock --storage /var/lib/crossbeam-lxmf-ai --name "N0CALL AI"
   ```

   `contrib/reticulum/crossbeam-lxmf-ai.service` is an example systemd unit for the same command. The bridge prints its LXMF address when it starts, and it announces every 30 minutes. `--announce SECONDS` changes that.

The socket `ai.sock` is opened beside the admin socket at every start of `crossbeam serve`, with the same mode as `--socket-mode`. It takes one JSON object per line, `{"from":"<LXMF address in hex>","text":"AI ..."}`, and answers `{"reply":"..."}` or `{"ignore":true}`.

### Reticulum and Part 97

Reticulum is not an amateur radio system. It encrypts every LXMF message, and it runs over the internet, a LAN, LoRa radios under Part 15, and other links. Part 97 applies only to what goes out on an amateur frequency.

- Over the internet, a LAN, or Part 15 LoRa, Part 97 does not apply to these replies. Crossbeam still keeps the filter, the log, the hourly limit, and the station instructions, because the replies still go out under your station's name.
- **Do not carry these messages on an amateur radio interface.** If this computer's Reticulum has an interface on an amateur frequency, such as an RNode on the 70 cm or 33 cm band or a KISS or AX.25 interface on a ham TNC, LXMF replies can leave on that interface. Encrypted messages on an amateur frequency are not allowed (47 CFR 97.113(a)(4)), and Reticulum cannot send LXMF unencrypted. Use a separate Reticulum instance for the bridge with no amateur interfaces, or leave `ai.reticulum` off.

## Part 97 safeguards

The station licensee and the control operator are responsible for what this station sends on amateur frequencies (47 CFR 97.103 and 97.105). Crossbeam does these things for you on every path, packet, MeshCore, and Reticulum:

- **Off by default.** Nothing is sent to a model until you turn on `ai.enabled`. MeshCore and Reticulum each have their own switch, also off.
- **Licensed callers.** On packet, by default only radio callers, whose licenses the FCC lists as active, may ask.
- **Station instructions.** The model is always told to answer briefly in plain English and plain ASCII text, with no profanity or obscene or indecent language, no codes or ciphers, and no business or commercial dealings (97.113(a)). `ai.instructions` adds to those instructions. It does not replace them.
- **Filter.** A question with a word from a short list of profanity, a word from `ai.banned_words`, or a long unbroken token that looks like base64, hex, or other encoded text, is not sent to the model. A long ordinary word, a callsign, or a grid square is left alone. A reply with the same is withheld, and the caller is told it was withheld. The withheld reply is kept in the log for you.
- **No encryption on packet.** Packet replies go out as readable text on the same unencrypted session as the rest of the prompt (97.113(a)(4)). Reticulum is the exception, as described above.
- **Identification.** Every AX.25 frame carries this station's callsign, as the rest of the prompt does (97.119).
- **Log.** Every question, reply, refusal, and error is written to `ai-log.jsonl` in the state directory, mode `0600`, with the time, the caller, the path (`radio`, `link`, `netrom`, `meshcore`, or `reticulum`), and the model. When the log passes 4 MB it is moved to `ai-log.jsonl.1`, and one old log is kept. `crossbeam ai log` shows the last entries from both files.
- **Limits.** The hourly limit and the reply length keep one station from tying up the channel.

The filter is a backstop, not a guarantee. Read the log, choose a model you trust, and turn the assistant off if it misbehaves.

## Set up Ollama

Crossbeam talks to Ollama directly. Open WebUI is not needed.

### On the same computer

1. Install Ollama. See [ollama.com/download](https://ollama.com/download).
2. Download the default model:

   ```bash
   ollama pull llama3.2:3b
   ```
3. Check it and turn the assistant on:

   ```bash
   sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
   ```

The defaults, `ai.backend` `ollama`, `ai.url` `http://127.0.0.1:11434`, and `ai.model` `llama3.2:3b`, need no change.

### On another computer

1. Install Ollama on the computer that will run the model, and run `ollama pull llama3.2:3b` there. A computer with a recent graphics card answers fastest, but a small model runs on an ordinary CPU. `qwen2.5:1.5b` and `gemma3:1b` are smaller. `llama3.1:8b` gives better answers on a faster computer.
2. Let Ollama listen on the network. By default it listens only on its own computer. On Linux, run `sudo systemctl edit ollama` and add:

   ```ini
   [Service]
   Environment="OLLAMA_HOST=0.0.0.0:11434"
   ```

   Then run `sudo systemctl restart ollama`. On Windows, set a user environment variable `OLLAMA_HOST` to `0.0.0.0:11434` and restart Ollama. On a Mac, run `launchctl setenv OLLAMA_HOST "0.0.0.0:11434"` and restart Ollama.
3. Open port 11434 to the Crossbeam computer only. Ollama has no password. Do not forward this port from your router to the internet. For example, with `ufw`:

   ```bash
   sudo ufw allow from 192.168.1.20 to any port 11434 proto tcp
   ```

   On Windows, add an inbound rule for TCP port 11434 limited to the Crossbeam computer's address.
4. On the Crossbeam computer, save the address, check it, and turn the assistant on:

   ```bash
   sudo -u crossbeam crossbeam settings set ai.url http://192.168.1.50:11434 --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
   ```

`crossbeam ai test` says what to fix. When Ollama cannot be reached, it names the address and says to check that Ollama is running, that `OLLAMA_HOST=0.0.0.0:11434` is set on another computer, and that its firewall lets this computer in. When the model is missing, it says to run `ollama pull llama3.2:3b` on that computer. The first question after the model has been idle can take longer while Ollama loads it.

If `ai.url` is not a private or local address, `crossbeam ai test`, the line `crossbeam serve` prints at startup, and the settings page all warn: questions will be sent off your network. The address is still used. Private means a loopback, link-local, RFC1918, or 100.64.0.0/10 address, IPv6 unique-local, or a hostname ending in `.local` or `.ts.net` that resolves to one of those. Any other hostname is treated as public.

## Set up Open WebUI

Open WebUI is optional. It sits in front of Ollama and adds users and API keys.

1. In Open WebUI, open **Settings**, then **Account**, and create an API key. An admin may first have to allow API keys under **Admin Panel**, **Settings**, **General**.
2. Save the key on the Crossbeam computer. It is read without echo and stored in `ai-key` in the state directory, mode `0600`:

   ```bash
   sudo -u crossbeam crossbeam ai key --state /var/lib/crossbeam
   ```
3. Save the settings. The model name is the one Open WebUI lists:

   ```bash
   sudo -u crossbeam crossbeam settings set ai.backend openai --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.url http://192.168.1.50:3000/api/chat/completions --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
   ```
4. Open port 3000 to the Crossbeam computer only, as for Ollama.

LM Studio and llama.cpp's `llama-server` also use `ai.backend` `openai`. Use their base address ending in `/v1`, such as `http://192.168.1.50:1234/v1`.

`crossbeam ai key --clear` removes the key. The key is never shown on the panel, by `crossbeam settings`, by the MCP server, or in a log.

## Commands

```text
crossbeam ai key [--clear] [--state DIR]
crossbeam ai test [QUESTION] [--state DIR]
crossbeam ai log [-n COUNT] [--state DIR]
crossbeam ai senders list|approve ID|deny ID|revoke ID [--state DIR]
```

`ai test` sends one question with the saved settings, even while `ai.enabled` is off, and prints the answer or what to fix. It goes through the same filter and log as a caller's question. `ai log` prints the last entries of the AI log, 20 by default.

## Limits

- A small local model is often wrong. The banner says so, but callers will still believe it.
- Answers are slow on a CPU. At 1200 baud the air time is short next to the time the model takes.
- The assistant does not read the bulletin board or browse the web. It knows only what the model knows.
- Chat memory is kept in memory only: per session on packet, and per sender for 30 minutes on MeshCore and Reticulum.
- The hourly count is kept in memory and starts over when `crossbeam serve` restarts.
- The profanity list is short and English only.
- A MeshCore question holds up other MeshCore messages for up to 45 seconds.
- The Reticulum bridge is a separate Python program. It is not in the Debian package.

## Settings

| Key | Panel name | Default |
|-----|------------|---------|
| `ai.enabled` | AI assistant | `false` |
| `ai.backend` | AI server type | `ollama` |
| `ai.url` | AI server address | `http://127.0.0.1:11434` |
| `ai.model` | AI model | `llama3.2:3b` |
| `ai.users` | Who may use it | empty, radio callers |
| `ai.hourly` | Questions per hour | `20` |
| `ai.reply` | Longest reply | `600` |
| `ai.timeout` | Answer time limit | `90` |
| `ai.instructions` | Extra instructions | empty |
| `ai.banned_words` | Extra banned words | empty |
| `ai.mesh` | AI over MeshCore | `false` |
| `ai.reticulum` | AI over Reticulum | `false` |
| `ai.require_approval` | Approve MeshCore and Reticulum senders | `true` |

Each one is described in `docs/settings.md`. A saved change applies to the next question. No restart is needed.
