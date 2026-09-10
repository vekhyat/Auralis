# Auralis Verification Branding (userscript)

Purely **cosmetic** userscript that makes the upstream verification pages
(`api.zarz.moe`, `verify.spotbye.qzz.io`) — the browser tabs that open for
Cloudflare Turnstile checks — present as **Auralis** instead of SpotiFLAC.

It does not touch, bypass, or interact with verification logic. The captcha,
grants, and sessions all work exactly as before; only visible text/branding is
restyled.

## Install

1. Install [Tampermonkey](https://www.tampermonkey.net/) (Chrome/Edge/Firefox) or
   [Violentmonkey](https://violentmonkey.github.io/) — any userscript manager.
2. Open the Tampermonkey dashboard → **Utilities** → *Import from file*, or simply
   create a new script and paste the contents of `auralis-verify.user.js`.
3. Done. Next time a verification page opens, it will show Auralis branding.

Alternative (CSS only, no text rewriting): paste the `STYLE` block into the
[Stylus](https://add0n.com/stylus.html) extension scoped to those two domains.

## What it changes

| Page | Change |
|---|---|
| Zarz challenge (`api.zarz.moe/v2/challenge`) | Hides nav bar, donation box, "Powered by this API" section, footer; rewrites "SpotiFLAC-Mobile" → "Auralis" in visible text and tab title |
| SpotBye verify (`verify.spotbye.qzz.io/challenge`) | Hides logo block, tagline, "Buy Me a Coffee" button; rewrites title |

The Turnstile widget itself is untouched.

## Scope

`@match` is limited to exactly the two gateway domains Auralis uses. If you
self-host or the upstream domains ever change, update the `@match` lines.
