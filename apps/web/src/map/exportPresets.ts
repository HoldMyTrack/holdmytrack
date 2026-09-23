/**
 * Platform image-size presets for the frame-and-capture export flow (`ExportFrame.tsx`'s shape
 * dropdown, `exportMap.ts`'s output size). Dimensions from Hootsuite's social-media-image-sizes guide
 * (blog.hootsuite.com/social-media-image-sizes-guide), profile-picture and cover-photo sizes
 * excluded — this feature frames map content, not an account avatar. A standalone data file
 * rather than inline in a component, since both the shape dropdown and the capture pipeline's
 * target-dimension lookup need it, and platform-declared sizes are the kind of thing worth
 * being able to find and refresh in one place without hunting through UI code.
 *
 * `group`/`row` are the dropdown's structure — one option group per platform, one option per
 * resolution row — not just a label; `EXPORT_PLATFORMS`/`EXPORT_PRESET_ROWS` fix that order so
 * the dropdown doesn't hardcode a second copy of it.
 */
export interface ExportPreset {
  id: string;
  group: string;
  row: string;
  widthPx: number;
  heightPx: number;
}

export const EXPORT_PLATFORMS = ['Instagram', 'Facebook', 'X'] as const;
export const EXPORT_PRESET_ROWS = ['Square', 'Portrait', 'Landscape', 'Story'] as const;

export const EXPORT_PRESETS: ExportPreset[] = [
  { id: 'instagram-square', group: 'Instagram', row: 'Square', widthPx: 1080, heightPx: 1080 },
  { id: 'instagram-portrait', group: 'Instagram', row: 'Portrait', widthPx: 1080, heightPx: 1350 },
  { id: 'instagram-landscape', group: 'Instagram', row: 'Landscape', widthPx: 1080, heightPx: 566 },
  { id: 'instagram-story', group: 'Instagram', row: 'Story', widthPx: 1080, heightPx: 1920 },
  { id: 'facebook-square', group: 'Facebook', row: 'Square', widthPx: 1080, heightPx: 1080 },
  { id: 'facebook-portrait', group: 'Facebook', row: 'Portrait', widthPx: 1080, heightPx: 1359 },
  { id: 'facebook-landscape', group: 'Facebook', row: 'Landscape', widthPx: 1080, heightPx: 566 },
  { id: 'facebook-story', group: 'Facebook', row: 'Story', widthPx: 1080, heightPx: 1920 },
  { id: 'x-square', group: 'X', row: 'Square', widthPx: 1080, heightPx: 1080 },
  { id: 'x-landscape', group: 'X', row: 'Landscape', widthPx: 1280, heightPx: 720 },
];
