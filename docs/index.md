# KC-LEGO-CLI-NEW

A LEGO collection manager for the terminal, built to **keep working when the internet doesn't**. It also carries tools for
ModernWMS and Part-DB inventories, and serves the same interface over **telnet** and a **web terminal**.

--8<-- "docs/assets/screens/lego-hub.html"

<div class="grid cards" markdown>

- **Offline-first**

    The whole Rebrickable catalog lives in a local database: parts, colours, sets, themes, minifigures and every set's
    parts list. Search, adding and "what am I missing" need no key and no network. [Offline mode](guides/offline-mode.md)

- **Lookup-first adding**

    Type a part number: name, category and the colours it comes in are filled in. You give colour and quantity.
    [Adding parts](guides/adding-parts.md)

- **Know what you can build**

    Sets your loose parts cover, shopping lists that count alternate and mould parts, BrickLink wanted lists.
    [Sets and building](guides/sets-and-building.md)

- **Pictures and detail pages**

    Part and set pictures drawn as text (half-blocks or ASCII) that work over telnet.
    [Search and pictures](guides/search-and-pictures.md)

- **BrickLink prices**

    Price guide, collection value and price alerts to your phone. [BrickLink](guides/bricklink.md)

- **Careful by default**

    Single-use 2FA with lockout, a tamper-evident audit log, encrypted backups, sandboxed plugins.
    [Security](guides/security.md)

</div>

## Where to start

1. [Getting started](getting-started.md): install it and take the first ten minutes.
2. Pick a guide from the menu when you need one.
3. Every command is in the [command reference](reference/cli/index.md), generated from the program's own `--help`.

!!! tip "Prefer scripts?"
    Everything has `--json`, `--quiet`, `--dry-run` and meaningful exit codes. See the
    [command reference](reference/cli/index.md).
