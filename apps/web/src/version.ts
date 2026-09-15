/**
 * This build's git short-SHA (apps/web/Dockerfile's GIT_SHA build arg, baked in at build
 * time by Vite) — 'dev' outside a Docker build (local `npm run dev`/`vite build`, where the
 * arg is never set). Compared against `/healthz`'s own `version` field by VersionBanner to
 * detect a tab left open across a deploy.
 */
export const APP_VERSION: string = (import.meta.env.VITE_GIT_SHA as string | undefined) || 'dev';
