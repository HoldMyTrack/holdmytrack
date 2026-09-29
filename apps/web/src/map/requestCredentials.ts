/**
 * MapLibre's `transformRequest` for this app's maps (useMapInstance.ts, exportMap.ts): the
 * session cookie goes with requests to the app's own API origin and with nothing else.
 * Tracks/fog/heatmap tiles need it (requireAuth, server.go), and MapLibre's own fetches
 * otherwise carry no cookie, since they don't go through api.ts's fetch wrapper.
 *
 * Compared by origin, not by URL prefix: a production build's API base is "" (same origin,
 * the Dockerfile's VITE_API_BASE_URL), and every URL starts with "". A request that
 * includes credentials needs `Access-Control-Allow-Credentials: true` in the answer, which
 * neither the basemap's CDN (fonts, sprites) nor the satellite provider sends, so the browser
 * refused all of them.
 *
 * [pageHref] resolves a relative base and relative URLs, as the browser does.
 */
export function withApiCredentials(apiBase: string, pageHref: string): (url: string) => { url: string; credentials?: 'include' } {
  const apiOrigin = new URL(apiBase || '/', pageHref).origin;
  return (url) => {
    let origin: string;
    try {
      origin = new URL(url, pageHref).origin;
    } catch {
      return { url };
    }
    return origin === apiOrigin ? { url, credentials: 'include' } : { url };
  };
}
