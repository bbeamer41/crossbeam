# AI assistant

Crossbeam can let a caller chat with an AI model that runs on a computer on your own network. The model runs in [Ollama](https://ollama.com), or in any server that speaks the OpenAI chat format, such as [Open WebUI](https://openwebui.com), LM Studio, or llama.cpp. Nothing goes to the internet. Crossbeam sends the question to the address you save and nowhere else, and it never uses an HTTP proxy from the environment.

The assistant is off until you turn on `ai.enabled`. You are still the control operator. Every reply goes out under this station's callsign, and every question and reply is written to the AI log so you can review it.

## On the air

A caller connects to the station prompt over packet and types `AI`. That starts a chat. The caller types a question, gets a short answer, and can ask a follow-up. The assistant remembers the last six questions and answers in this chat, and it forgets them when the caller leaves or the link drops.

```text
K5LEB-1>
AI
AI chat on K5LEB-1 using llama3.2:3b.
Replies are machine-made and may be wrong. Questions and replies
are logged for the control operator. Keep it legal for amateur radio.
Send /EXIT to go back, /NEW to start over, /? for help.
AI>
What does a TNC do?
Thinking...
A TNC, or terminal node controller, turns data from a computer into
packet radio frames and back. It handles AX.25 so the computer does not
have to.
AI>
Is 1200 baud the usual speed on 2 meters?
Thinking...
Yes. 1200 baud AFSK is the usual packet speed on 2 meters. 9600 baud is
used on some UHF links.
AI>
/EXIT
Back to the BBS.
K5LEB-1>
```

`AI` and a question, such as `AI what is APRS`, starts the chat and asks that question at once.

In chat:

| Line | What it does |
|------|--------------|
| any text | Asks the assistant. |
| `/NEW` | Forgets this conversation and starts over. |
| `/STOP` | Cancels a question that is still waiting, or skips the rest of a long reply. |
| `/EXIT`, `/BYE`, `/QUIT`, `/Q`, `EXIT`, `BYE`, `QUIT` | Back to the station prompt. In chat, `BYE` does not disconnect. A second `BYE` at the prompt does. |
| `/?` | Lists these commands. |

While the server works, the caller sees `Thinking...`, and `Still thinking...` every 30 seconds so a slow model does not look like a dead link. A line typed while waiting gets `Still thinking. Send /STOP to cancel.` A dropped link cancels the question on the AI server.

A reply is plain ASCII text, wrapped at 76 columns, with CR line endings like the rest of the prompt. Markdown marks and emoji are removed. A reply longer than `ai.reply` characters is cut at a word and ends with `[cut]`. A reply longer than 12 lines is sent in pages. Between pages the caller presses Enter for more, or sends `/STOP` to skip the rest.

`?` at the station prompt lists `AI` while the assistant is on.

## Who may use it

With `ai.users` empty, only a caller heard over the KISS radio may use the assistant. Crossbeam already checks that callsign with the FCC before the radio session opens. A callsign listed in `ai.users` may also use it over the AX/UDP packet link or NET/ROM. The SSID does not matter: `W5ABC-7` counts as `W5ABC`.

Telnet has no callsign, so a telnet caller cannot use the assistant.

Each callsign may ask `ai.hourly` questions an hour. At most two questions wait on the AI server at once. A third caller is told the server is busy.

## MeshCore

When `ai.mesh` is on, a MeshCore direct message that starts with `AI`, such as `AI what is a repeater`, gets one answer in that conversation. There is no chat memory over MeshCore. The answer is at most 400 characters, split the same way as a bulletin, and the question waits at most 45 seconds. MeshCore contacts are identified by their key, not a callsign, so `ai.users` does not apply. The hourly limit counts per contact. MeshCore usually runs under Part 15 rather than Part 97, but the same filter and log apply.

## Part 97 safeguards

The station licensee and the control operator are responsible for what this station sends (47 CFR 97.103 and 97.105). Crossbeam does these things for you:

- **Off by default.** Nothing is sent to a model until you turn on `ai.enabled`.
- **Licensed callers.** By default only radio callers, whose licenses the FCC lists as active, may ask.
- **Station instructions.** The model is always told to answer briefly in plain English and plain ASCII text, with no profanity or obscene or indecent language, no codes or ciphers, and no business or commercial dealings (97.113(a)). `ai.instructions` adds to those instructions. It does not replace them.
- **Filter.** A question with a word from a short list of profanity, or a long run of what looks like base64 or hex, is not sent to the model. A reply with the same is withheld, and the caller is told it was withheld. The withheld reply is kept in the log for you.
- **No encryption.** Replies go out as readable text on the same unencrypted session as the rest of the prompt (97.113(a)(4)).
- **Identification.** Every AX.25 frame carries this station's callsign, as the rest of the prompt does (97.119).
- **Log.** Every question, reply, refusal, and error is written to `ai-log.jsonl` in the state directory, mode `0600`, with the time, callsign, path, and model. When the log passes 4 MB it is moved to `ai-log.jsonl.1`, and one old log is kept. `crossbeam ai log` shows the last entries.
- **Limits.** The hourly limit and the reply length keep one station from tying up the channel.

The filter is a backstop, not a guarantee. Read the log, choose a model you trust, and turn the assistant off if it misbehaves.

## Set up Ollama on another computer

1. Install Ollama on the computer that will run the model. See [ollama.com/download](https://ollama.com/download). A computer with a recent graphics card answers fastest, but a small model runs on an ordinary CPU.
2. Download a model. A small model is quick enough for packet:

   ```bash
   ollama pull llama3.2:3b
   ```

   `qwen2.5:1.5b` and `gemma3:1b` are smaller. `llama3.1:8b` gives better answers on a faster computer.
3. Let Ollama listen on the network. By default it listens only on its own computer. On Linux, run `sudo systemctl edit ollama` and add:

   ```ini
   [Service]
   Environment="OLLAMA_HOST=0.0.0.0:11434"
   ```

   Then run `sudo systemctl restart ollama`. On Windows, set a user environment variable `OLLAMA_HOST` to `0.0.0.0:11434` and restart Ollama. On a Mac, run `launchctl setenv OLLAMA_HOST "0.0.0.0:11434"` and restart Ollama.
4. Open port 11434 to the Crossbeam computer only. Ollama has no password. Do not forward this port from your router to the internet. For example, with `ufw`:

   ```bash
   sudo ufw allow from 192.168.1.20 to any port 11434 proto tcp
   ```

   On Windows, add an inbound rule for TCP port 11434 limited to the Crossbeam computer's address.
5. From the Crossbeam computer, check that the server answers:

   ```bash
   curl http://192.168.1.50:11434/api/tags
   ```
6. Save the settings on the panel, or with the command:

   ```bash
   sudo -u crossbeam crossbeam settings set ai.url http://192.168.1.50:11434 --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.model llama3.2:3b --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
   ```

The first question after the model has been idle can take longer while Ollama loads it.

## Set up Open WebUI

Open WebUI sits in front of Ollama and adds users and API keys.

1. In Open WebUI, open **Settings**, then **Account**, and create an API key. An admin may first have to allow API keys under **Admin Panel**, **Settings**, **General**.
2. Save the key on the Crossbeam computer. It is read without echo and stored in `ai-key` in the state directory, mode `0600`:

   ```bash
   sudo -u crossbeam crossbeam ai key --state /var/lib/crossbeam
   ```
3. Save the settings. The model name is the one Open WebUI lists:

   ```bash
   sudo -u crossbeam crossbeam settings set ai.backend openai --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.url http://192.168.1.50:3000/api/chat/completions --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.model llama3.2:3b --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam ai test --state /var/lib/crossbeam
   sudo -u crossbeam crossbeam settings set ai.enabled true --state /var/lib/crossbeam
   ```
4. Open port 3000 to the Crossbeam computer only, as for Ollama.

LM Studio and llama.cpp's `llama-server` also use `ai.backend` `openai`. Use their base address ending in `/v1`, such as `http://192.168.1.50:1234/v1`. Ollama's own OpenAI address, `http://host:11434/v1`, works too.

`crossbeam ai key --clear` removes the key. The key is never shown on the panel, by `crossbeam settings`, by the MCP server, or in a log.

## Commands

```text
crossbeam ai key [--clear] [--state DIR]
crossbeam ai test [QUESTION] [--state DIR]
crossbeam ai log [-n COUNT] [--state DIR]
```

`ai test` sends one question with the saved settings, even while `ai.enabled` is off, and prints the answer. It goes through the same filter and log as a caller's question. `ai log` prints the last entries of the AI log, 20 by default.

## Limits

- A small local model is often wrong. The banner says so, but callers will still believe it.
- Answers are slow on a CPU. At 1200 baud the air time is short next to the time the model takes.
- The assistant does not read the bulletin board or browse the web. It knows only what the model knows.
- Chat memory is kept in memory only, per session.
- The hourly count is kept in memory and starts over when `crossbeam serve` restarts.
- The profanity list is short and English only.
- There is no AI over NomadNet yet. NomadNet pages are files, and Crossbeam does not run a NomadNet node.

## Settings

| Key | Panel name | Default |
|-----|------------|---------|
| `ai.enabled` | AI assistant | `false` |
| `ai.backend` | AI server type | `ollama` |
| `ai.url` | AI server address | empty |
| `ai.model` | AI model | empty |
| `ai.users` | Who may use it | empty, radio callers |
| `ai.hourly` | Questions per hour | `20` |
| `ai.reply` | Longest reply | `600` |
| `ai.timeout` | Answer time limit | `90` |
| `ai.instructions` | Extra instructions | empty |
| `ai.mesh` | AI over MeshCore | `false` |

Each one is described in `docs/settings.md`. A saved change applies to the next question. No restart is needed.
