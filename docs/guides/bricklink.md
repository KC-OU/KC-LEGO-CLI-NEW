# BrickLink

BrickLink is **optional**. Everything except prices works without it. What it adds:

| Feature | Command |
|---------|---------|
| Look an item up **by number** (part, set, minifigure) | `wms-go bricklink item set 75192` |
| **Price guide** (average sold, or current listings) | `wms-go bricklink price part 3001 --color red` |
| **Where used** (which sets contain a part) | `wms-go bricklink where-used 3001` |
| **Collection value** | `wms-go lego value --refresh 200` |
| **Price alerts** | `wms-go lego watch add part 3001 --color red --max 0.05` |

!!! warning "BrickLink has no search by name"
    Its API looks things up by number. To search by name use `wms-go lego search` (offline catalog).

## Set it up

You need a free BrickLink account. **Whether an account without a store can register an API consumer is up to BrickLink**; if the
registration page refuses you, everything else here still works.

1. **Find this server's public IP.** BrickLink ties your token to one IP address.

    ```bash
    wms-go bricklink whoami        # asks api.ipify.org; the only command that contacts anything but Rebrickable/BrickLink
    ```

2. **Register an API consumer** at <https://www.bricklink.com/v2/api/register_consumer.page> for that IP. A static address is safest.
   Using `0.0.0.0` (any address) works from a changing IP but means anyone with your four values can use them.
3. BrickLink shows **four values**: consumer key, consumer secret, token value, token secret. Enter them (hidden as you type):

    ```bash
    wms-go bricklink configure     # or Admin → Settings & API Keys → BrickLink API in the interface
    ```

4. It tests them straight away. Later: `wms-go bricklink test` and `wms-go bricklink status`.
5. Map colours once (one call): `wms-go bricklink colors sync`.

A wrong IP shows as `TOKEN_IP_MISMATCHED`; the message points you back to `whoami`. `wms-go doctor` checks the token.

## Limits, kept for you

BrickLink allows 5,000 calls a day. The tool keeps a **hard daily budget of 4,500** (`BRICKLINK_DAILY_BUDGET`, never above 5,000), shared
by every session, so the account can't be blocked; when it is used up, cached answers still work and you are told. Catalogue answers are
cached for 30 days and prices for a day. Requests are spaced 250 ms apart.

## Prices are honest

- Currency and region are `BRICKLINK_CURRENCY` (default `GBP`) and `BRICKLINK_REGION` (default `europe`).
- **No data is not "free".** If nothing sold in the window the tool says "no sales", never a price of 0.
- **Collection value** is worked out from stored prices and always shown as *N of M lines priced*, with a note that the total is a floor.
  `--refresh N` fetches up to N missing or week-old prices (oldest first, inside the daily budget); run it daily to fill the picture in.

## Price alerts

`wms-go lego watch add part 3001 --color red --max 0.05` (or `set 75192 --max 550 --condition N`). The gateway checks watches once a day
(and `wms-go lego watch check` checks now). A watch alerts when the average sold price is at or below your limit, and again only if it falls
further within a week. Alerts go to your phone through [Alerts](alerts.md); plugins can react to the `price_drop` event.
