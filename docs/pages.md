# NomadNet pages

A stored article can be written as a Micron page, and a Micron page can be read back into the same article. The page text is the rendering in `docs/article.md`. NomadNet is a separate program. Crossbeam reads and writes the page files. It does not start NomadNet, and it does not speak Reticulum.

## When the directory is down

Saving an article does not need NomadNet or a network connection. The article stays in the store. Set `nomad.pages` to the pages directory, such as `/var/lib/nomadnetwork/storage/pages`, when that directory exists. Leave the setting empty to keep articles in the store only.

`crossbeam page publish` and `crossbeam page ingest` read the saved directory each time. If the setting is empty, the command reports `nomad.pages is empty`. If the directory is missing or cannot be used, the command reports `the page directory is not available`. Neither command creates the directory. Run publish again after the directory is present. The articles are still in the store.

## Commands

These commands open the state database directly.

```bash
crossbeam page publish --state /var/lib/crossbeam
crossbeam page ingest --state /var/lib/crossbeam
```

Publish writes every stored article. Ingest reads the top of the pages directory. A file in a subdirectory stays where it is.

Publish prints `wrote N`. Ingest prints `read N`. When some pages cannot be written or imported, the count of those pages is printed on standard error and the command exits 1. A file that is not a Crossbeam page is skipped, and the command still exits 0 when nothing else failed.

The command output does not include the directory path.

## Files

A page Crossbeam writes is named `cb-<row>.mu`, where `<row>` is the article row. The file mode is `0644`. NomadNet runs a `.mu` file when that file is executable, so a page written here is a normal file and is served as text.

The file is written under a temporary name that starts with a dot, then renamed into place. NomadNet skips names that start with a dot. `index.mu` is left unchanged. Publish replaces `cb-<row>.mu` for each article still in the store and leaves every other file, including a page whose article was deleted.

A symbolic link is not followed, including when the pages directory itself is a symbolic link. Replacing a page does not change the file a symbolic link points at. Hidden files are skipped. A file larger than 1 MiB is skipped. A file that is not this program's Micron page is skipped, including `index.mu` and a page written by another program.

Ingest updates an article when the file name is `cb-<row>.mu` and that row still exists. Otherwise it updates the one article with the same ID. When the page has no ID, it updates the one article with the same title, author, and created time. Anything else is inserted. Running ingest again updates that article. Deleting an article leaves the page file in place. A later ingest stores the page again when the row is gone.

An operator can edit the `.mu` file. The next ingest updates the matching article. Restart NomadNet when it should notice a new page. Crossbeam does not restart it.
