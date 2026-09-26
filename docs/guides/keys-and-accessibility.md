# Keys, palette and accessibility

## Keys

| Key | Does |
|-----|------|
| **F1** or **?** | Key help for the current screen (on a form, only F1: a `?` there is text) |
| **Ctrl-K** or **F2** | Command palette |
| **F3, F12, Esc, Q** | Back (Q at the main menu signs out). On a form Q is an ordinary letter: type `q` and press Enter in the first field to leave, or use Esc |
| **F6** or **+** | Quick add |
| **F9** or **U** | Undo the last change made in this session |
| **F10** or **L** | Lock the session (it also locks itself after 15 idle minutes) |
| **G** | Jump between the main menu and LEGO Collection |
| **/** | Filter a list |

--8<-- "docs/assets/screens/help.html"

## The palette

Press **Ctrl-K** (or **F2** if your terminal swallows Ctrl-K), type a few letters of where you want to go, or a part or set number,
and press Enter. It lists screens, actions, your recent parts and, as you type, matching parts and sets from the offline catalog. It goes
through the same navigation as the menus, so a screen your role may not use bounces you with the usual message.

--8<-- "docs/assets/screens/palette.html"

## Accessibility

- **Themes:** press **Ctrl-K** and choose *My display theme* — ↑/↓ previews each one on a sample panel, **Enter** keeps it **for you**
  (every user can have their own; it is applied when you sign on). Admins also get *Admin → Settings → Display Theme*, where **D** makes
  the highlighted theme **everyone's default** (`MODERNWMS_TUI_THEME`). The themes: `green` (default), `amber`, `high-contrast` (nothing
  dimmed, reverse video for problems), `colorblind` (blue and orange instead of green and red), `dracula`, `half-life` (matches the zsh
  theme), `nord`, `gruvbox`, `catppuccin`, `tokyo-night`, `ibm-3270`, `matrix` and `lego`.
- After sign-on a short **themed animation** plays (an HEV boot for half-life, a brick for lego, falling glyphs for matrix); any key
  skips it and `MODERNWMS_TUI_SPLASH=0` turns it off.
- **My loading indicator** (also in the palette, next to *My display theme*): a spinner on the message line for calls that actually
  take time — signing on, a Part-DB sync, a BrickLink price look-up. On by default; turning it off only hides the spinner text, it
  never brings back a frozen-looking screen — those calls always run in the background either way.
- Hub and section menus (Main Menu, Set Workshop, LEGO Collection, ...) are shown in a rounded, theme-coloured panel, the same one
  the sign-on card uses. A menu with too many options to fit the panel's two extra border lines on your terminal falls back to the
  plain list automatically (e.g. the LEGO Collection hub, at 80x24) — nothing is ever cut off to make room for the border.
- **Every parts list you browse** — Owned Parts, a set's parts check, and Missing Parts & Prices in Set Workshop — is sorted by
  colour (black, red, blue, then alphabetically), then by part category within each colour, so a specific part is easy to find.
  There's no banner row breaking it up: real part categories are far more granular than "Bricks"/"Plates" (Rebrickable splits
  them down to things like "Technic Axles" and "Technic Beams"), so a banner per category turned into more bars than parts —
  the plain table with the improved order reads better. (A set's Missing Parts *report* stays sorted by biggest shortfall
  first, for shopping — that's deliberate, not a screen this missed.)
- A set coming up short on a check also **pops up on screen** while you're using the app (not just the external notification it
  already sent) — **Q** dismisses it; nothing else does, and a second one queues rather than replacing or being lost.
- **`NO_COLOR=1`** turns colour off entirely; pictures switch to ASCII.
- In the high-contrast, colour-blind and no-colour modes **OK / FAIL / WARN are spelled out** beside the icons, and **LOW** is always
  spelled out, so meaning never depends on colour alone.
- Screens fit an 80x24 window (the classic terminal size; smaller than that is not supported). Larger windows are used when the client reports its size, and long lists page with PgUp/PgDn.
