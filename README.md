# Sideboarder

A terminal UI ([Textual](https://textual.textualize.io/)) for planning **Magic: The
Gathering** sideboard guides — plus a mobile-friendly [web app](#web-app-mobile-over-tailscale)
served by a small Go server over Tailscale, working on the same files.

Load a decklist, list the opponent archetypes you expect, and for each matchup record
which cards come **OUT** of the mainboard and which come **IN** from the sideboard — with
optional **on-the-play / on-the-draw** adjustments. Everything saves to a single JSON file,
and a frequency report shows how often each card is boarded in or out across all matchups.

## Features

- **Import decklists** by pasting a Moxfield "MTGO" plaintext export (mainboard, blank line,
  sideboard). Set/collector annotations and `SB:` prefixes are tolerated.
- **Per-matchup plans** with a **Base** layer plus optional **on-the-play** and
  **on-the-draw** override deltas. The effective plan = base combined with the relevant
  override (quantities summed per card).
- **Sideboard rules check**: IN cards must come from your 15-card sideboard; the editor warns
  when a plan is unbalanced (OUT ≠ IN) or references a card not in the sideboard.
- **Frequency report** of OUT/IN counts per card, switchable between base and effective
  (play/draw) plans, with CSV export.
- **In-app deck editing**: replace a card or change quantities, with name autocomplete.
- **Card-name autocomplete** backed by a locally-cached card database (downloaded once from
  MTGJSON or Scryfall; updated manually from Settings; works fully offline afterward).
- **Tournament results tracking** (`t` toggles the view): log matches (date, event,
  opponent archetype, play/draw, games won-lost such as `2-1`, notes) into the same file,
  either through a new-result form or by editing table cells directly, with per-archetype
  records, game counts and winrates plus CSV export.
- **Deck revisions**: each result is pinned to the decklist revision it was played with.
  Editing or re-importing the deck after logging results automatically freezes the old
  list as a numbered snapshot in the same file. Deck edits alone never create a new
  revision — any number of composition changes count as one pending revision, which is
  finalized only when the revised deck is *used*: a sideboard plan is edited against it
  or a match result is recorded with it. `v` (or `enter` on the Rev cell) shows the
  exact decklist behind any result.
- **Single deck per file** (`*.sbd.json`), opened and saved individually.

## Install

```bash
pip install -e ".[dev]"   # from the repo root
```

Requires Python 3.11+.

## Run

```bash
sideboarder                 # start empty
sideboarder my-deck.sbd.json  # open an existing file
# or, without installing:
python -m sideboarder
```

## Web app (mobile, over Tailscale)

`web/` holds a Go server that serves the same features as a mobile-friendly web page, so
you can check and update sideboard plans and log results from your phone between rounds.
It reads and writes the **same `*.sbd.json` files** (and the same card-name database) as the
TUI, so the two can be used side by side.

```bash
./run-web.sh                       # build + serve on http://<tailnet-ip>:8080
# or
cd web && go build -o bin/sideboarder-web . && ./bin/sideboarder-web
```

Requires Go 1.22+ and no third-party Go modules; the HTML/JS/CSS is embedded in the binary.

| Flag         | Default                                  | Meaning                                    |
| ------------ | ---------------------------------------- | ------------------------------------------ |
| `-addr`      | `tailscale:8080`                         | Listen address. The host `tailscale` binds to this machine's tailnet IP only, so the page isn't reachable from the LAN or internet. Any other `host:port` is used as given. |
| `-dir`       | TUI's default save dir, else `./saves`   | Folder of `*.sbd.json` documents.          |
| `-cardnames` | the TUI's `cardnames.json`               | Card-name database for autocomplete.       |

Open `http://<machine-name>:8080` (MagicDNS) or `http://100.x.y.z:8080` from any device on
your tailnet, and use your Tailscale ACLs to decide who can reach it — the server itself has
no login. For HTTPS and a clean URL, bind to localhost and let Tailscale proxy it:

```bash
./run-web.sh -addr 127.0.0.1:8080
tailscale serve --bg 8080          # https://<machine-name>.<tailnet>.ts.net
```

On a phone, "Add to Home Screen" gives it an app icon. Differences from the TUI:

- **Every change is saved immediately** (there is no Save / unsaved state). Edits are sent as
  small operations applied to the latest file on disk, so a phone and a laptop can edit the
  same document; switching back to the tab reloads it.
- Documents are picked from the server's folder (**New**, **Open**, **Save a copy as**,
  **Download**) instead of arbitrary paths. CSV exports download to the browser.
- Matchups can be renamed and given notes, and each plan shows its **effective** play/draw
  lists. Deck name/format can be edited without re-importing.
- The card database update downloads MTGJSON's `.gz` file (the TUI uses `.xz`); the
  resulting `cardnames.json` is identical in format.

### Build mode: a 75 from your matchups

The web app's **Build** tab works backwards from the decks you want to play. For each opponent
archetype, enter its **share of the field** and the **60 cards you want after sideboarding**
(paste a list, or start from the current deck plus that matchup's plan). It then suggests a
60-card main deck, a 15-card sideboard, and the sideboard plan for every matchup.

How it chooses: each copy of each card is valued by the share of the field whose post-board deck
wants at least that many copies (the 3rd Path to Exile is worth 40% if matchups making up 40% of
the field play 3 or more). The 60 most valuable copies form the main deck and the next 15 the
sideboard. This minimises the share-weighted number of target cards you are missing, both in
game 1 (main deck only) and after boarding (main deck plus sideboard), and keeps the 4-copy
limit because no copy beyond what some target plays has any value. Ties go to the card with the
higher share-weighted average count.

For each matchup it shows how many target cards you'd have in game 1 and after boarding, how
many cards you swap, and anything the 75 can't supply. **Use this deck and plans** replaces the
deck and those matchups' base plans (play/draw changes are kept, and results already logged keep
the list they were played with). Shares are scaled to the matchups that have a post-board deck,
so they don't need to add up to 100%; with no shares entered every matchup counts equally.

Run the Go tests with `cd web && go test ./...`.

**Try it without a server:** `web/demo/build.sh` compiles the same server to WebAssembly and
writes a static site to `web/demo/dist/` (serve it with any static file server, e.g.
`python3 -m http.server -d web/demo/dist`). It opens with a sample deck and keeps documents
in the browser's localStorage instead of on disk.

## Keys

| Key      | Action                          |
| -------- | ------------------------------- |
| `i`      | Import / paste a decklist       |
| `a`      | Add an archetype                |
| `x`      | Remove the selected archetype   |
| `o`      | Open a file                     |
| `ctrl+s` | Save                            |
| `f`      | Frequency report                |
| `t`      | Toggle tournament results view  |
| `,`      | Settings (card DB source/update)|
| `?`      | Help                            |
| `ctrl+q` | Quit                            |

In the **plan editor**, pick the Base / On-the-play / On-the-draw layer, then **Add OUT** /
**Add IN**; with a list focused, `delete` removes the selected card and `+` / `-` change its
quantity. In the **deck pane**, focus a table and use `e` to edit/replace, `d` to delete,
`+` / `-` for quantity.

In the **results view** (`t`), `n` opens a form for a new match result, `enter` edits the
highlighted cell in place, `e` edits the whole row, and `d` deletes it; the stats pane shows
W-L-D, games won-lost and winrate per opponent archetype (draws excluded from winrate) plus an
overall line. A match is scored in **games** (`2-1`, `1-2`, `1-1`, `1-0`); the match outcome is
derived from that score, and the **P/D** column records whether you were on the play or the draw.
Rows are tinted by that outcome — green for a win, red for a loss, yellow for a draw (theme
colours, with a legend under the table).
Each row's **Rev** column names the deck revision it was played with — press `v` (or `enter`
on the Rev cell) to view that decklist. Deck edits made after results are logged snapshot the
old list automatically, so old results always point at the version they were actually played
with — but the edits themselves don't advance the revision number. However many cards you
swap, it stays one pending revision until the new deck is used: editing a sideboard plan or
logging a result finalizes it.

## Card database

Autocomplete needs a one-time download. Open **Settings** (`,`), choose a source
(**MTGJSON** default, or **Scryfall**), and click **Update card database now**. The full
dataset is distilled to a compact local name list under your platform data directory; the app
then autocompletes offline. Re-run the update whenever you want fresh card names. Without it,
the app still works — you just type names without suggestions.

## Data format

A document is one JSON file:

```json
{
  "schema_version": 1,
  "deck": {
    "name": "Mono-Red Burn",
    "format": "",
    "mainboard": [{"name": "Lightning Bolt", "qty": 4}],
    "sideboard": [{"name": "Smash to Smithereens", "qty": 3}]
  },
  "archetypes": [
    {
      "id": "…",
      "name": "Azorius Control",
      "notes": "",
      "base": {"out": [{"name": "Searing Blaze", "qty": 2}],
               "in":  [{"name": "Smash to Smithereens", "qty": 2}]},
      "play_override": {"out": [], "in": [{"name": "Roiling Vortex", "qty": 1}]},
      "meta_share": 18.5,
      "target_deck": [{"name": "Lightning Bolt", "qty": 4}, "…"]
    }
  ],
  "deck_revision": 2,
  "revisions": [
    {"revision": 1, "saved_at": "2026-08-05", "deck": {"name": "Mono-Red Burn", "…": "…"}}
  ],
  "results": [
    {
      "id": "…",
      "date": "2026-08-01",
      "event": "FNM",
      "archetype": "Azorius Control",
      "play_draw": "draw",
      "games_won": 2,
      "games_lost": 1,
      "deck_revision": 1,
      "notes": "close"
    }
  ]
}
```

## Development

```bash
pytest          # unit + Textual Pilot tests
ruff check src tests
```
