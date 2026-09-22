import { useRef, useState } from 'react';
import { API_BASE_URL, removeAvatar, updateSettings, uploadAvatar } from '../api';
import { useAuth } from '../auth/AuthContext';
import { COUNTRIES } from './countries';
import { elevationUnitLabel, feetToMeters, metersToFeet } from './format';
import { Header } from './Header';
import { TIMEZONES } from './timezones';
import { unitSystemForCountry, type UnitSystem } from './units';

const MAX_PRIVACY_TRIM_CM = 20000;

/** Converts a centimeters value (what the server always takes/returns — cm precision, not
 *  whole meters, is what lets a feet input round-trip back to the exact number typed; see
 *  IMPLEMENTATION.md's Settings-page note) to whatever the input should display it as —
 *  rounded, since the field is a plain whole-number input, the same precision
 *  `formatElevation` (format.ts) already uses for a meters-or-feet value. Used to seed the
 *  field and, in `handleCountryChange` below, to re-express it the moment Country changes
 *  which unit is implied — the field's own state is *not* kept in centimeters in between, so
 *  free typing in either unit never gets silently reconverted underneath the user mid-edit. */
function trimDisplay(cm: number, system: UnitSystem): string {
  const meters = cm / 100;
  return String(Math.round(system === 'imperial' ? metersToFeet(meters) : meters));
}

/**
 * The Settings page (Avatar, Name, Country, Timezone, Privacy Trim) — reached from the account menu's
 * "Settings" item (UserMenu.tsx), a separate screen from ProfilePage.tsx rather than wired
 * into `profile-v1.png`'s own still-unbuilt "Edit profile" button (a direct user choice, not
 * a default). Same page shell as ProfilePage (`Header` + a back button), since both are
 * private, full-screen detours from the map.
 *
 * Country is the field with a side effect reaching the rest of the app: it's what
 * `units.ts`'s `useUnitSystem()` derives metric-vs-imperial from, so every distance/pace/
 * elevation display everywhere (not just this page) changes the moment it's saved — that's
 * why Save calls `updateUser()` on success rather than leaving the change to a future reload.
 * Privacy Trim reacts to Country too, but only within this page and only before Save: its
 * field is shown (and its max) in the same unit Country implies, converted live off the local,
 * still-unsaved Country selection (`unitSystemForCountry`, not `useUnitSystem()`), so switching
 * Country here doesn't leave the trim value looking like it just changed by a factor of ~3.3.
 *
 * Avatar and Name/Country/Timezone/Privacy Trim are two independent save actions, not one
 * combined form submit: the avatar drop-zone commits on drop/pick (matching how a file picker
 * already reads as "done" the instant a file is chosen), while Name/Country/Timezone/Privacy
 * Trim share one Save button since they're plain text/number/select fields with nothing to
 * commit until asked to.
 */
export interface SettingsPageProps {
  onBack: () => void;
  /** Lets the account menu jump straight to Profile without detouring back through the map
   *  first — the same cross-link ProfilePage offers back to Settings. */
  onOpenProfile: () => void;
}

export function SettingsPage({ onBack, onOpenProfile }: SettingsPageProps) {
  const { user, updateUser } = useAuth();
  const [displayName, setDisplayName] = useState(user.displayName);
  const [country, setCountry] = useState(user.country);
  const [timezone, setTimezone] = useState(user.timezone);
  const [privacyTrim, setPrivacyTrim] = useState(() => trimDisplay(user.privacyTrimCm, unitSystemForCountry(user.country)));
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // Derived from the *local* Country field, not useUnitSystem()'s saved-account value — Privacy
  // Trim needs to react to what's picked in this form right now, before Save, the same as every
  // other field here is still just local state until then.
  const system = unitSystemForCountry(country);

  // The input's own max, in whichever unit is currently shown — MAX_PRIVACY_TRIM_CM's imperial
  // equivalent, not the same number relabeled.
  const trimMax = system === 'imperial' ? Math.round(metersToFeet(MAX_PRIVACY_TRIM_CM / 100)) : MAX_PRIVACY_TRIM_CM / 100;

  // Any further edit after a successful save invalidates the "Saved" confirmation — it
  // should read as "your last save succeeded," not linger once the form no longer matches
  // what was actually saved.
  function editField<T>(setter: (v: T) => void, value: T) {
    setSaved(false);
    setter(value);
  }

  // Country's own onChange, not a plain editField(setCountry, ...) — the one field whose edit
  // has to also touch a second field. Converts the *currently typed* Privacy Trim value into
  // the newly-implied unit right here, synchronously, in the same event handler that changes
  // `country` — deliberately not a useEffect keyed on the derived `system`, which sounds
  // equivalent but isn't as easy to reason about (it has to compare against a remembered
  // previous system via a ref, and only actually runs after the render that already changed
  // the label/max, which is exactly the kind of two-step timing that's easy to get subtly
  // wrong). Recomputing "old system, new system" directly from the event and doing the
  // conversion before either state update commits is simpler to verify by inspection and
  // guarantees the displayed number and the displayed unit change in the same tick, together.
  function handleCountryChange(nextCountry: string) {
    const prevSystem = system;
    const nextSystem = unitSystemForCountry(nextCountry);
    if (nextSystem !== prevSystem) {
      setPrivacyTrim((current) => {
        const typed = Number(current);
        if (!Number.isFinite(typed)) return current; // an empty/in-progress edit — leave it alone
        const meters = prevSystem === 'imperial' ? feetToMeters(typed) : typed;
        return trimDisplay(meters * 100, nextSystem);
      });
    }
    editField(setCountry, nextCountry);
  }

  const [avatarBusy, setAvatarBusy] = useState(false);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  async function handleAvatarFile(file: File) {
    setAvatarBusy(true);
    setAvatarError(null);
    try {
      const profile = await uploadAvatar(file);
      updateUser(profile);
    } catch (err) {
      setAvatarError(err instanceof Error ? err.message : 'avatar upload failed');
    } finally {
      setAvatarBusy(false);
    }
  }

  async function handleRemoveAvatar() {
    setAvatarBusy(true);
    setAvatarError(null);
    try {
      const profile = await removeAvatar();
      updateUser(profile);
    } catch (err) {
      setAvatarError(err instanceof Error ? err.message : 'could not remove avatar');
    } finally {
      setAvatarBusy(false);
    }
  }

  async function handleSave() {
    const typedTrim = Number(privacyTrim);
    if (!Number.isFinite(typedTrim) || typedTrim < 0 || typedTrim > trimMax) {
      setSaveError(`Privacy trim must be a number between 0 and ${trimMax} ${elevationUnitLabel(system)}.`);
      return;
    }
    // The server only ever takes/stores centimeters (services/server/internal/httpapi/
    // account.go) — rounded to the nearest cm, not the nearest meter, since privacy_trim_cm
    // is a Go int: cm precision is what lets a feet input round-trip back to the exact number
    // typed (docs/IMPLEMENTATION.md's Settings-page note), unlike the old whole-meters column.
    const trimCm = Math.round((system === 'imperial' ? feetToMeters(typedTrim) : typedTrim) * 100);
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const profile = await updateSettings({ displayName, country, privacyTrimCm: trimCm, timezone });
      updateUser(profile);
      setSaved(true);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'could not save settings');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="app-shell">
      <Header onBrandClick={onBack} onOpenProfile={onOpenProfile} />
      <main className="profile-page__body">
        <button type="button" className="profile-page__back" onClick={onBack}>
          ← Back to map
        </button>

        <section className="settings-page" aria-label="Settings">
          <h1 className="settings-page__title">Settings</h1>

          <div className="settings-page__section">
            <span className="settings-page__label">Avatar</span>
            <div
              className="settings-page__avatar-row"
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={(e) => {
                e.preventDefault();
                setDragOver(false);
                const file = e.dataTransfer.files[0];
                if (file) void handleAvatarFile(file);
              }}
            >
              <div className={`settings-page__avatar-drop${dragOver ? ' settings-page__avatar-drop--over' : ''}`}>
                {user.avatarUrl ? (
                  <img
                    className="settings-page__avatar-preview"
                    src={`${API_BASE_URL}${user.avatarUrl}`}
                    crossOrigin="use-credentials"
                    alt=""
                  />
                ) : (
                  <span className="settings-page__avatar-placeholder" aria-hidden="true">
                    ⬚
                  </span>
                )}
              </div>
              <div className="settings-page__avatar-actions">
                <button
                  type="button"
                  className="settings-page__button"
                  disabled={avatarBusy}
                  onClick={() => fileInputRef.current?.click()}
                >
                  {avatarBusy ? 'Working…' : user.avatarUrl ? 'Change avatar' : 'Browse files'}
                </button>
                {user.avatarUrl && (
                  <button type="button" className="settings-page__button" disabled={avatarBusy} onClick={() => void handleRemoveAvatar()}>
                    Remove
                  </button>
                )}
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  hidden
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    e.target.value = '';
                    if (file) void handleAvatarFile(file);
                  }}
                />
                {avatarError && <p className="settings-page__error">{avatarError}</p>}
              </div>
            </div>
          </div>

          <label className="settings-page__section">
            <span className="settings-page__label">Name</span>
            <input
              className="settings-page__input"
              type="text"
              value={displayName}
              onChange={(e) => editField(setDisplayName, e.target.value)}
              placeholder="Not set"
            />
          </label>

          <label className="settings-page__section">
            <span className="settings-page__label">Country</span>
            <select className="settings-page__input" value={country} onChange={(e) => handleCountryChange(e.target.value)}>
              <option value="">Not set (metric)</option>
              {COUNTRIES.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.name}
                </option>
              ))}
            </select>
            <p className="settings-page__hint">Decides whether distance, pace and elevation show in km/m or mi/ft, everywhere in the app.</p>
          </label>

          <label className="settings-page__section">
            <span className="settings-page__label">Timezone</span>
            <select className="settings-page__input" value={timezone} onChange={(e) => editField(setTimezone, e.target.value)}>
              {!TIMEZONES.includes(timezone) && <option value={timezone}>{timezone}</option>}
              {TIMEZONES.map((tz) => (
                <option key={tz} value={tz}>
                  {tz}
                </option>
              ))}
            </select>
            <p className="settings-page__hint">
              Decides which calendar day an activity falls on everywhere in the app (the histogram, the activity graph, date filtering).
              Auto-detected from your browser when you signed up; change it here if you're somewhere else now.
            </p>
          </label>

          <label className="settings-page__section">
            <span className="settings-page__label">Privacy trim ({elevationUnitLabel(system)})</span>
            <input
              className="settings-page__input settings-page__input--narrow"
              type="number"
              min={0}
              max={trimMax}
              value={privacyTrim}
              onChange={(e) => editField(setPrivacyTrim, e.target.value)}
            />
            <p className="settings-page__hint">
              Trims this distance from the start and end of every new track, so it doesn't reveal exactly where you started or
              finished. Applies to new uploads only — activities you've already uploaded keep the trim they were processed with.
            </p>
          </label>

          {saveError && <p className="settings-page__error">{saveError}</p>}
          <div className="settings-page__save-row">
            <button type="button" className="settings-page__submit" disabled={saving} onClick={() => void handleSave()}>
              {saving ? 'Saving…' : 'Save'}
            </button>
            {saved && !saving && <span className="settings-page__saved">Saved</span>}
          </div>
        </section>
      </main>
    </div>
  );
}
