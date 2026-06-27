# Sideboarder

A terminal UI ([Textual](https://textual.textualize.io/)) for planning **Magic: The
Gathering** sideboard guides.

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

## Keys

| Key      | Action                          |
| -------- | ------------------------------- |
| `i`      | Import / paste a decklist       |
| `a`      | Add an archetype                |
| `x`      | Remove the selected archetype   |
| `o`      | Open a file                     |
| `ctrl+s` | Save                            |
| `f`      | Frequency report                |
| `,`      | Settings (card DB source/update)|
| `?`      | Help                            |
| `ctrl+q` | Quit                            |

In the **plan editor**, pick the Base / On-the-play / On-the-draw layer, then **Add OUT** /
**Add IN**; with a list focused, `delete` removes the selected card and `+` / `-` change its
quantity. In the **deck pane**, focus a table and use `e` to edit/replace, `d` to delete,
`+` / `-` for quantity.

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
      "play_override": {"out": [], "in": [{"name": "Roiling Vortex", "qty": 1}]}
    }
  ]
}
```

## Development

```bash
pytest          # unit + Textual Pilot tests
ruff check src tests
```
