import { useEffect, useRef, useState } from 'react';
import { ChevronDown } from 'lucide-react';

/**
 * The header's "Info" menu — a text trigger and a dropdown of links to the pages about the app
 * itself: About, Help and Contacts. Everything here is a plain link, not a navigation callback:
 * those pages are server-rendered (services/server/internal/web), outside this app, so each
 * entry is a real page load (and Back returns here). The dropdown behaves like UserMenu's:
 * dismissed by a click anywhere else or by Escape. The pages' own header (web.go's InfoLinks)
 * lists the same entries; keep them in step.
 */
const LINKS = [
  { href: '/about', label: 'About' },
  { href: '/help', label: 'Help' },
  { href: '/contacts', label: 'Contacts' },
];

export function InfoMenu() {
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
    <div className="info-menu" ref={rootRef} data-testid="info-menu">
      <button
        type="button"
        className="info-menu__trigger"
        data-testid="info-menu-button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((was) => !was)}
      >
        Info
        <ChevronDown className="info-menu__caret" size={14} />
      </button>

      {open && (
        <div className="user-menu__dropdown" role="menu" data-testid="info-menu-dropdown">
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
