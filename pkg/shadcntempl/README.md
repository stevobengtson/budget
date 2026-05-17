# shadcntempl

shadcn/ui-style components for [templ](https://templ.guide) + Tailwind v4, designed
to be vendored or — eventually — published as a standalone Go module.

## Status

Early. Implemented: `button`, `table`, `card`, `badge`, `input`. Themed against
shadcn 2.x OKLCH tokens with light + `.dark` palettes.

## Layout

```
pkg/shadcntempl/
├── doc.go               # package overview
├── theme/theme.css      # :root + .dark OKLCH token blocks (preset target)
├── tailwind/input.css   # Tailwind v4 entry: @import + @theme inline
├── button/              # one subpackage per component
├── table/
├── card/
├── badge/
├── input/
└── internal/cn/         # class-name join helper
```

Each subpackage exposes a `Props` struct + one or more `templ.Component`
constructors with variant/size enums that mirror the upstream React API.

## Usage in a host project

1. Add the standalone Tailwind v4 binary to your build chain. With this
   repository's Makefile that is `make tools` (downloads `bin/tailwindcss`).
2. Point Tailwind at `pkg/shadcntempl/tailwind/input.css` and emit the compiled
   CSS wherever your server serves static files (this repo writes to
   `internal/web/static/app.css`).
3. Generate templ as usual: `templ generate ./pkg/shadcntempl ./your/views`.
4. Import components and render them inside your templ pages:

   ```templ
   import "github.com/sbengtson/budget/pkg/shadcntempl/button"

   templ Page() {
       @button.Button(button.Props{Variant: button.VariantDestructive}) {
           Delete
       }
   }
   ```

5. Link the compiled stylesheet from your layout (`<link rel="stylesheet" href="/static/app.css">`).

## Themes

`theme/theme.css` is the canonical token surface. Replace it with a preset from
[ui.shadcn.com/create](https://ui.shadcn.com/create) via the bundled CLI:

```bash
make theme PRESET=b6FTKD8F6
# or, if you already have the registry URL:
make theme URL=https://ui.shadcn.com/r/themes/<id>.json
```

The importer rewrites the `:root { ... }` and `.dark { ... }` blocks in place;
the `@theme inline { ... }` mapping in `tailwind/input.css` continues to wire
the variables into Tailwind utilities.
