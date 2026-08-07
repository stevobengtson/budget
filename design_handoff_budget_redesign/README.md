# Handoff: Pigglet budget app redesign

## Overview

A redesign of the Pigglet web app (`github.com/sbengtson/budget` — Go + Gin + Templ + HTMX + templUI/Tailwind). It covers the two tasks that matter most — **entering transactions** and **understanding the budget at a glance** — plus Paydown, category/goal management, account editing, confirmations, transfers, first run, and the phone layout.

The redesign replaces the current six-numeric-column budget table with **two numbers per row** (Assigned as an editable field, Available as the headline), moves every total out of the table into a left summary rail, flags overspending loudly, and turns transaction entry into an **always-open quick-add row** at the top of the list.

## About the Design Files

The files in this bundle are **design references created in HTML** — prototypes showing intended look and behaviour. They are **not production code to copy**.

The task is to **recreate these designs inside the existing codebase's environment**: Go + Templ components under `internal/web/views/`, styled with Tailwind v4 against the token sheet in `internal/web/tailwind/theme.css`, with HTMX for partial updates and templUI components where they fit. Use the repo's established patterns (`*_templ.go` regenerated via `task templ`, `task css` for Tailwind) rather than importing anything from these HTML files.

The prototypes are built as streaming "Design Component" HTML with inline styles and a design-system stylesheet. Read them for layout, values and copy — then express the same result as Templ + Tailwind classes.

## Fidelity

**High-fidelity.** Final colours, typography, spacing, radii, shadows, states and copy. Recreate the UI to match. Every value in this document is the value used in the prototype.

Two exceptions that are deliberately unresolved and marked ⚠ below: the ⌘K command palette's contents, and the "Cover the overspend" action's backend.

---

## Design Tokens

The design uses a warm, cream-grounded system ("Organic") rather than the current shadcn `maia/green/mist` theme. `styles.css` in this bundle is the source of truth for the values.

### How to port them — swap values, keep names

**Do not delete the shadcn token names.** Every vendored templUI component (button, input, dialog, selectbox, card…) reads `--primary`, `--background`, `--card`, `--border`, `--ring`, `--muted-foreground`. Removing them breaks all of them at once.

Instead **keep the shadcn variable names in `theme.css` and change only their values**, then add the Organic-specific tokens alongside. Every templUI component re-skins itself for free, and `signedmoney` stays the single point of change for money colour.

| shadcn token | New value |
| --- | --- |
| `--background` | `#f5ead8` |
| `--foreground` | `#201e1d` |
| `--card` / `--popover` | `#ebddc5` |
| `--primary` | `#c67139` |
| `--primary-foreground` | `#f5ead8` |
| `--secondary` / `--muted` / `--accent` | `#eee7db` |
| `--muted-foreground` | `#645c50` |
| `--border` / `--input` | `#dcd3c4` |
| `--ring` | `#c67139` |
| `--destructive` | `#8c491a` |

Then add, as new tokens: the three ramps (`--color-neutral-100…900`, `--color-accent-100…900`, `--color-accent-2-100…900`), the 1.10× spacing scale, and the fonts.

**Radius is the one thing that cannot be value-swapped.** templUI computes `sm`/`md`/`lg` off a single `--radius`, so setting it to `999px` would make cards pill-shaped too. Keep `--radius: 16px` as the base and give the pill shapes their own token:

```css
--radius: 16px;          /* templUI's base — cards, dialogs, panels */
--radius-pill: 999px;    /* buttons, inputs, tags, segmented controls */
```

Apply `--radius-pill` explicitly on those four component classes rather than globally.

### Core roles

| Role | Value |
| --- | --- |
| Page ground | `#f5ead8` |
| Raised surface (rail, cards, sheets) | `#ebddc5` |
| Text | `#201e1d` |
| Accent (primary, terracotta) | `#c67139` |
| Accent 2 (sage) | `#7a8a5e` |
| Divider | `#201e1d` at 16% |

### Neutral ramp

`100 #f9f4ed` · `200 #eee7db` · `300 #dcd3c4` · `400 #c0b6a5` · `500 #a19786` · `600 #82796a` · `700 #645c50` · `800 #474238` · `900 #2e2b25`

### Accent ramp (terracotta)

`100 #fff2eb` · `200 #ffe1d0` · `300 #ffc6a5` · `400 #f6a06b` · `500 #d67f48` · `600 #b2622d` · `700 #8c491a` · `800 #643312` · `900 #402310`

### Accent-2 ramp (sage)

`100 #f0fae1` · `200 #e1eecc` · `300 #ccdbb2` · `400 #aebf92` · `500 #8fa073` · `600 #728157` · `700 #56633f` · `800 #3d472b` · `900 #272e1b`

### Semantic mapping — use these, not raw ramp steps

| Meaning | Colour |
| --- | --- |
| Money positive / available and healthy | `#56633f` (accent-2-700) |
| Money negative / overspent / amount owing | `#8c491a` (accent-700) |
| Money exactly zero | `#645c50` (neutral-700) |
| Muted / secondary text | `#645c50` (neutral-700) — **never** `opacity` on the cream ground |
| Overspent row background | `#ffe1d0` (accent-200) |
| "Needs attention" card | `#ffe1d0` (accent-200), text `#643312` (accent-800) |
| "Ready to assign" card | `#f0fae1` (accent-2-100), text `#56633f` |
| Quick-add card | `#fff2eb` (accent-100) |
| Progress fill, healthy | `#8fa073` (accent-2-500) |
| Progress fill, overspent | `#8c491a` (accent-700), always at 100% width |
| Progress track | `#201e1d` at 12% |

There is no red in the palette. Overspending is terracotta — the accent — reinforced by a tinted row background and a worded note, so it never relies on colour alone.

### Dark mode

The repo already has the plumbing — `theme-toggle.js` wired in `layout.templ`, a `.dark` block in `theme.css`, and `dark:` utilities across the views including `signedmoney.templ`. **Keep all of it.** The Organic stylesheet ships no dark ramp, so the mapping below is the missing half; drop it into the existing `.dark` block and the toggle keeps working unchanged.

A warm dark, not a grey one — the ground stays on the same brown-cream axis so the palette keeps its character. **No new hues**: the dark accents are steps of the same two ramps. Nothing about layout, spacing or type changes.

| Role | Light | Dark |
| --- | --- | --- |
| Ground | `#f5ead8` | `#1c1a17` |
| Raised surface (rail, cards, tab bar) | `#ebddc5` | `#2e2b25` (neutral-900) |
| Sunk (inputs, segmented track) | `#f5ead8` | `#252220` — **new role, dark only** |
| Ink | `#201e1d` | `#f5ead8` |
| Muted text | `#645c50` (neutral-700) | `#c0b6a5` (neutral-400) |
| Divider | text at 16% | `rgba(245,234,216,0.14)` |
| Negative / overspent / owing | `#8c491a` (accent-700) | `#f6a06b` (accent-400) |
| Positive / available | `#56633f` (accent-2-700) | `#aebf92` (accent-2-400) |
| Zero | `#645c50` | `#c0b6a5` |
| Overspent row bg / Needs attention | `#ffe1d0` (accent-200) | `#402310` (accent-900) |
| Ready to assign card | `#f0fae1` (accent-2-100) | `#272e1b` (accent-2-900) |
| Progress track | text at 12% | `rgba(245,234,216,0.14)` |
| Progress fill, healthy | `#8fa073` (accent-2-500) | unchanged |
| Progress fill, overspent | `#8c491a` (accent-700) | `#d67f48` (accent-500) |
| Primary button | fill `#c67139`, label `#f5ead8` | fill `#d67f48` (accent-500), label `#1c1a17` |
| Secondary / ghost button | as in the system | transparent, `1px` border `rgba(245,234,216,0.22)`, ink label |
| Input border | `#201e1d` at 30% | `rgba(245,234,216,0.22)` |
| `kbd` chip | text at 8% | `rgba(245,234,216,0.12)`, label `#c0b6a5` |
| Avatar | `#ffe1d0` / `#643312` | `#402310` / `#f6a06b` |

Three rules that are not just a colour swap:

1. **Tinted fills invert across the ramp.** A fill moves from the 100–200 steps to 900, and the text on it from 700–800 to 400 — the same contrast relationship, flipped. Do not reuse a light-mode pair on a dark ground.
2. **Shadows stop carrying elevation.** On the dark ground a shadow is nearly invisible, so surfaces separate by lightness instead: ground → surface → sunk. Only the screen container keeps `--shadow-lg`; cards inside it get `box-shadow: none` and rely on their background. The quick-add card, which is a tint in light mode, instead takes a `1px #402310` border.
3. **Primary buttons keep a dark label.** The fill steps *down* to accent-500 rather than up, because accent-400 text on accent-500 would not hold.

Muted text is `neutral-400` — still a real ink colour, never an `opacity` fade. Semantic meaning is unchanged in both modes: terracotta is money going the wrong way, sage is money you have.

**Implementation:** every value above is already a custom property. Scope the overrides to `:root[data-theme="dark"]` and remap **the semantic roles only** — no component needs its own dark rules. Let `prefers-color-scheme` set the initial value and persist an explicit override; server-render the attribute on `<html>` to avoid a flash. Screens are shown in `6a`–`6c` of the redesign file.

### Spacing scale

`space-1 4.4px` · `space-2 8.8px` · `space-3 13.2px` · `space-4 17.6px` · `space-6 26.4px` · `space-8 35.2px`

(1.10× density baked in — use the scale, not round numbers.)

### Radius

`sm 8px` · `md 16px` · `lg 28px` · cards and dialogs `32.2px` (`lg × 1.15`) · **buttons, tags, inputs and segmented controls `999px`**

### Shadows

| Level | Value |
| --- | --- |
| sm | `0 1px 2px rgba(46,43,37,0.14)` |
| md | `0 3px 10px rgba(46,43,37,0.16)` |
| lg | `0 12px 32px rgba(46,43,37,0.22)` |

Screens use `md`, cards inside them `sm`, sheets/dialogs/menus `lg`.

### Typography

Two families, loaded from Google Fonts:

- **Headings — Source Serif 4, weight 600.** A text serif, chosen after the display face (Caprasimo) tested as hard to read.
- **Body — Figtree**, 400/600/700.

| Role | Size / weight / family |
| --- | --- |
| Page heading | 32–34px / 600 / serif |
| Screen title | 28px / 600 / serif |
| Panel title (Transactions, Edit account) | 22–24px / 600 / serif |
| Big number (Ready to assign, stat cards) | 34–40px / 600 / serif, tabular |
| Group header | 17px / 600 / serif |
| Body base | 16px / 400 / Figtree, line-height 1.55 |
| Row name | 16px / 600 |
| Money in a row | 17px / 700, tabular |
| Money in a rail card | 15px / 700, tabular |
| Secondary / meta text | 14px / 400, `#645c50` |
| Micro label (column heads, kickers, day headers) | 12px / 700, uppercase, letter-spacing 0.08em, `#645c50` |
| Progress note | 12px / 400, `#645c50` |
| `kbd` chip | 11px / 600, `#201e1d` at 8% background, radius 6px, padding 2px 6px |

**Every money figure uses `font-variant-numeric: tabular-nums`.** Mobile drops the base to the same 16px but raises the quick-add amount to 22px and the assign-sheet amount to 26px.

### Icons

Lucide, **stroke-width 2.75**, `currentColor`. Sizes: 17–18px in buttons and rows, 20px in the omnibox, 22px in the mobile tab bar, 16px for grips and ticks.

Icons used: `wallet`-style card glyph (Budget), three-line list (Transactions/Activity), `trending-down` (Paydown), `chevron-left/right`, `chevron-right` (disclosure), `grip-vertical`, `ellipsis-vertical`, `check`, `arrow-right`, `arrow-left-right` (swap), `plus`, `alert-circle`.

### Interaction states — from the design system, do not restyle per screen

| State | Treatment |
| --- | --- |
| Keyboard focus | `outline: 2px solid #c67139; outline-offset: 2px` (inputs: `outline-offset: 0` and `border-color: #c67139`) |
| Primary button hover / active | `#b2622d` / `#8c491a` |
| Secondary button hover / active | text at 7% / 14% |
| Ghost button hover / active | accent at 10% / 18% |
| Input hover | border `#201e1d` at 45% |
| Disabled | 45% opacity, `cursor: not-allowed` |
| Selected segment | accent fill `#c67139`, label `#f5ead8` |

Never leave a default browser focus ring.

---

## Screens / Views

All desktop screens share a shell: a **72px icon rail** on the left (`#ebddc5`, padding `17.6px 0`, items centred, gap `13.2px`, brand mark `P` at 22px serif in `#8c491a`, avatar pinned to the bottom with `margin-top:auto`), and a content pane on `#f5ead8` with `26.4px` padding. Screen corners `28px`, shadow `md`.

The current app instead uses a 20rem templUI inset sidebar carrying the whole accounts panel. In the redesign that panel moves into the Budget screen's summary rail on desktop and under the Budget tab on mobile.

### 1. Budget (primary screen)

**Purpose:** see whether the month is on track, and assign money.

**Layout:** rail 72px · content pane in two columns with `26.4px` gap — **summary rail 300px fixed**, category list `flex:1`.

**Summary rail, top to bottom:**

1. Month navigation — two 36px icon buttons (secondary, pill) with the month centred, `22px` serif, `flex:1; text-align:center`.
2. **Ready to assign** card — background `#f0fae1`, radius `32.2px`, padding `13.2px`, shadow `sm`, gap `8.8px`. Micro label "Ready to assign" in `#56633f`; value `$2,295.00` at 40px serif, line-height 1.1, `#56633f`, tabular; primary block button "Assign the rest" with a `kbd` `A`.
3. **Totals** card — background `#ebddc5`. Four rows, `15px`, label `#645c50` left / value 700 tabular right: Income, Assigned, Spent, then a `1px` divider and "Still in envelopes" with the value in `#56633f`.
4. **Needs attention** card — background `#ffe1d0`, all text `#643312`. Micro label, then one row per problem: "Gas overspent −$30.50", "Chase Sapphire owing $15.77". Omit the card entirely when there is nothing wrong.

**Category list:**

- Column head row: grid `1fr 140px 200px 140px`, gap `17.6px`, padding `0 13.2px 8.8px`, micro-label style. Labels: Category · Assigned (right) · Progress · Available (right).
- **Group header:** flex, `13.2px 13.2px 4.4px` padding. Name at `17px` serif; group's total available at `13px`/700 tabular, coloured by sign.
- **Category row:** same grid, `align-items:center`, padding `9px 13.2px`, radius `16px`.
  - Name — `16px`/600, truncated with ellipsis.
  - Assigned — `input`, height 36px, pill, right-aligned, 15px/600 tabular, background `#f5ead8`.
  - Progress — track 8px tall, radius 999px, background text-at-12%; fill radius 999px, width `spent / assigned` capped at 100%, colour by state.
  - Available — `17px`/700 tabular, coloured by sign.
  - Overspent rows additionally get background `#ffe1d0`.
- Row order and grouping match the current `BudgetGroupTbody` / `BudgetRow` output.

**Footer:** shortcut strip, `13px`, `#645c50`, above a `1px` divider: `J`/`K` move · `⏎` assign · `N` new transaction · `[`/`]` month · `⌘K` anything.

**Below the fold on the same screen:** the Transactions block (see next), separated by a `1px` divider and `26.4px` of padding. Budget and recent activity live on one page so entering a transaction never costs a navigation.

### 2. Transactions

**Purpose:** enter transactions fast; scan what happened.

**Header:** title `22px` serif left; on the right a segmented control filtering by account (All accounts / Main Checking / Chase Sapphire) plus the month icon buttons.

**Quick-add row** — the centrepiece. A card, background `#fff2eb`, shadow `sm`, gap `13.2px`, containing a single wrapping flex row (`gap: 8.8px`, `align-items: flex-end`) of labelled fields. Each field is a 12px/600 `#645c50` label above a pill input.

Expense mode, left to right:

| Field | Width | Notes |
| --- | --- | --- |
| Type | — | segmented control Expense / Income / Transfer |
| Date | 110px | defaults to "Today" |
| Amount | 124px | right-aligned, 700, tabular, border `#c67139` (it holds focus on open) |
| Payee | `flex:1`, min 150px | |
| Category | 170px | |
| Account | 180px | |
| Add | — | primary button with `kbd` `⏎` |

Helper line beneath, `13px` `#645c50`: "⏎ save and start the next · ⇥ next field · Payee remembers its usual category and account".

**Transfer mode** replaces Payee **and** Category with a **From → To pair**: `From` (`flex:1`), an 18px `arrow-right` in `#c67139` at 36px height, `To` (`flex:1`), then a 36px secondary icon button to swap them. No payee, no category.

**When the To account is a credit card or loan**, one extra 210px field appears — `Budget category` — bordered `#c67139`, with the note: "Because the destination is a card you owe on, you can tag the payment with a budget category. The money then shows as spent in that envelope without the arriving side counting as income." This mirrors the existing behaviour of attaching a category to the from-leg.

**List:** grouped by day.

- Day header: flex, padding `17.6px 13.2px 4.4px`; left the date ("Mon 25 May") in micro-label style; right the day's net in `13px` tabular `#645c50`.
- Row: grid `1fr 190px 140px 28px`, gap `17.6px`, padding `9px 13.2px`, radius `16px`.
  - Name + tag — payee at `16px`/600, then either a sage `tag` with the category or a neutral `tag` reading `transfer`.
  - Account — `14px` `#645c50`, truncated.
  - Amount — `17px`/700 tabular, right. Outflows in `#201e1d`, inflows in `#56633f`.
  - Cleared — a 17px sage `check`; not-yet-cleared is a 10px circle with a 2px `#645c50` border (an explicit "pending", not an absence).

**Transfers are one row, not two.** See "Interactions" for the rule.

### 3. Paydown

**Header:** "Debt paydown" at `28px` serif; right, a horizon control — label, a 76px right-aligned tabular input (`24`), the word "months", and a secondary "Apply" button. Matches the existing `GET /paydown?horizon=`.

**Three stat cards**, grid `repeat(3, 1fr)`, gap `13.2px`, shadow `sm`, each a micro label over a `34px` serif tabular number:

1. "Going to debt each month" — `$485.00` — `#f0fae1` / `#56633f`
2. "Interest over 24 months" — `$1,935.14` — `#ffe1d0` / `#643312`
3. "Debt-free" — `Aug 2029` — `#ebddc5`

**One card per debt** (background `#f5ead8`, `1px` divider border, gap `13.2px`):

- Header row: account name `20px` serif; a neutral tag with the APR ("21.24% APR"); "payment **$200.00/mo**" with the figure in `#56633f`; "start **$2,886.61**"; "clears **Sep 2027**"; an ellipsis menu pinned right. When the payment does not cover interest, add an accent tag: "payment ≤ interest — debt grows".
- Schedule: grid `1fr 130px 130px 150px 150px`, gap `17.6px`, rows padding `8px 13.2px`. Columns Month · Interest (right, `#8c491a`) · Payment (right) · Source · Balance (right, 700).
- **Source is written in words** — "spent in Credit Card Payment", "assigned in Credit Card Payment", "default payment" — replacing the current `✓ spent` / `→ assigned` / `· default` colour glyphs, which were the least legible part of the original.
- Pager beneath: "page 1 of 5 · 24 months" with prev/next secondary buttons.

Empty case: "Visa has no balance, so it is not in the plan. Add an APR on an account to include it."

### 4. Category and goal management

Inline on the Budget list — there is no separate Categories page (matching the current code, where `/categories` redirects to `/budget`).

- Grid gains a fifth 44px column for the row menu.
- Each row shows a `grip-vertical` in `#c0b6a5` before the name for drag reorder.
- **Renaming:** the group name becomes a 220px pill input, border `#c67139`, with the hint "renaming — ⏎ to save, Esc to cancel". Same pattern for category names.
- Group header right side: the group's available total, then a secondary "Add category" button.
- **New category row:** a bordered input in the name column with "⏎ adds it to Savings Goals · Esc cancels" spanning the remaining columns.
- **Goal editor** opens as a panel directly beneath its row: margin `0 13.2px 8.8px`, padding `17.6px`, radius `16px`, background `#fff2eb`. Micro label "Goal for Vacation", then a wrapping row: "Target amount" (160px), "Needed by" (200px), a read-only "To arrive on time" showing `$675.00/mo` at `22px` serif in `#643312`, and Cancel / "Save goal" pushed right. A sentence underneath states the position in words: "You have $300.00 of $3,000.00 after 4 months. Keeping $675.00/mo gets you there in September."
- **Row menu** (260–280px, `#ebddc5`, shadow `lg`, radius `32.2px`, item padding `8px 8.8px`, radius `8px`, 15px): See transactions · Set a goal · Move to another group · ✓ Carry the balance forward · Ignore overspending · divider · Archive category (in `#8c491a`).
  The last two are the existing `rollover` and `ignore negative` flags, **reworded to say what they do**. A 16px sage check occupies a fixed 16px slot so labels align whether checked or not.

### 5. Accounts, payments and confirmations

- **Edit account** — a 420px side sheet, `#ebddc5`, padding `26.4px`, shadow `lg`, gap `17.6px`. Name; Type as a four-option segmented control (Checking / Savings / Credit / Loan); "Amount owed" and "Credit limit" side by side; "Interest rate" (140px). Note: "Enter what you owe as a positive number — Pigglet keeps the sign right", matching the existing form's sign handling. Cancel (ghost) / "Save account" (primary), right-aligned.
- **Monthly payment** dialog — 420px card. Payment (170px) and "Take the real amount from" (a category). Explains the fallback in the order the engine actually uses: "uses what you actually **spent** in that category, then what you **assigned**, and only falls back to this figure if neither exists."
- **Archive confirmation** — title in `#643312`; body states the consequence precisely: "Its 9 transactions stay where they are and still count in past months. The account leaves the sidebar and the paydown plan." Buttons "Keep it" / "Archive".
- **Transaction row menu** — 260px: Edit transaction · Mark as cleared · Duplicate · Move to another account · divider · Delete transaction (`#8c491a`).

### 6. First run

Rail present but inert. Content max-width 620px, padding `35.2px`, gap `26.4px`.

Micro label with the month, then "Let's find out what your money is doing." at `34px` serif. Body at `17px` `#645c50`: "Add the account you spend from most. Pigglet gives you a starter set of categories, and you can assign your first month straight away."

Then one form, max 420px: Account name, Type (segmented), "What's in it today" (180px), and a primary block button "Add account and start". Nothing else — there is nothing to assign until an account exists.

### 7. Mobile (390px)

One layout, reflowed. Frame padding `17.6px 13.2px 0`.

- **Rail becomes a bottom tab bar**: background `#ebddc5`, top corners `30px`, padding `8.8px 13.2px 17.6px`, three tabs (Budget / Activity / Paydown), each `min-width:72px; min-height:48px`, a 22px icon over an 11px/700 label, active tab in `#8c491a`.
- **Budget rows** stack: the four columns become name (16px/600) over a 7px bar over a 12px note, with Available at `19px`/700 on the right. Rows `min-height: 56px`, padding `10px 8.8px`.
- The overspend becomes a tappable row (`min-height:48px`, background `#ffe1d0`) with the amount and a chevron.
- **Quick add** becomes a card: a full-width segmented control (each option `flex:1`, `min-height:44px`), then a 52px amount input at `22px`/700 beside the Add button, then Payee at 48px, then category/account/date as 40px **tappable chips** instead of dropdowns, with the hint "Chevron usually means Gas on Main Checking".
- **Transfer mode** stacks Leaves / Arrives with a 44px swap button centred between them, then a full-width "Add transfer" button. The Budget-category field appears in its own `#ffe1d0` card when the destination is a card or loan.
- **Assigning** is a bottom sheet (top corners `30px`, `#ebddc5`, shadow `lg`, a 44×5px grab handle centred): title "Assign to Gas" at `24px` serif, a plain-language position line, a 56px amount input at `26px`/700, three 44px shortcut buttons ("Cover the $30.50" / "Same as April" / "Empty it"), a primary block Assign button, and a centred note "Comes out of the $2,295.00 left to assign". The list behind dims to 40% opacity.

**The one deliberate divergence:** assigned amounts are **not** inline inputs on mobile — a 36px number field inside a four-column row does not survive the reflow. Tapping the row opens the sheet instead.

Breakpoint: switch from rail to bottom bar and from grid rows to stacked rows below **768px**.

---

## Interactions & Behavior

### Quick-add row (the main change)

- The row is always present and always open at the top of the list — never behind a button, a dialog or a sheet. This removes the current `Add transaction` → sheet → form → save round trip.
- Focus opens on **Amount**.
- `⏎` submits. On success: prepend the new row to the list, **keep focus in the quick-add row**, clear Amount and Payee, and **retain Date and Account** so a run of receipts is a run of Enters.
- `⇥` moves through fields; `Esc` clears the row.
- Choosing a payee that has been used before **prefills its most recent category and account**. ⚠ New behaviour — needs a lookup of the last transaction per payee per user.
- Changing Type re-labels and swaps fields. The existing `input.css` already does exactly this with `:has(input[name="tx_type"][value="transfer"]:checked)` for the transfer destination and the Inflow/Outflow label — extend the same CSS-only approach to hide Payee/Category and show the From → To pair.

### Transfers — one row, two readings

Keep storing **two linked transactions** as today. Change only the rendering:

- Render **only the leg belonging to the account in view**.
- In the all-accounts list, render the **leaving** leg, so money always reads as leaving: `High-Yield Savings · transfer · from Main Checking · −$300.00`.
- Filtered to a single account, render that account's own leg: viewing High-Yield Savings the same transfer reads `+$300.00 from Main Checking`.
- **Cleared toggles both legs. Delete removes both legs** — there is no half-deleted transfer, and the confirmation names both resulting balances.
- The edit form labels the two accounts **Leaves** and **Arrives**, offers a swap button, shows one "Cleared on both accounts" checkbox, and states both balances after saving.

### Assigning

Unchanged from the current implementation: `POST /budget/assign/:catID` returning the edited row plus out-of-band swaps for the banner and totals (`BudgetAssignResult`). The redesign changes where those totals land — the summary-rail cards instead of table footer rows — so the OOB targets move, but the mechanism stays.

`A` triggers "Assign the rest". ⚠ **"Cover the $30.50" / "Cover from Fun Money" does not exist today** — it needs an endpoint that moves assigned money from one category to another in one action. Specify the source-picking rule before building it (suggest: the largest positive available in the same group, then the largest overall, with the source named in the button).

### Other behaviour

- Group and category **drag reorder** keeps the existing `POST /budget/groups/reorder` and `/budget/categories/reorder` plus `sortable.min.js`, `group-sort.js`, `category-sort.js`.
- **Click-to-rename** and click-to-edit-amount keep `inline-edit.js` and `name-edit.js`.
- **Section collapse** (Income, Credit, per-group) keeps the cookie-backed classes in `input.css` / `section-collapse.js`. In the redesign Income and Credit are rail cards rather than collapsible table sections, so only per-group collapse remains.
- **Loading:** keep the `.htmx-indicator` fade; the Paydown schedule keeps its spinner overlay.
- **Errors:** field-level, inline under the field in `#8c491a` at 13px. The quick-add row must not lose typed values on a failed save.
- **Empty states:** no accounts → the first-run screen; no categories in a group → the group header still renders with its "Add category" button; no debts → the Paydown sentence above.
- **Motion:** the assign sheet slides up over 300ms `cubic-bezier(0.32,0.72,0,1)` — the same curve as the existing `sheet-in` keyframe. Respect `prefers-reduced-motion`.

### Shortcut map

Keyboard-first was an explicit requirement.

| Key | Action | Scope |
| --- | --- | --- |
| `J` / `K` | Move down / up a row | Budget, Transactions |
| `⏎` | Assign the focused category | Budget |
| `⏎` | Save and start the next entry | Quick-add row |
| `G` | Set a goal on the focused category | Budget |
| `A` | Assign the rest | Budget |
| `N` | New transaction (focus the quick-add row) | Anywhere |
| `[` / `]` | Previous / next month | Anywhere |
| `T` | Today's month | Anywhere |
| `⇥` / `⇧⇥` | Next / previous field | Forms |
| `⇧⇥` | Swap From and To | Quick-add, transfer mode |
| `Esc` | Cancel or clear | Forms, sheets, menus |
| `⌘K` | Command palette | Anywhere |

⚠ `⌘K` is designed as an entry point but its **contents are not specified**. Decide the command set before building it, or omit it from the first pass — the strip is honest either way.

## State Management

Server-rendered, so most state stays on the server. Client-side state needed:

- Quick-add row: type (expense/income/transfer), the six field values, submitting flag, and the "sticky" date + account carried between saves.
- Focused row index for `J`/`K`, per screen.
- Which goal editor / new-category row / row menu is open (one at a time).
- Per-group collapse — already cookie-backed; keep it.
- Mobile: which assign sheet is open and its amount.

Server data per screen is unchanged from the current handlers — `BudgetData`, `TransactionsData`, `PaydownData` already carry everything the redesign shows, with two additions: the payee→last-category/account lookup, and whichever source the cover-overspend action picks.

## Assets

### Fonts — not in this bundle, you must fetch them

The prototypes pull webfonts from `fonts.googleapis.com`; `styles.css` has **zero `@font-face` rules**. Self-host these two before building:

| Family | Weights | Used for |
| --- | --- | --- |
| **Source Serif 4** | 600 | All headings |
| **Figtree** | 400, 600, 700 | All body, labels, numbers |

Both are SIL Open Font License. Fetch from `github.com/adobe-fonts/source-serif` and `github.com/erikdkennedy/figtree` (or google-webfonts-helper for ready-made woff2 subsets), drop them in `internal/web/static/fonts/`, and add `@font-face` rules next to the existing Source Sans 3 / Oxanium ones in `internal/web/tailwind/input.css`.

Two traps:

- **Do not self-host Caprasimo.** `styles.css` sets `--font-heading: "Caprasimo"` because that is the Organic system's stock display face, but it was **rejected during review as hard to read** and replaced by Source Serif 4. The redesign overrides `--font-heading` inline on every screen. When you port the tokens, set `--font-heading: "Source Serif 4", Georgia, serif` and `--font-heading-weight: 600` at the root and drop Caprasimo entirely.
- **`fonts/` in this bundle is not for the redesign.** It holds `oxanium.woff2` and `source-sans-3.woff2`, which are byte-identical copies of the repo's current fonts and exist only so `Pigglet Current.dc.html` (the before-state recreation) renders offline. Neither is used by the redesign.

### Icons

Lucide at stroke-width 2.75. The repo already vendors templUI's `icon` package (`icon.Wallet`, `icon.ChevronRight`, `icon.GripVertical`, `icon.EllipsisVertical`, `icon.Check`, `icon.TrendingDown` …) which is Lucide — keep using it and set the stroke width.

### Images

**None.** The design uses no photography, so the design system's `.washed` treatment is unused. The Catppuccin-era PNGs in `screenshots/` are stale and should not be used as reference — they predate the templUI/shadcn rewrite.

## Mapping to the existing Templ views

| Repo file | What changes |
| --- | --- |
| `views/layout.templ` | Replace the templUI inset sidebar (`sidebar.VariantInset`, `--sidebar-width:20rem`) with the 72px icon rail; add the <768px bottom tab bar. Nav becomes Budget / Transactions / Paydown — **Transactions needs a nav entry**, it currently has none and is only reachable by clicking an account. |
| `views/budget_page.templ` | Month header moves into the summary rail. |
| `views/budget_table.templ` | `BudgetTable` / `subHeaderRow` / `BudgetSummary` → the 300px summary rail plus a 4-column div grid. `<table>`/`<tbody>` become `div`s; the footer total rows become rail cards. |
| `views/budget_row.templ` | `budgetRow` → the new row; `GoalCell` → goal tags; `rowMenu` → the reworded menu; `BudgetGoalEditor` → the inline goal panel with the computed monthly figure. |
| `views/budget_income.templ` | Income stops being a collapsible table section; the total lands in the rail's Totals card, with the source editor kept as a panel. |
| `views/budget_credit.templ` | Owing lands in the "Needs attention" card. |
| `views/transactions.templ` | `TransactionsBody` header; `TxFilterBar` → the segmented account filter; `TxRows` → day-grouped rows; `TransactionRow`/`TxCategoryCell` → the new row with one-row transfers; **`TransactionForm` moves out of `sheetPanel` and becomes the inline quick-add row** (the sheet remains for editing). |
| `components/monthselector` | Same three controls, restyled as pill icon buttons. |
| `components/signedmoney` | Change the three colours to sage / terracotta / neutral-700. Single point of change for all money. |
| `components/accountsoverview` | Becomes the Budget rail's account list on desktop and a Budget-tab section on mobile. |
| `views/paydown.templ` | `pdStat` → the three stat cards; `PaydownSection` → the per-debt card; `PaydownScheduleBody` → the 5-column grid; **`PaydownSource` becomes words instead of glyphs**; `PaymentModal` / `CategoryModal` restyled. |
| `tailwind/theme.css` | **Keep the shadcn token names, swap their values** (see "How to port them"), add the Organic ramps and spacing alongside, and keep `--radius: 16px` with a separate `--radius-pill`. The file already has a `.dark` block — repoint it at the dark role map rather than the shadcn dark values. |
| `tailwind/input.css` | Keep the `:has()`-driven transaction-type field swapping; extend it to the From → To pair. Keep the collapse rules for groups; drop the income/credit ones. |

## Suggested phasing

Each phase leaves the app shippable.

| # | Scope | Verify |
| --- | --- | --- |
| 0 | Tokens (value-swap) + self-hosted fonts + `signedmoney` colours | `task css` + `task test`; every screen renders, recoloured only |
| 1 | Shell: 72px rail, <768px tab bar, Transactions nav entry | all routes reachable |
| 2 | Budget: summary rail + 4-column grid, rewritten `budget_row` | assign OOB swaps still land |
| 3 | Transactions: inline quick-add, one-row transfers | a run of receipts is a run of Enters |
| 4 | Paydown worded sources, panels, dialogs, first run | |
| 5 | Mobile reflow + assign sheet | |
| 6 | Dark role map into the existing `.dark` block | toggle switches both modes cleanly |

Cover-overspend, ⌘K and payee memory stay deferred — they are new features, not redesign.

## Open decisions

1. **Cover-overspend action** — needs an endpoint and a source-picking rule. ⚠
2. **⌘K command palette** — contents undefined. ⚠
3. **Payee memory** — needs a per-payee lookup of last category and account.
4. **Reports** — has no route in the current code and was not designed. New scope, not a redesign.
5. **Mobile assign** — sheet rather than inline field, by necessity. Confirm that is acceptable before building.
6. **Dark mode default** — follow `prefers-color-scheme`, or ship light-first with a toggle? The repo's existing `theme-toggle.js` already answers this in code; the design supports either.
7. **Scope** — this redesign changes layout, not just styling: it adds the 72px rail, the mobile tab bar, the Budget summary rail, and a Transactions nav entry. That is intentional and was agreed during the design review (a sidebar-plus-bottom-bar shell and an always-open quick-add row were explicit requirements). It supersedes any earlier "restyle the existing layout only" instruction. If that instruction still stands, stop at Phase 0 — the token and font work alone is a real improvement and needs no layout change.

## Files

In this bundle:

- `Pigglet Redesign.dc.html` — the redesign. Turns are ordered newest-first: **Turn 6** dark mode (`6a` desktop, `6b` phone, `6c` the light→dark role map), **Turn 5** mobile (4 phone frames), **Turn 4** transfers (`4a` entry, `4b` editing), **Turn 3** Paydown / categories & goals / panels / first run (`3a`–`3d`), **Turn 2** the chosen desktop design in two type treatments (`2a` Figtree, **`2b` Source Serif 4 — the one selected**), **Turn 1** the four original directions (`1a`–`1d`). Build from Turns 2–6; Turn 1 is history.
- `Pigglet Current.dc.html` — a faithful recreation of today's Budget and Transactions screens, rebuilt from the `*_templ.go` sources, for before/after comparison.
- `styles.css` — the Organic design-system stylesheet (token source of truth).
- `support.js` — runtime needed to open the two `.dc.html` files in a browser.

Open either HTML file directly in a browser. `Pigglet Redesign.dc.html` pans and zooms as a canvas.
