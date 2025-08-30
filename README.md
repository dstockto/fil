Fil is a command line tool for managing filament in a 3D printer using spoolman.

# Commands:

---

find (f) - find a spool based on a partial name/color match, shows where it is and how much is left

> $ fil f 'muted red'
```
- AMS A - #20 Polymaker Muted Red - 894g remaining, last used 18 days ago
```
results should list most used first
Other options:
-a - include archived filaments
-f / --filament-id = <digits> - find spools based on filament id
-u / --used - used only?
-p / --pristine - not used?

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
