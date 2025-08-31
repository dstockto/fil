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

> $fil f 'marble' 'blue' 'muted green'
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
> $fil f 42
```aiignore
Found 1 spool with ID #42:

 - ████ Shelf 1A - #42 Black (2.85mm) (PLA #060505) - 500.0g remaining, last used never
```

You can specify a name of '*' (with the quotes) to return all spools.
> $fil f '*'

You may filter based on the filament manufacturer (partial matches) with the -m / --manufacturer flag. The manufacturer
is case insensitive and will apply to all name matches. The -m will not apply to ID matches.
> $fil f -m 'poly' 'red' 'blue'
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
> $fil f 'marble' -d 2.85
```aiignore
Found 1 spools matching 'marble':

 - ████ Polydryers - #49 Parthenon Gray (Marble) (2.85mm) (PLA PRO #898181) - 1000.0g remaining, last used never
```
> $fil f '*' -d '*'
```aiignore
Returns all filament, regardless of diameter.
```


---
move (m) - move a spool from one location to another, allows for aliased locations for ease of use
> $ fil m 20 A - (A could be an alias for AMS A)
```
Moved #20 Polymaker Muted Red from Shelf 6B to AMS A
```
Consider allowing multiple moves in one command?
fil m 20 A 45 6c 13 6c 12 B
Would move #20 -> AMS A, #45 -> Shelf 6C, #12 -> AMS B in one command

---
info (i) - Show more info about a spool or spools - should allow for spool ID or for partial matches
> $ fil i muted
```
Found 9 spools:
- #1  PolyTerra™ Muted Green (Shelf 6B) - 199g remaining, last used 8/15/2025
- #14 PolyTerra™ Muted Blue (Shelf 7A) - 1000g remaining, never used
....
```
Consider a -v (verbose) flag that will give all the info: Spool location, remaining, used, last used, filament ID, comment
Need to figure out how to display these and if it would be useful
---
> $ fil used
Show how much filament has been used total
Flags:
- -a / --archived - include archived spools
- -f / --by-filament - instead of showing just a total for all, show total by filament
---
use (u) - Marks filament as used
> $ fil u 20 43.5
```
Used 43.5g of Polymaker Muted Red in AMS A. 654.5g remaining.
```

---

Ideas:

Find options:
- Show spools that are in AMS's
- Filtering by filament type (partial match?)
- Allow filtering by location (with some special stuff for "All AMS")
- Allow changing of position within a location???? (to line up where stuff is in the AMS)
- Handle multi-color filaments (maybe?)
  Other options (ideas, not implemented):
  -a - include archived filaments
  --archived-only - only show archived spools
  -c / --comment = <string> - find spools based on comment (or that have a comment)? - how to do with requiring an argument?
  -f / --filament-id = <digits> - find spools based on filament id (figure out with args?)
  -u / --used - used only? - only show used spools?
  -p / --pristine - only show unused?
- -l / --location - show spools by location (with some special stuff for "All AMS")
- -m / --manufacturer - filter by manufacturer (partial match?)
- -s / --sort - sort by filament type (partial match?) ???
- -v / --verbose - show more info about a spool or spools (like info command)
- -d / --diameter - show spools by diameter (2.85mm, 1.75mm, or all)
- allow for matches on 2.85mm filament (1.75mm is default) (-d option?)