import { useEffect, useRef, useState } from 'react';

/**
 * The header's "About" menu — a text trigger and a dropdown of links into the static
 * about.html page, rather than one "About HoldMyTrack" item buried in the account menu.
 * Everything here is a plain link, not a navigation callback: about.html lives outside this
 * app, so each entry is a real page load (and Back returns here). The dropdown behaves like
 * UserMenu's: dismissed by a click anywhere else or by Escape.
 */
const LINKS = [
  { href: '/about', label: 'About HoldMyTrack' },
  { href: '/about#funding', label: 'How it’s funded' },
  { href: '/about#contact', label: 'Contact' },
];

export function AboutMenu() {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  return (
    <div className="about-menu" ref={rootRef} data-testid="about-menu">
      <button
        type="button"
        className="about-menu__trigger"
        data-testid="about-menu-button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((was) => !was)}
      >
        About
        <svg className="about-menu__caret" viewBox="0 0 10 6" aria-hidden="true" focusable="false">
          <path d="M1 1l4 4 4-4" />
        </svg>
      </button>

      {open && (
        <div className="user-menu__dropdown" role="menu" data-testid="about-menu-dropdown">
          {LINKS.map((link) => (
            <a key={link.href} role="menuitem" className="user-menu__item" href={link.href}>
              {link.label}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
