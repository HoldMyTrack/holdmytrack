import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported module itself.
// Only readExif is tested here; preparePhoto's resizing needs a browser's canvas.
import { readExif } from '../src/ui/photoPrep.ts';

const ASCII = 2;
const LONG = 4;
const RATIONAL = 5;

/** A TIFF block, in either byte order, with IFD0 pointing at an Exif IFD and a GPS IFD.
 *  Each entry is [tag, type, value]: a string for ASCII, [[num, den], …] for RATIONAL. */
function tiff({ little = true, ifd0 = [], exif = [], gps = [] }) {
  const ifds = [ifd0, exif, gps];
  const ifdSize = (entries) => 2 + entries.length * 12 + 4;
  // IFD0 gains its two pointer entries.
  ifd0.push([0x8769, LONG, 0], [0x8825, LONG, 0]);
  const ifdOffsets = [];
  let at = 8;
  for (const entries of ifds) {
    ifdOffsets.push(at);
    at += ifdSize(entries);
  }
  ifd0[ifd0.length - 2][2] = ifdOffsets[1];
  ifd0[ifd0.length - 1][2] = ifdOffsets[2];
  const data = [];
  const bytes = new DataView(new ArrayBuffer(4096));
  bytes.setUint16(0, little ? 0x4949 : 0x4d4d);
  bytes.setUint16(2, 42, little);
  bytes.setUint32(4, 8, little);
  ifds.forEach((entries, i) => {
    let p = ifdOffsets[i];
    bytes.setUint16(p, entries.length, little);
    p += 2;
    for (const [tag, type, value] of entries) {
      bytes.setUint16(p, tag, little);
      bytes.setUint16(p + 2, type, little);
      if (type === LONG) {
        bytes.setUint32(p + 4, 1, little);
        bytes.setUint32(p + 8, value, little);
      } else {
        const payload = type === ASCII ? [...value].map((c) => c.charCodeAt(0)).concat(0) : value;
        const size = type === ASCII ? payload.length : payload.length * 8;
        bytes.setUint32(p + 4, payload.length, little);
        if (size <= 4) {
          payload.forEach((b, j) => bytes.setUint8(p + 8 + j, b));
        } else {
          bytes.setUint32(p + 8, at, little);
          data.push([at, type, payload]);
          at += size;
        }
      }
      p += 12;
    }
    bytes.setUint32(p, 0, little);
  });
  for (const [offset, type, payload] of data) {
    if (type === ASCII) payload.forEach((b, j) => bytes.setUint8(offset + j, b));
    else
      payload.forEach(([num, den], j) => {
        bytes.setUint32(offset + j * 8, num, little);
        bytes.setUint32(offset + j * 8 + 4, den, little);
      });
  }
  return new Uint8Array(bytes.buffer, 0, at);
}

/** A JPEG that's just SOI, an APP1 Exif segment holding block, and an empty SOS. */
function jpeg(block) {
  const app1 = new Uint8Array(4 + 6 + block.length);
  const view = new DataView(app1.buffer);
  view.setUint16(0, 0xffe1);
  view.setUint16(2, 2 + 6 + block.length);
  app1.set([...'Exif'].map((c) => c.charCodeAt(0)).concat(0, 0), 4);
  app1.set(block, 10);
  const out = new Uint8Array(2 + app1.length + 4);
  out.set([0xff, 0xd8]);
  out.set(app1, 2);
  out.set([0xff, 0xda, 0x00, 0x02], 2 + app1.length);
  return out.buffer;
}

const dms = (deg, min, sec) => [
  [deg, 1],
  [min, 1],
  [Math.round(sec * 100), 100],
];

test('a capture time with its zone is an instant', () => {
  const exif = readExif(
    jpeg(tiff({ exif: [[0x9003, ASCII, '2026:05:01 12:00:30'], [0x9011, ASCII, '+02:00']] })),
  );
  assert.deepEqual(exif, { takenAt: '2026-05-01T10:00:30Z' });
});

test('a capture time with no zone takes it from the GPS clock, keeping its own seconds', () => {
  for (const little of [true, false]) {
    const exif = readExif(
      jpeg(
        tiff({
          little,
          exif: [[0x9003, ASCII, '2026:05:01 19:00:30']],
          gps: [
            [0x0001, ASCII, 'N'],
            [0x0002, RATIONAL, dms(50, 0, 1.8)],
            [0x0003, ASCII, 'W'],
            [0x0004, RATIONAL, dms(10, 30, 0)],
            // Stale by two minutes, as a phone's last fix can be.
            [0x0007, RATIONAL, dms(9, 58, 41)],
            [0x001d, ASCII, '2026:05:01'],
          ],
        }),
      ),
    );
    assert.equal(exif.takenAt, '2026-05-01T10:00:30Z', little ? 'little-endian' : 'big-endian');
    assert.ok(Math.abs(exif.lat - 50.0005) < 1e-6);
    assert.equal(exif.lon, -10.5);
  }
});

test('a capture time with no zone and no GPS clock goes as a wall clock', () => {
  const exif = readExif(jpeg(tiff({ ifd0: [[0x0132, ASCII, '2026:05:01 12:00:30']] })));
  assert.deepEqual(exif, { takenLocal: '2026-05-01T12:00:30' });
});

test('a blank clock and a zero position are nothing', () => {
  const exif = readExif(
    jpeg(
      tiff({
        exif: [[0x9003, ASCII, '0000:00:00 00:00:00']],
        gps: [
          [0x0001, ASCII, 'N'],
          [0x0002, RATIONAL, dms(0, 0, 0)],
          [0x0003, ASCII, 'E'],
          [0x0004, RATIONAL, dms(0, 0, 0)],
        ],
      }),
    ),
  );
  assert.deepEqual(exif, {});
});

test('not a JPEG, or a JPEG with no EXIF, reads as nothing', () => {
  assert.deepEqual(readExif(new Uint8Array([0x89, 0x50, 0x4e, 0x47]).buffer), {});
  assert.deepEqual(readExif(new Uint8Array([0xff, 0xd8, 0xff, 0xda, 0x00, 0x02]).buffer), {});
  assert.deepEqual(readExif(new ArrayBuffer(0)), {});
});
