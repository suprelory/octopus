/**
 * Recompress the PWA install screenshots in place.
 *
 * These are only fetched when a browser renders an install prompt, so they are
 * not page weight - but they are embedded into the Go binary via
 * `//go:embed all:out` and dominated the static payload.
 *
 * Format stays PNG and dimensions stay byte-identical to the values declared in
 * `public/manifest.json`: guidance on whether manifest screenshots may be WebP
 * is contradictory (web.dev's richer-install-ui docs say PNG/JPEG only, MDN
 * shows an `image/webp` example), so this only changes the encoder settings.
 *
 * Already palette-encoded PNGs are treated as optimized so repeated runs do
 * not rewrite committed assets.
 *
 * Run with: node scripts/optimize-screenshots.mjs
 */
import { createRequire } from 'node:module';
import { readdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';

// sharp is a transitive dependency, so resolve it out of the pnpm store rather
// than assuming a hoisted top-level install.
const require = createRequire(import.meta.url);

async function loadSharp() {
    try {
        return require('sharp');
    } catch {
        const pnpmDir = path.resolve(import.meta.dirname, '..', 'node_modules', '.pnpm');
        const entries = await readdir(pnpmDir);
        const match = entries.find((name) => name.startsWith('sharp@'));
        if (!match) throw new Error('sharp not found; run `pnpm install` first');
        return require(path.join(pnpmDir, match, 'node_modules', 'sharp'));
    }
}

const sharp = await loadSharp();
const dir = path.resolve(import.meta.dirname, '..', 'public', 'screenshot');
const files = (await readdir(dir)).filter((f) => f.endsWith('.png'));

let before = 0;
let after = 0;

for (const file of files.sort()) {
    const full = path.join(dir, file);
    const original = await readFile(full);
    const { width, height, isPalette } = await sharp(original).metadata();

    if (isPalette) {
        console.log(`${file.padEnd(24)} already palette-encoded, skipped`);
        before += original.length;
        after += original.length;
        continue;
    }

    const optimized = await sharp(original)
        // palette quantisation is what actually shrinks UI screenshots: they use
        // few distinct colours, so 8-bit indexed output is near-lossless here.
        .png({ compressionLevel: 9, effort: 10, palette: true, quality: 90 })
        .toBuffer();

    const check = await sharp(optimized).metadata();
    if (check.width !== width || check.height !== height) {
        throw new Error(`${file}: dimensions changed ${width}x${height} -> ${check.width}x${check.height}`);
    }
    if (optimized.length >= original.length) {
        console.log(`${file.padEnd(24)} kept original (${original.length} <= ${optimized.length})`);
        before += original.length;
        after += original.length;
        continue;
    }

    await writeFile(full, optimized);
    before += original.length;
    after += optimized.length;
    const pct = (100 * (1 - optimized.length / original.length)).toFixed(1);
    console.log(
        `${file.padEnd(24)} ${width}x${height}  ${original.length.toLocaleString()} -> ${optimized.length.toLocaleString()}  (-${pct}%)`
    );
}

console.log(
    `\ntotal ${(before / 1024 / 1024).toFixed(2)} MB -> ${(after / 1024 / 1024).toFixed(2)} MB ` +
    `(-${(100 * (1 - after / before)).toFixed(1)}%)`
);
