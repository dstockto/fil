Fil is a command line tool for managing filament in a 3D printer using spoolman.

# Commands:

---

find (f) - find a spool based on a partial name/color match, shows where it is and how much is left

> $ fil f 'muted red'
```
Found 1 spools matching 'muted red':
 - AMS A - #20 PolyTerra™ Muted Red (Matte PLA #DB3E14) - 891.7g remaining, last used 1 hours ago
```
You can provide a partial match, and you can specify multiple partial matches. Each individual partial match will be 
handled separately. 

> $ fil f 'marble' 'blue' 'muted green'
```aiignore
Found 2 spools matching 'marble':
 - Shelf 7C - #16 Marble Brick (Marble PLA #c65454) - 966.9g remaining, last used 61 days ago
 - Shelf 6C - #140 Panchroma Marble Limestone (Marble PLA #9f9090) - 1000.0g remaining, last used never

Found 11 spools matching 'blue':
 - Shelf 6A - #90 PolyTerra™ Muted Blue (Matte PLA #4E6A84) - 276.1g remaining, last used 35 days ago
 - AMS C - #145 PolyTerra™ Army Blue (Matte PLA #062B4D) - 787.3g remaining, last used 6 days ago
 - Shelf 2C - #74 PanChroma™ Matte Sky Blue (Matte PLA #1ac5fc) - 787.6g remaining, last used 26 days ago
 - AMS B - #124 PolyTerra™ Sapphire Blue (Matte PLA #005aa2) - 890.7g remaining, last used 7 days ago
 - Shelf 2C - #76 Blue (PLA+ #201def) - 1000.0g remaining, last used never
 - Shelf 2A - #66 Blue Ombré (PLA #) - 1000.0g remaining, last used never
 - Shelf 2A - #59 Blue Ombré (PLA #) - 1000.0g remaining, last used never
 - Shelf 5B - #136 Panchroma Silk Blue (PLA Silk #3609e9) - 1000.0g remaining, last used never
 - Shelf 7A - #14 PolyTerra™ Muted Blue (Matte PLA #4E6A84) - 1000.0g remaining, last used never
 - Shelf 6B - #31 PolyTerra™ PLA+ Blue (Matte PLA #342de7) - 1000.0g remaining, last used never
 - Shelf 7C - #143 Polylite PLA Pro Metallic Blue (PLA Pro #2c3449) - 1000.0g remaining, last used never

Found 2 spools matching 'muted green':
 - Shelf 6B - #1 PolyTerra™ Muted Green (Matte PLA #656D60) - 200.3g remaining, last used 14 days ago
 - Shelf 3A - #125 PolyTerra™ Muted Green (Matte PLA #656D60) - 1000.0g remaining, last used never
```

If you know the ID of the spool, you can provide it to get that single spool:
> $ fil f 42
```aiignore
Found 1 spool with ID #42:

 - ████ Shelf 1A - #42 Black (2.85mm) (PLA #060505) - 500.0g remaining, last used never
```

You can specify a name of '*' (with the quotes) to return all spools.
> $ fil f '*'

You may filter based on the filament manufacturer (partial matches) with the -m / --manufacturer flag. The manufacturer
is case insensitive and will apply to all name matches. The -m will not apply to ID matches.
> $ fil f -m 'poly' 'red' 'blue'
```aiignore
Found 5 spools matching 'red':

 - ████ AMS A - #20 PolyTerra™ Muted Red (Matte PLA #DB3E14) - 880.1g remaining, last used 5 hours ago
 - ████ Shelf 5B - #37 PolyTerra™ Army Red (Matte PLA #bf312e) - 413.3g remaining, last used 8 days ago
 - ████ Shelf 6B - #12 PolyTerra™ Lava Red (Matte PLA #DE1619) - 971.6g remaining, last used 14 days ago
 - ████ Shelf 6C - #139 Polylite PLA Pro Metallic Red (PLA Pro #c92626) - 991.6g remaining, last used 26 days ago
 - ████ Shelf 7A - #154 PolyTerra™ Army Red (Matte PLA #bf312e) - 1000.0g remaining, last used never

Found 8 spools matching 'blue':

 - ████ AMS B - #124 PolyTerra™ Sapphire Blue (Matte PLA #005aa2) - 890.7g remaining, last used 7 days ago
 - ████ AMS C - #145 PolyTerra™ Army Blue (Matte PLA #062B4D) - 787.3g remaining, last used 7 days ago
 - ████ Shelf 2C - #74 PanChroma™ Matte Sky Blue (Matte PLA #1ac5fc) - 787.6g remaining, last used 27 days ago
 - ████ Shelf 5B - #136 Panchroma Silk Blue (PLA Silk #3609e9) - 1000.0g remaining, last used never
 - ████ Shelf 6A - #90 PolyTerra™ Muted Blue (Matte PLA #4E6A84) - 276.1g remaining, last used 35 days ago
 - ████ Shelf 6B - #31 PolyTerra™ PLA+ Blue (Matte PLA #342de7) - 1000.0g remaining, last used never
 - ████ Shelf 7A - #14 PolyTerra™ Muted Blue (Matte PLA #4E6A84) - 1000.0g remaining, last used never
 - ████ Shelf 7C - #143 Polylite PLA Pro Metallic Blue (PLA Pro #2c3449) - 1000.0g remaining, last used never
```

By default only 1.75mm filament is returned. You can specify a different diameter with the -d option. If you specify a
diameter that is not '2.85' or '*' then it will use '1.75' as the default.
> $ fil f 'marble' -d 2.85
```aiignore
Found 1 spools matching 'marble':

 - ████ Polydryers - #49 Parthenon Gray (Marble) (2.85mm) (PLA PRO #898181) - 1000.0g remaining, last used never
```
> $ fil f '*' -d '*'
```aiignore
Returns all filament, regardless of diameter.
```

You can include archived spools with the -a / --archived flag.
> $ fil f 'charcoal' -a
```aiignore
Found 4 spools matching 'charcoal':

 - ████ AMS A - #36 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 726.4g remaining, last used 6 hours ago
 - ████ AMS B - #123 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 0.0g remaining, last used 27 days ago (archived)
 - ████ Shelf 4A - #126 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 1000.0g remaining, last used never
 - ████ Top Shelf - #6 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 0.0g remaining, last used 56 days ago (archived)
```

If you want to see only archived spools, you can use the --archived-only flag.
> $ fil f 'charcoal' --archived-only
```aiignore
Found 2 spools matching 'charcoal':

 - ████ AMS B - #123 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 0.0g remaining, last used 27 days ago (archived)
 - ████ Top Shelf - #6 PolyTerra™ Charcoal Black (Matte PLA #1C1C1C) - 0.0g remaining, last used 56 days ago (archived)
```

To filter spools that have a comment, use the -c / --comment flag. The -c will not apply to ID matches. It will match 
on the comment, not the name.
> $ fil f '*' -c bad
```aiignore
Found 1 spools matching '*':

 - ████ Shelf 7B - #128 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
 ```
---

If you don't care about the content of the comment, you can use the --has-comment flag.
> $ fil f '*' --has-comment
```aiignore
Found 1 spools matching '*':

 - ████ Shelf 7B - #128 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
```

To filter spools that have been used, at least some, use the -u / --used flag. The -u will not apply to ID matches.
> $ fil f 'white' -u
```aiignore
Found 3 spools matching 'white':

 - ████ AMS B - #127 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 91.5g remaining, last used 2 days ago
 - ████ AMS C - #70 PolyTerra™ Muted White (Matte PLA #AFA198) - 814.9g remaining, last used 5 hours ago
 - ████ Shelf 4B - #129 White (PLA #C7CDD7) - 936.2g remaining, last used 38 days ago
```

To filter spools that have not been used, use the -p / --pristine flag. The -p will not apply to ID matches.
> $ fil f 'white' -p
```aiignore
Found 8 spools matching 'white':

 - ████ Shelf 1C - #130 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
 - ████ Shelf 1D - #131 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
 - ████ Shelf 2C - #78 Bone White (PLA+ #c2b9af) - 1000.0g remaining, last used never
 - ████ Shelf 2C - #79 PLA-Matte MILKY WHITE (PLA #dfdbd8) - 1000.0g remaining, last used never
 - ████ Shelf 2D - #132 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
 - ████ Shelf 5A - #118 PolyTerra™ Muted White (Matte PLA #AFA198) - 1000.0g remaining, last used never
 - ████ Shelf 5A - #117 PolyTerra™ Muted White (Matte PLA #AFA198) - 1000.0g remaining, last used never
 - ████ Shelf 7B - #128 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 1000.0g remaining, last used never
```

You can filter by the location of the spool using the -l / --location flag. The -l will not apply to ID matches. The 
location can be a partial case-insensitive match. Use 'ams' to find all spools in AMS.
> $ fil f '*' -l 6b
```aiignore
Found 7 spools matching '*':

 - ████ Shelf 6B - #23 PolyTerra™ Electric Indigo (Matte PLA #9917e4) - 178.0g remaining, last used 6 days ago
 - ████ Shelf 6B - #1 PolyTerra™ Muted Green (Matte PLA #656D60) - 200.3g remaining, last used 15 days ago
 - ████ Shelf 6B - #19 PolyTerra™ Forest Green (Matte PLA #519F61) - 316.3g remaining, last used 7 days ago
 - ████ Shelf 6B - #12 PolyTerra™ Lava Red (Matte PLA #DE1619) - 971.6g remaining, last used 15 days ago
 - ████ Shelf 6B - #86 Panchroma™ Matte (Formerly PolyTerra™) Lime Green (PLA #d0e740) - 1000.0g remaining, last used never
 - ████ Shelf 6B - #32 PolyLite™ Silk Bronze (Matte PLA #a9470a) - 1000.0g remaining, last used never
 - ████ Shelf 6B - #31 PolyTerra™ PLA+ Blue (Matte PLA #342de7) - 1000.0g remaining, last used never
```

---

Use (u) - Mark a specified amount of a spool as used. You can specify several spools at once by repeating the spool id and amount.
For negative amounts (like undoing a usage), you'll need to use `--` before you start doing the negative amount. Otherwise
it will think you're trying to give a flag that does not exist. Filament amounts will be rounded to the nearest 0.1g.
---
> $ fil u 106 -- -2.01 106 2.01
```aiignore
- Unusing spool #106 [Green - eSun] (-2.0g of filament) - 993.8g remaining.
- Marking spool #106 [Green - eSun] as used (2.0g of filament) - 991.8g remaining.
```

If you tell fil to use more filament than the spool has remaining, you'll get an error:
> $ fil u 127 104.5
Not enough filament on spool #127 [PolyTerra™ Cotton White - Polymaker] (only 91.5g available).
Error: not enough filament on spool #127 [PolyTerra™ Cotton White - Polymaker] (only 91.5g available)
exit status 1
---
If you did tell fil to use more than one filament, the other ones that have enough will succeed, but you'll still see an
error and a non-zero exit code.

# Ideas:

Find options:
- Show spools that are in AMS's (in the right order)
- Filtering by filament type (partial match?)
- Add purchase link in normal find (with switch)
    
Move options:
- Allow changing of position within a location???? (to line up where stuff is in the AMS)
  Other options (ideas, not implemented):
- -v / --verbose - show more info about a spool or spools (like info command)
- -t / --template - allow customizable templates for output
- allow customizable templates for output

Other uses for this tool:
- Figure out what filaments are running low and show them

move (m) - move a spool from one location to another, allows for aliased locations for ease of use
> $ fil m 20 A - (A could be an alias for AMS A)
```
Moved #20 Polymaker Muted Red from Shelf 6B to AMS A
```

Use ideas:
- `fil -m -f <limit search> <spool selector> <amount> <spool selector> <amount>...`
- --summary prints totals per filament and overall weight used/remaining.

Interactive selection for move and use:
- When a spool selector matches more than one spool, `fil move` and `fil use` will now open an interactive selector to pick the intended spool.
- The selector supports arrow keys and live filtering; start typing to filter the list. The initial label shows your query.
- You can choose from all available spools within the current filters (e.g., `--from`/`--location`), not only the initial matches.
- If you cancel at any selection prompt (Esc/Ctrl+C), the entire command is aborted and no changes are made.
- In dry-run mode, prompts still appear unless you also pass `--non-interactive`.
- To disable prompting and keep the prior behavior (error on ambiguous match), pass `--non-interactive` (alias `-n`).
  - Interaction is also disabled automatically when stdout/stderr are not TTYs (e.g., when piping output).

Examples:
- `fil move "muted green" A:2` → if multiple spools match "muted green", you will be prompted to pick one before applying the move.
- `fil use "cotton white" 2.5` → prompts to choose the exact spool, then applies the usage.
- `fil use -n "blue" 5.0` → non-interactive; if multiple "blue" spools match, prints an error and does nothing.


Provide a way to archive spools.
Special output when using the last bit of a spool (when it goes empty)
Find spools that have more or less than a specified amount of filament.
Make the movement of spools be consistent with the spool ordering data (it now has the same spool id in multiple locations)
Moving filament locations does not remove it from the old location, it just adds it to the new location. I suspect spoolman iterates the whole 
deal and only renders the spool if the spool's location matches the location it is iterating over (from the settings/location json value). Cleaning this up could be a good thing.
Perhaps a periodic cleanup job that removes spools from locations that are no longer in use instead of keeping it clean all the time?

---

Low (reorder) - Show filaments that are running low so you know what to reorder. By default, it shows 1.75mm filaments with
200g or less remaining. You can filter by manufacturer. Archived spools are excluded.

> $ fil low '*'
```aiignore
Filaments running low matching '*': 3
 - ████ Shelf 6B - #23 PolyTerra™ Electric Indigo (Matte PLA #9917e4) - 178.0g remaining, last used 6 days ago
 - ████ AMS B - #127 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 91.5g remaining, last used 2 days ago
 - ████ Shelf 6B - #1 PolyTerra™ Muted Green (Matte PLA #656D60) - 200.3g remaining, last used 15 days ago
```

Flags:
- --max-remaining: grams threshold (default 200). Set to 0 to disable grams threshold.
- -d, --diameter: 1.75 (default), 2.85, or '*' for all.
- -m, --manufacturer: filter by filament manufacturer.

Configuration lookup and overrides:
- If you do not pass --config, fil will merge configs from these locations (later entries override earlier):
  1) $HOME/.config/fil/config.json
  2) $XDG_CONFIG_HOME/fil/config.json
  3) ./config.json (current working directory)
- Pass --config <path> to use a single explicit config file instead.

Custom thresholds via config:
- You can set per-filament custom low thresholds in your config.json using the `low_thresholds` map. Keys are matched case-insensitively and support two forms:
  1) "NamePart" → match by filament name substring
  2) "VendorPart::NamePart" → match only when both the manufacturer/vendor and the filament name contain the given substrings
  If a key matches, its value (in grams) overrides `--max-remaining` for that filament.
- Example config.json snippet:
  {
    "low_thresholds": {
      "Charcoal Black": 1000,
      "Bambu::Orange": 1500
    }
  }
- Notes:
  - When using the specific form, both sides are substring matches; whitespace is ignored around the :: separator.
  - If no "::" is present, the pattern matches by name only.
  - The first matching key found is used. Values <= 0 are ignored.

Examples:
> $ fil reorder --max-remaining 150
> $ fil low -m Polymaker
> $ fil low '*' -d '*'

---

Project Management - Manage complex 3D printing projects involving multiple plates and filaments.

`fil plan list` - List all discovered plans and their completion status. Plans are discovered from the current directory and the `plans_dir` configured in `config.json`.

`fil plan new [-m]` - Create a new template plan YAML file in the current directory. The project name is taken from the current directory name. If STL files are present, they are added as plates.
- `-m, --move`: Automatically move the created plan to the `plans_dir` central location.

`fil plan move [file]` - Move a YAML plan file from the current directory to the `plans_dir` central location.

`fil plan check [file]` - Check if enough filament is on hand across all spools in Spoolman to complete the pending plates in the plan(s).

`fil plan resolve [file]` - Interactively link human-readable filament names in a plan to specific Spoolman Filament IDs.

`fil plan next` - Interactively recommend the next plate to print based on currently loaded filaments in your printers (minimizing swaps). Provides step-by-step unload/load instructions.

`fil plan complete [file]` - Mark a plate or project as completed and optionally record filament usage in Spoolman.

`fil plan archive [file]` - Move completed plan files to the `archive_dir` configured in `config.json`.

---

Ignoring retired filaments via config:
- You can exclude certain filaments from appearing in `fil low` by listing patterns in `low_ignore` within your config.json.
  - Simple form: "NamePart" → matches by filament name substring (case-insensitive).
  - Specific form: "VendorPart::NamePart" → matches only when both the manufacturer/vendor AND the filament name contain the given substrings (case-insensitive). This lets you retire something like Bambu "Orange" without ignoring "Sunrise Orange" from other vendors.
- Example config.json snippet:
  {
    "low_ignore": [
      "Charcoal Black",
      "Bambu::Orange"
    ]
  }
- Notes:
  - Ignored entries are excluded from low evaluation in both single-spool and grouped modes.
  - When using the specific form, both sides are substring matches; whitespace is ignored around the :: separator.
  - If no "::" is present, the pattern matches by name only.
