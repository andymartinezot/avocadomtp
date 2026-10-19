# AvocadoMTP icon designs

Sources and reference renders for the app icon and the menu bar icon.
Open [`preview.html`](preview.html) in a browser to compare the concepts at real size,
in light and dark mode.

```
macos/Design/
├── preview.html            side-by-side comparison of every concept
├── menubar/                menu bar icon sources (SVG, 18×18 pt, template)
│   ├── A-usbc-pit-idle.svg
│   ├── A-usbc-pit-connected.svg
│   ├── B-plug-idle.svg
│   └── B-plug-connected.svg
├── appicon/                app icon sources (SVG, 1024×1024)
│   ├── A-port-pit.svg
│   ├── B-plugged.svg
│   ├── B-plugged-fullbleed.svg  ← the app icon source
│   ├── B-pixel.svg           ← pixel-art version of B (full-bleed, for the app)
│   └── B-pixel-preview.svg   ← same, rounded for previews
├── tools/pixel_icon.py     generates the pixel-art icon from a 32×32 grid (edit and rerun)
└── previews/               PNG renders (transparent), regenerated from the SVGs
```

## Concepts

| | A | B |
|---|---|---|
| Menu bar | **USB-C pit**: the pit is a USB-C port, empty when idle, filled when connected | **Plugged-in avocado**: avocado with a cable and plug; hollow when idle, filled when connected |
| App icon | **Port in the pit**: cut avocado whose pit is a USB-C port | **Plugged in**: USB-C plug in the pit, cable running down |

## Decisions

- **Menu bar:** concept A (USB-C pit), in the app as `Assets.xcassets/MenuBarIdle` and `MenuBarConnected`.
- **App icon:** concept B, smooth version. The app uses `appicon/B-plugged-fullbleed.svg`
  (background fills the square; macOS 26 applies the rounded shape and edge highlight),
  rendered into `Assets.xcassets/AppIcon.appiconset` (16–1024 px).
- Explored, not used: app icon A, menu bar B, and a pixel-art B (`B-pixel*.svg`, `tools/pixel_icon.py`).

## Specs

### Menu bar icon (template image)

- **Format:** SVG (preferred) or PDF; PNG at 18×18 and 36×36 px also works.
- **Canvas:** 18×18 pt, drawing ≈ 16–17 pt tall.
- **Color:** black on transparent, one color only. macOS tints it for light/dark menu bars
  and dims it when inactive.
- **Lines:** ≥ 1.2 pt; keep details few and bold.
- **States:** `idle` (no phone) and `connected`. The app shows the total speed next to the icon
  while copying, so no separate "transferring" glyph is needed.

### App icon

- **Best:** an Icon Composer file (`AppIcon.icon`) built from separate layers (background,
  avocado, pit/USB). macOS 26 adds the Liquid Glass material, shape and dark/tinted variants.
- **Simple:** one 1024×1024 PNG, sRGB, square. Don't round the corners: macOS applies the shape.
  The rounded square in these SVGs is only there for previews.
- Must stay recognizable at 16–32 px (Finder lists, Spotlight).

## Replacing a design

Drop new files here with the same names (or new ones, and say which to use), then they're
integrated into `macos/AvocadoMTP/Assets.xcassets` (menu bar) and the app icon.

To regenerate the PNG renders after editing an SVG, open the SVG in a browser and export it.
