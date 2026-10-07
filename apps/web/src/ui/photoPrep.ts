/**
 * Getting a picked photo ready for upload (IMPLEMENTATION.md §4.27, ADR-0024): read what its
 * EXIF says about when and where it was taken, then redraw it at most 2048 px on its long side,
 * plus a 320 px thumbnail. Redrawing re-encodes the pixels alone, so no EXIF field — the
 * position included — reaches the server inside the file; the time and position travel as
 * plain upload fields, and the server keeps only a moment on the track.
 *
 * The EXIF reader is a small one for JPEG, which is what cameras and phones (an iPhone too,
 * once its picker converts) hand a browser. Anything else uploads with no metadata, unplaced.
 */

export const PHOTO_MAX_SIDE = 2048;
export const PHOTO_THUMB_SIDE = 320;
const QUALITY = 0.85;

/** What a photo's EXIF says, as `POST /v1/photos` takes it. `takenAt` is an instant (RFC 3339,
 *  UTC), `takenLocal` a camera clock with no zone (`YYYY-MM-DDTHH:MM:SS`); at most one is set. */
export interface PhotoExif {
  takenAt?: string;
  takenLocal?: string;
  lat?: number;
  lon?: number;
}

const TAG_EXIF_IFD = 0x8769;
const TAG_GPS_IFD = 0x8825;
const TAG_DATETIME = 0x0132;
const TAG_DATETIME_ORIGINAL = 0x9003;
const TAG_OFFSET_TIME_ORIGINAL = 0x9011;
const GPS_LAT_REF = 0x0001;
const GPS_LAT = 0x0002;
const GPS_LON_REF = 0x0003;
const GPS_LON = 0x0004;
const GPS_TIME = 0x0007;
const GPS_DATE = 0x001d;

type TagValue = string | number[];

/** The TIFF structure inside a JPEG's APP1 "Exif" segment, or null. */
function findTiff(buf: ArrayBuffer): DataView | null {
  const view = new DataView(buf);
  if (view.byteLength < 4 || view.getUint16(0) !== 0xffd8) return null;
  let offset = 2;
  while (offset + 4 <= view.byteLength) {
    const marker = view.getUint16(offset);
    if ((marker & 0xff00) !== 0xff00) return null;
    // Start of scan: the image data follows, and no metadata segment comes after it.
    if (marker === 0xffda) return null;
    const length = view.getUint16(offset + 2);
    if (marker === 0xffe1 && length >= 8 && offset + 2 + length <= view.byteLength) {
      const header = String.fromCharCode(...new Uint8Array(buf, offset + 4, 6));
      if (header === 'Exif\0\0') return new DataView(buf, offset + 10, length - 8);
    }
    offset += 2 + length;
  }
  return null;
}

/** One IFD's entries we care about, by tag: ASCII as a string, (S)RATIONAL/SHORT/LONG as numbers. */
function readIfd(tiff: DataView, ifdOffset: number, little: boolean): Map<number, TagValue> {
  const tags = new Map<number, TagValue>();
  if (ifdOffset + 2 > tiff.byteLength) return tags;
  const count = tiff.getUint16(ifdOffset, little);
  for (let i = 0; i < count; i++) {
    const entry = ifdOffset + 2 + i * 12;
    if (entry + 12 > tiff.byteLength) break;
    const tag = tiff.getUint16(entry, little);
    const type = tiff.getUint16(entry + 2, little);
    const n = tiff.getUint32(entry + 4, little);
    const size = { 1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 10: 8 }[type];
    if (size === undefined) continue;
    const at = size * n <= 4 ? entry + 8 : tiff.getUint32(entry + 8, little);
    if (at + size * n > tiff.byteLength) continue;
    if (type === 2) {
      let s = '';
      for (let j = 0; j < n; j++) {
        const c = tiff.getUint8(at + j);
        if (c === 0) break;
        s += String.fromCharCode(c);
      }
      tags.set(tag, s);
    } else {
      const values: number[] = [];
      for (let j = 0; j < n; j++) {
        const p = at + j * size;
        if (type === 1) values.push(tiff.getUint8(p));
        else if (type === 3) values.push(tiff.getUint16(p, little));
        else if (type === 4) values.push(tiff.getUint32(p, little));
        else if (type === 5) values.push(tiff.getUint32(p, little) / (tiff.getUint32(p + 4, little) || 1));
        else values.push(tiff.getInt32(p, little) / (tiff.getInt32(p + 4, little) || 1));
      }
      tags.set(tag, values);
    }
  }
  return tags;
}

/** "YYYY:MM:DD HH:MM:SS" → "YYYY-MM-DDTHH:MM:SS", or null for anything else (including the
 *  all-blank or all-zero value a camera with no clock writes). */
function exifDateTime(v: TagValue | undefined): string | null {
  if (typeof v !== 'string') return null;
  const m = /^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})/.exec(v.trim());
  if (!m || m[1] === '0000') return null;
  return `${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}`;
}

function degrees(v: TagValue | undefined, ref: TagValue | undefined, negative: string): number | undefined {
  if (!Array.isArray(v) || v.length < 3 || typeof ref !== 'string') return undefined;
  const d = v[0]! + v[1]! / 60 + v[2]! / 3600;
  if (!Number.isFinite(d)) return undefined;
  return ref.toUpperCase().startsWith(negative) ? -d : d;
}

/** A wall clock read as if it were UTC, in milliseconds — for arithmetic between two of them. */
function wallMs(local: string): number {
  return Date.parse(`${local}Z`);
}

function iso(ms: number): string {
  return new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/**
 * Reads a JPEG's capture time and position. The capture time is DateTimeOriginal (or the
 * file's DateTime). Its zone comes from OffsetTimeOriginal when the camera wrote one; else from
 * the GPS clock, which is UTC — the camera's wall clock minus the GPS time, rounded to the
 * nearest 15 minutes, is its zone, and that keeps the camera's own seconds rather than the GPS
 * fix's, which can be a little stale. With neither, the time goes as a zoneless wall clock.
 */
export function readExif(buf: ArrayBuffer): PhotoExif {
  const tiff = findTiff(buf);
  if (!tiff || tiff.byteLength < 8) return {};
  const order = tiff.getUint16(0);
  if (order !== 0x4949 && order !== 0x4d4d) return {};
  const little = order === 0x4949;
  const ifd0 = readIfd(tiff, tiff.getUint32(4, little), little);
  const pointer = (tag: number) => {
    const v = ifd0.get(tag);
    return Array.isArray(v) && v.length > 0 ? v[0]! : null;
  };
  const exifAt = pointer(TAG_EXIF_IFD);
  const gpsAt = pointer(TAG_GPS_IFD);
  const exif = exifAt !== null ? readIfd(tiff, exifAt, little) : new Map<number, TagValue>();
  const gps = gpsAt !== null ? readIfd(tiff, gpsAt, little) : new Map<number, TagValue>();

  const out: PhotoExif = {};
  const lat = degrees(gps.get(GPS_LAT), gps.get(GPS_LAT_REF), 'S');
  const lon = degrees(gps.get(GPS_LON), gps.get(GPS_LON_REF), 'W');
  if (lat !== undefined && lon !== undefined && Math.abs(lat) <= 90 && Math.abs(lon) <= 180 && (lat !== 0 || lon !== 0)) {
    out.lat = lat;
    out.lon = lon;
  }

  const local = exifDateTime(exif.get(TAG_DATETIME_ORIGINAL)) ?? exifDateTime(ifd0.get(TAG_DATETIME));
  const gpsDate = gps.get(GPS_DATE);
  const gpsTime = gps.get(GPS_TIME);
  let gpsMs: number | null = null;
  if (typeof gpsDate === 'string' && Array.isArray(gpsTime) && gpsTime.length >= 3) {
    const d = /^(\d{4}):(\d{2}):(\d{2})$/.exec(gpsDate.trim());
    if (d) gpsMs = Date.UTC(+d[1]!, +d[2]! - 1, +d[3]!, gpsTime[0]!, gpsTime[1]!, Math.floor(gpsTime[2]!));
  }

  const offset = exif.get(TAG_OFFSET_TIME_ORIGINAL);
  const offsetMatch = typeof offset === 'string' ? /^([+-])(\d{2}):(\d{2})$/.exec(offset.trim()) : null;
  if (local && offsetMatch) {
    const minutes = (offsetMatch[1] === '-' ? -1 : 1) * (+offsetMatch[2]! * 60 + +offsetMatch[3]!);
    out.takenAt = iso(wallMs(local) - minutes * 60_000);
  } else if (local && gpsMs !== null && Number.isFinite(gpsMs)) {
    const quarter = 15 * 60_000;
    const zone = Math.round((wallMs(local) - gpsMs) / quarter) * quarter;
    out.takenAt = zone >= -12 * 3_600_000 && zone <= 14 * 3_600_000 ? iso(wallMs(local) - zone) : iso(gpsMs);
  } else if (local) {
    out.takenLocal = local;
  } else if (gpsMs !== null && Number.isFinite(gpsMs)) {
    out.takenAt = iso(gpsMs);
  }
  return out;
}

/** A picked photo ready for `POST /v1/photos`: the resized copy, its thumbnail, and its EXIF. */
export interface PreparedPhoto {
  file: Blob;
  thumb: Blob;
  exif: PhotoExif;
}

/** Thrown when the browser can't decode the picked file (most HEIC files outside Safari). */
export class UnreadablePhotoError extends Error {}

function toBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, type, quality));
}

/** The picked file as a loaded `<img>`: its header read, its natural size the picture as it's
 *  shown — turned the way its EXIF Orientation says, as drawing it also is — and its pixels not
 *  yet decoded. */
async function loadImage(file: File): Promise<HTMLImageElement> {
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    await new Promise((resolve, reject) => {
      img.onload = resolve;
      img.onerror = reject;
      img.src = url;
    });
    if (img.naturalWidth === 0 || img.naturalHeight === 0) throw new UnreadablePhotoError();
    return img;
  } catch {
    throw new UnreadablePhotoError();
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** The file decoded by WebCodecs straight at width × height — the upright size — or null where
 *  that isn't what comes back: no ImageDecoder, a type it doesn't take, or a frame at another
 *  size (Chromium treats the size as a hint, scaling a JPEG by a power of two below it). */
async function decodeAt(file: File, width: number, height: number): Promise<HTMLCanvasElement | null> {
  if (typeof ImageDecoder === 'undefined' || !file.type) return null;
  try {
    if (!(await ImageDecoder.isTypeSupported(file.type))) return null;
    const decoder = new ImageDecoder({ data: file.stream(), type: file.type, desiredWidth: width, desiredHeight: height });
    try {
      const { image } = await decoder.decode();
      try {
        if (image.displayWidth !== width || image.displayHeight !== height) return null;
        return draw(image, width, height, Math.max(width, height));
      } finally {
        image.close();
      }
    } finally {
      decoder.close();
    }
  } catch {
    return null;
  }
}

/** The source drawn at most maxSide on its long side. */
function draw(source: CanvasImageSource, width: number, height: number, maxSide: number): HTMLCanvasElement {
  const scale = Math.min(1, maxSide / Math.max(width, height));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(width * scale));
  canvas.height = Math.max(1, Math.round(height * scale));
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new UnreadablePhotoError();
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(source, 0, 0, canvas.width, canvas.height);
  return canvas;
}

/** The canvas as WebP — or JPEG where the browser can't encode WebP (Safari hands back a PNG
 *  instead, which the server refuses). */
async function encode(canvas: HTMLCanvasElement): Promise<Blob> {
  const webp = await toBlob(canvas, 'image/webp', QUALITY);
  if (webp && webp.type === 'image/webp') return webp;
  const jpeg = await toBlob(canvas, 'image/jpeg', QUALITY);
  if (!jpeg) throw new UnreadablePhotoError();
  return jpeg;
}

/** How much of a file readExif sees: a JPEG's metadata segments come before its image data,
 *  each at most 64 KB, so the Exif block sits well inside this — and a large photo isn't read
 *  into memory whole just for it. */
const EXIF_SCAN_BYTES = 1 << 20;

export async function preparePhoto(file: File): Promise<PreparedPhoto> {
  let exif: PhotoExif = {};
  try {
    exif = readExif(await file.slice(0, EXIF_SCAN_BYTES).arrayBuffer());
  } catch {
    // A malformed EXIF block isn't a reason to refuse the picture; it just goes unplaced.
  }
  // A large photo isn't decoded at full size (4 bytes a pixel: 770 MB for 192 MP) where the
  // browser can avoid it. ImageDecoder decodes straight at the target size where it honors one
  // (Firefox); otherwise the <img> is drawn onto a canvas of the target size, which Chromium
  // decodes a JPEG for at a fraction of its size and Safari subsamples. createImageBitmap
  // decodes it whole in all three, even when asked for a smaller one. The thumbnail is drawn
  // from the resized copy.
  const img = await loadImage(file);
  const scale = Math.min(1, PHOTO_MAX_SIDE / Math.max(img.naturalWidth, img.naturalHeight));
  const width = Math.max(1, Math.round(img.naturalWidth * scale));
  const height = Math.max(1, Math.round(img.naturalHeight * scale));
  let photo = scale < 1 ? await decodeAt(file, width, height) : null;
  // Not img.decode() first: that decodes it at full size, where drawing it leaves the size to
  // the browser.
  photo ??= draw(img, img.naturalWidth, img.naturalHeight, PHOTO_MAX_SIDE);
  const thumb = draw(photo, photo.width, photo.height, PHOTO_THUMB_SIDE);
  return { file: await encode(photo), thumb: await encode(thumb), exif };
}
