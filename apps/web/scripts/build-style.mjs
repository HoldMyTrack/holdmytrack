/**
 * Emits one style document per flavor for the API to serve (ARCHITECTURE.md §2.1:
 * "Serve the style as a document from the API rather than reimplementing it per client").
 *
 * Generating rather than hand-writing these is the whole point: `buildStyle()` stays the
 * single definition of the style, and a Go reimplementation of 71 layers is exactly the
 * divergence §2.1 exists to prevent. The `.json` files this writes are build output, not a
 * second source — edit `src/map/style.ts` and re-run; never edit the JSON.
 *
 * `origin` is baked as a placeholder rather than a real host because one built artifact has
 * to serve every deployment: dev reads the archive from the app's own origin, production may
 * read it from a CDN (`VITE_BASEMAP_ORIGIN`, `config.ts`'s `basemapOrigin()`). The server
 * substitutes its own configured origin per request.
 *
 * Usage: node scripts/build-style.mjs [--check]
 *   --check regenerates in memory and fails if the committed files differ, so drift between
 *   style.ts and the served document is caught here rather than on a client.
 */
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { registerHooks } from 'node:module';
import { dirname, join, resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

// src/ imports are extensionless by convention (Vite resolves them; Node does not). Rather
// than write `./config.ts` into production source for this script's benefit, retry a failed
// relative resolve with the extension Node needs.
registerHooks({
  resolve(specifier, context, nextResolve) {
    try {
      return nextResolve(specifier, context);
    } catch (err) {
      if (specifier.startsWith('.') && !/\.[cm]?[jt]s$/.test(specifier)) {
        return nextResolve(`${specifier}.ts`, context);
      }
      throw err;
    }
  },
});

const { buildStyle, FLAVORS } = await import('../src/map/style.ts');

/** Must match originPlaceholder in services/server/internal/mapstyle/mapstyle.go. */
const ORIGIN_PLACEHOLDER = '__HOLDMYTRACK_BASEMAP_ORIGIN__';

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolvePath(here, '../../../services/server/internal/mapstyle/styles');
const check = process.argv.includes('--check');
let drifted = false;

await mkdir(outDir, { recursive: true });

for (const flavor of FLAVORS) {
  const style = buildStyle({ flavor, origin: ORIGIN_PLACEHOLDER });
  const json = `${JSON.stringify(style, null, 2)}\n`;
  const path = join(outDir, `${flavor}.json`);

  if (check) {
    const existing = await readFile(path, 'utf8').catch(() => null);
    if (existing !== json) {
      console.error(`drift: ${flavor}.json does not match src/map/style.ts`);
      drifted = true;
    }
    continue;
  }

  await writeFile(path, json);
  console.log(`wrote ${flavor}.json (${style.layers.length} layers)`);
}

if (drifted) {
  console.error('\nRun `npm run build:style` in apps/web and commit the result.');
  process.exit(1);
}
