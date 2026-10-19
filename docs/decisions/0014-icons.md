# 0014. Icons: menu bar A (USB-C pit), app icon B (plugged in)

- **Status:** Accepted
- **Date:** 2026-10-19

## Context
The app needed a menu bar glyph and an app icon. Two concepts each were drawn and previewed (`macos/Design/preview.html`).

## Decision
Menu bar: avocado whose pit is a USB-C port, empty when idle and filled when connected (vector template images). App icon: cut avocado with a USB-C plug in the pit and its cable running down, from a full-bleed source so macOS 26 applies its own shape.

## Alternatives considered
| Option | Why not |
|---|---|
| App icon A (port in the pit) | reads better at 16 px, less story |
| Pixel-art B | explored, user preferred the smooth version |

## Consequences
Sources, renders and specs live in `macos/Design/`; the user reviews designs before integration.
