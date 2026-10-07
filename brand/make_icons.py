#!/usr/bin/env python3
"""Renders every icon and logo file in the repo from brand/logo.svg.

Run from anywhere after changing logo.svg: python3 brand/make_icons.py
Needs rsvg-convert (brew install librsvg) and Pillow.
"""
import json
import os
import struct
import subprocess

from PIL import Image, ImageDraw, ImageFont

BRAND = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(BRAND)
GOLD, OCHRE, GREEN = '#E4B867', '#93691F', '#1C2520'  # --fm-accent-strong (dark, light), --fm-surface (dark)

src = open(os.path.join(BRAND, 'logo.svg')).read()
inner = src[src.index('<defs>'):src.rindex('</svg>')].strip()
# The mark's ink bounds in logo.svg's own units, strokes included.
X0, X1, Y0, Y1 = 12.25, 106, 7.75, 100.75
CX, CY, W = (X0 + X1) / 2, (Y0 + Y1) / 2, max(X1 - X0, Y1 - Y0)


def mark(color, frac, size=1024):
    """The mark in color, centred on a size-square canvas, frac of its width across."""
    s = size * frac / W
    body = inner.replace(f'stroke="{GOLD}"', f'stroke="{color}"')
    return f'<g transform="translate({size / 2} {size / 2}) scale({s:.5f}) translate({-CX} {-CY})">{body}</g>'


def svg(content, size=1024, bg=None, rx=0):
    rect = f'<rect width="{size}" height="{size}" rx="{rx}" fill="{bg}"/>' if bg else ''
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="0 0 {size} {size}">{rect}{content}</svg>\n'


def write(path, text):
    p = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, 'w') as f:
        f.write(text)
    return p


def png(svg_path, path, w, h=None, opaque=False):
    p = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    subprocess.run(['rsvg-convert', '-w', str(w), '-h', str(h or w), '-o', p, svg_path], check=True)
    if opaque:  # the App Store and Play reject an alpha channel
        Image.open(p).convert('RGB').save(p)
    return p


# Masters, each one platform's framing of the mark.
tile = write('brand/source/tile.svg', svg(mark(GOLD, 0.64), bg=GREEN))  # full-bleed: platforms round it
write('brand/source/tile-rounded.svg', svg(mark(GOLD, 0.64), bg=GREEN, rx=225))
tile_fav = write('brand/source/tile-favicon.svg', svg(mark(GOLD, 0.78), bg=GREEN, rx=200))
maskable = write('brand/source/tile-maskable.svg', svg(mark(GOLD, 0.52), bg=GREEN))  # inside the 80% safe circle
fg = write('brand/source/android-foreground.svg', svg(mark(GOLD, 46 / 108)))  # diagonal inside the 66dp safe zone
mono = write('brand/source/android-monochrome.svg', svg(mark('#FFFFFF', 46 / 108)))
favicon_svg = svg(mark('currentColor', 0.94)).replace(
    '<g transform', f'<style>g{{color:{OCHRE}}}@media (prefers-color-scheme:dark){{g{{color:{GOLD}}}}}</style><g transform', 1)

# The mark alone, cropped to its ink, one PNG per background it sits on: the README's (GitHub's
# light and dark themes) and the map export watermark's (exportMap.ts, light and dark basemaps,
# in the color of the wordmark's "Track" beside it). The page header draws logo.svg inline
# instead (templates/header.html), so it follows the theme.
cropped = src.replace('viewBox="10 5 99 98"', f'viewBox="{X0 - 1} {Y0 - 1} {X1 - X0 + 2} {Y1 - Y0 + 2}"')
for color, name, path in [
    (OCHRE, 'logo-on-light', 'brand/logo-on-light.png'),
    (GOLD, 'logo-on-dark', 'brand/logo-on-dark.png'),
    ('#9A6B1E', 'watermark-on-light', 'apps/web/src/assets/logo-on-light.png'),
    ('#E0A84A', 'watermark-on-dark', 'apps/web/src/assets/logo-on-dark.png'),
]:
    s = write(f'brand/source/{name}.svg', cropped.replace(f'stroke="{GOLD}"', f'stroke="{color}"'))
    png(s, path, 512, round(512 * (Y1 - Y0 + 2) / (X1 - X0 + 2)))

# Web: apps/web/public, served by Caddy's @static list (apps/web/docker/Caddyfile).
web = 'apps/web/public'
write(f'{web}/favicon.svg', favicon_svg)
ico_sizes = [16, 32, 48]
ico_pngs = []
for n in ico_sizes:
    p = png(tile_fav, f'{web}/.favicon-{n}.png', n)
    ico_pngs.append(open(p, 'rb').read())
    os.remove(p)
with open(os.path.join(ROOT, f'{web}/favicon.ico'), 'wb') as f:
    f.write(struct.pack('<HHH', 0, 1, len(ico_pngs)))
    offset = 6 + 16 * len(ico_pngs)
    for n, data in zip(ico_sizes, ico_pngs):
        f.write(struct.pack('<BBBBHHII', n % 256, n % 256, 0, 0, 1, 32, len(data), offset))
        offset += len(data)
    for data in ico_pngs:
        f.write(data)
png(tile_fav, f'{web}/favicon.png', 64)
png(tile, f'{web}/apple-touch-icon.png', 180, opaque=True)
png(tile, f'{web}/icon-192.png', 192)
png(tile, f'{web}/icon-512.png', 512)
png(maskable, f'{web}/icon-maskable-512.png', 512)
manifest = {
    'name': 'HoldMyTrack', 'short_name': 'HoldMyTrack', 'start_url': '/', 'display': 'standalone',
    'background_color': GREEN, 'theme_color': GREEN,
    'icons': [
        {'src': '/icon-192.png', 'sizes': '192x192', 'type': 'image/png'},
        {'src': '/icon-512.png', 'sizes': '512x512', 'type': 'image/png'},
        {'src': '/icon-maskable-512.png', 'sizes': '512x512', 'type': 'image/png', 'purpose': 'maskable'},
    ],
}
write(f'{web}/site.webmanifest', json.dumps(manifest, indent=2) + '\n')

# Android adaptive icon (res/mipmap-anydpi/ic_launcher.xml), plus the Play listing's 512.
main = 'apps/android/holdmytrack/app/src/main'
for density, k in [('mdpi', 1), ('hdpi', 1.5), ('xhdpi', 2), ('xxhdpi', 3), ('xxxhdpi', 4)]:
    png(fg, f'{main}/res/mipmap-{density}/ic_launcher_foreground.png', int(108 * k))
    png(mono, f'{main}/res/mipmap-{density}/ic_launcher_monochrome.png', int(108 * k))
write(f'{main}/res/values/ic_launcher_background.xml',
      f'<?xml version="1.0" encoding="utf-8"?>\n<resources>\n    <color name="ic_launcher_background">{GREEN}</color>\n</resources>\n')
png(tile, f'{main}/ic_launcher-playstore.png', 512, opaque=True)

# iOS: a single-size 1024 AppIcon (Xcode 14+), ready for the app's asset catalog.
ios = 'apps/ios/Assets.xcassets'
png(tile, f'{ios}/AppIcon.appiconset/AppIcon-1024.png', 1024, opaque=True)
write(f'{ios}/AppIcon.appiconset/Contents.json', json.dumps({
    'images': [{'filename': 'AppIcon-1024.png', 'idiom': 'universal', 'platform': 'ios', 'size': '1024x1024'}],
    'info': {'author': 'xcode', 'version': 1}}, indent=2) + '\n')
write(f'{ios}/Contents.json', json.dumps({'info': {'author': 'xcode', 'version': 1}}, indent=2) + '\n')

# Play's 1024 × 500 feature graphic, one per store-listing language: the mark beside the page
# header's wordmark ("HoldMy" regular, "Track" bold, Fraunces) over its tagline, on the dark
# surface, centred and well clear of the edges Play crops or covers. Fraunces has no Cyrillic,
# so the Russian tagline is Source Serif 4, as on the web (tokens.css :lang(ru)).
fonts = f'{main}/res/font'
mark_png = os.path.join(BRAND, 'source/.mark.png')
subprocess.run(['rsvg-convert', '-h', '190', '-o', mark_png, os.path.join(BRAND, 'source/logo-on-dark.svg')], check=True)
logo = Image.open(mark_png).convert('RGBA')
os.remove(mark_png)


def font(name, px, weight):
    f = ImageFont.truetype(os.path.join(ROOT, fonts, name), px)
    f.set_variation_by_axes([weight])
    return f


for lang, serif in [('en', 'fraunces.ttf'), ('ru', 'source_serif_4.ttf')]:
    tagline = json.load(open(os.path.join(ROOT, f'services/server/internal/i18n/locales/{lang}.json')))['header.tagline']
    img = Image.new('RGB', (1024, 500), GREEN)
    d = ImageDraw.Draw(img)
    light, bold, small = font('fraunces.ttf', 78, 400), font('fraunces.ttf', 78, 800), font(serif, 29, 400)
    w1, w2 = d.textlength('HoldMy', font=light), d.textlength('Track', font=bold)
    x = (1024 - (logo.width + 38 + max(w1 + w2, d.textlength(tagline, font=small) + 4))) / 2
    y = (500 - logo.height) // 2
    img.paste(logo, (int(x), y), logo)
    tx, base = x + logo.width + 38, y + logo.height * 0.52
    d.text((tx, base), 'HoldMy', font=light, fill='#C7C3B7', anchor='ls')  # header's ink at 82% on GREEN
    d.text((tx + w1, base), 'Track', font=bold, fill=GOLD, anchor='ls')
    d.text((tx + 4, base + 52), tagline, font=small, fill='#A8B2A9', anchor='ls')  # --fm-ink-secondary, dark
    img.save(os.path.join(BRAND, 'play-feature-graphic.png' if lang == 'en' else f'play-feature-graphic-{lang}.png'))
