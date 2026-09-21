# Getting started

## Install

=== "Install script (Linux, macOS)"

    ```bash
    curl -fsSL https://raw.githubusercontent.com/KC-OU/KC-LEGO-CLI-NEW/main/scripts/install.sh | sh
    ```

    Downloads the latest release, **verifies its SHA-256**, checks the binary runs, and installs `wms-go` into `/usr/local/bin`.

=== "Package"

    Download the `.deb`, `.rpm` or `.apk` from the [releases page](https://github.com/KC-OU/KC-LEGO-CLI-NEW/releases)
    and install it with your package manager. It also installs the systemd units.

=== "Docker"

    ```bash
    docker run --rm ghcr.io/kc-ou/kc-lego-cli-new:latest version
    ```

    The image runs the sync engine (`wms sync serve`); the interactive interface needs a real terminal on the host.

=== "From source"

    ```bash
    go install github.com/KC-OU/KC-LEGO-CLI-NEW/cmd/wms@latest
    ```

    Needs Go 1.26 or newer. No C compiler.

Check the install and see what is set up:

```bash
wms-go doctor
```

`doctor` reports OK / WARN / FAIL for each thing it checks and says what to do about each. Warnings such as "Part-DB API
token: not set" are fine for now.

## The first ten minutes

### 1. Load the offline catalog

```bash
wms-go lego catalog refresh
```

One download of about 16 MB from Rebrickable's free data files (allowed once a day). From now on search and adding work with
no internet. No download possible? [Load it from files](guides/offline-mode.md#no-internet-at-all-load-the-files-by-hand).

### 2. Search

```bash
wms-go lego search sets falcon
wms-go lego search parts "brick 2 x 4"
```

### 3. Add a part

```bash
wms-go lego add-part 3001 --color red --qty 25
```

Or do it in the interface, which asks for each thing in turn: start it with `wms-go`, press **Ctrl-K**, type *add owned*.

--8<-- "docs/assets/screens/add-part-colour.html"

### 4. See what you could build

```bash
wms-go lego build
```

### 5. Open the interface

```bash
wms-go
```

Sign in with a ModernWMS or Part-DB account. Press **F1** for the keys on any screen and **Ctrl-K** to jump anywhere.

--8<-- "docs/assets/screens/hub.html"

## Optional next steps

| You want | Do |
|----------|----|
| Parts to appear in Part-DB | Create an Edit-level API token in Part-DB and paste it under *Admin → Settings & API Keys → Part-DB API Token*. Until then parts are saved locally and flagged *not in Part-DB yet*. |
| Live Rebrickable lookups | Add a free key under *Settings & API Keys → Rebrickable*. Not needed for anything offline. |
| Prices | Set up [BrickLink](guides/bricklink.md). |
| Alerts on your phone | [Alerts](guides/alerts.md). |
| Reach it from another machine | [Telnet and the web terminal](guides/telnet-and-web.md). |
| Import your old collection | `wms-go lego import` (the legacy JSON files) or `wms-go lego import-parts` ([Export and import](guides/export-import.md)). |
