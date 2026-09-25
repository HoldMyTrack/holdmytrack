/**
 * The header shared by the static pages (about.html, help.html), written once here instead of
 * copied into each file. vite.config.ts's staticHeader plugin swaps it in for the
 * `<!-- static-header -->` placeholder before Vite processes the page, so the logo reference
 * below still comes out as a hashed /assets/ URL, and it runs in dev as well as the build.
 *
 * It mirrors the app's Header.tsx (brand, divider, tagline, then the Info menu) with "Open the
 * app" in place of the app-only actions. The Info menu is a <details> rather than
 * InfoMenu.tsx's button: these pages have no script, and <details> opens and closes without
 * one. The entries match InfoMenu.tsx's LINKS; keep them in step.
 */
const LINKS = [
  { href: '/about', label: 'About' },
  { href: '/help', label: 'Help' },
];

/** `page` is the current page's path (e.g. '/help'), marked aria-current in the Info menu. */
export function staticHeader(page: string): string {
  const items = LINKS.map(
    (link) =>
      `<a role="menuitem" class="info-menu__item" href="${link.href}"${link.href === page ? ' aria-current="page"' : ''}>${link.label}</a>`,
  ).join('\n          ');
  return `<header class="about-header">
      <a class="about-header__brand" href="/">
        <img class="about-header__logo" src="/src/assets/logo.png" alt="" />
        <span class="about-wordmark"><span class="about-wordmark__light">HoldMy</span><span class="about-wordmark__bold">Track</span></span>
      </a>
      <span class="about-header__divider" aria-hidden="true"></span>
      <span class="about-header__tagline">Every journey, mapped.</span>
      <nav class="about-header__actions" aria-label="Main">
        <details class="info-menu">
          <summary class="info-menu__trigger">Info<svg class="info-menu__caret" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></summary>
          <div class="info-menu__dropdown" role="menu">
          ${items}
          </div>
        </details>
        <a class="about-button about-button--quiet" href="/">Open the app</a>
      </nav>
    </header>`;
}
