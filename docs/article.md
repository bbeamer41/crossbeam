# Article format

An article is the one copy Crossbeam stores. Rendering does not change that copy. Importing a rendering returns the same article.

| Field | Required | Rules |
| --- | --- | --- |
| Title | yes | One line. Leading and trailing spaces are removed. |
| Author | yes | One line. This is the station callsign, such as `K5LEB`. |
| Created | yes | A date and time. Stored in UTC and truncated to one second. Written as RFC3339, for example `2026-10-11T18:04:05Z`. |
| ID | no | One line. Omitted from both renderings when empty. |
| Body | no | Plain text. Paragraphs are separated by a blank line. Words inside a paragraph are separated by single spaces. Line breaks inside a paragraph are for display and are joined back on import. |

Control characters are rejected in Title, Author, and ID.

The body width for both renderings is 72 runes. A word longer than 72 stays on one line. Header lines are not wrapped.

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

## NomadNet Micron page

The title is a level-1 heading. The other fields are plain lines. A `----` divider marks the start of the body. Body lines are indented two spaces, so a line the author wrote cannot become a heading or a divider. A backslash or a backtick in any text field is written with a backslash in front of it (`\\` and `` \` ``). Import reverses that.

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

`go test ./...` checks that both renderings of one article import back to that article, including backticks, backslashes, and lines that look like Micron markup.
