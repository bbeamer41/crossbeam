# Article format

An article is the one copy Crossbeam stores. Rendering does not change that copy. Importing a rendering returns the same article.

| Field | Required | Rules |
| --- | --- | --- |
| Title | yes | One line, at most 200 runes. Leading and trailing spaces are removed. |
| Author | yes | One line, at most 64 runes. This is the station callsign, such as `K5LEB`. |
| Created | yes | A date and time. Stored in UTC and truncated to one second. Written as RFC3339, for example `2026-10-11T18:04:05Z`. |
| ID | no | One line, at most 80 runes. Omitted from both renderings when empty. |
| Body | no | Plain text, at most 256 KiB. Paragraphs are separated by a blank line. Words inside a paragraph are separated by single spaces. Line breaks inside a paragraph are for display and are joined back on import. |

All text is UTF-8. Control characters are rejected in every field, including C1 controls such as U+0085. Format characters are rejected too, including zero-width characters and bidi controls, so a callsign or a line cannot hide extra characters. In the body, tab, vertical tab, form feed, and carriage return are whitespace, and carriage return is turned into a newline before paragraphs are split. A Unicode line separator (U+2028) or paragraph separator (U+2029) is rejected in every field. Other Unicode spaces in the body, including a non-breaking space, become a normal space.

A bulletin or a page passed to a parser may be at most 1 MiB. A header line may be at most 1024 bytes. Anything larger is rejected before it is parsed.

Created is stored only when that UTC second can be written as RFC3339 and read back as the same instant. The year is four digits, from 0000 through 9999. An offset is converted to UTC. A fractional second is truncated.

The body width for both renderings is 72 runes of text. A word longer than 72 stays on one line. Header lines are not wrapped. On a Micron page the body is also indented two spaces, and those spaces are not part of the 72.

## Packet bulletin

Plain text. Each header is `Name: value`. A blank line ends the header. The body follows, wrapped to 72.

```
Title: Net night notes
Author: K5LEB
Created: 2026-10-11T18:04:05Z
ID: k5leb-001

The Denton net meets on Thursday.
```

`ID` is left off when the article has no ID. An empty body is the header, a blank line, and nothing else. The text ends with a newline.

Header names are case-sensitive and may appear in any order. A repeated header, or a header other than `Title`, `Author`, `Created`, and `ID`, is an error. Values are trimmed. The first blank line ends the header, so a body line that reads `Title: …` stays in the body. `ParsePacket` reads a bulletin written by `RenderPacket`.

## NomadNet Micron page

The title is a level-1 heading. The other fields are plain lines. A line that is exactly `----` marks the start of the body. Body lines are indented two spaces, so NomadNet does not treat a line the author wrote as a heading, a comment, or a divider. A backslash or a backtick is written with a backslash in front of it (`\\` and `` \` ``). That is Micron's own escape: the page shows the original character, and import removes the extra backslash. `ParseMicron` reads a page written by `RenderMicron`. It is not a general Micron parser.

```
> Net night notes
Author: K5LEB
Created: 2026-10-11T18:04:05Z
ID: k5leb-001

----
  The Denton net meets on Thursday.
```

## Code

`internal/article` implements this.

- `Article.Canonical` checks and normalizes an article.
- `Article.RenderPacket` and `ParsePacket` are the bulletin.
- `Article.RenderMicron` and `ParseMicron` are the page.

`internal/page` writes that Micron rendering into a directory and reads it back. See `docs/pages.md`.

`go test ./...` checks that both renderings of one article import back to that article, including backticks, backslashes, and lines that look like Micron markup.
