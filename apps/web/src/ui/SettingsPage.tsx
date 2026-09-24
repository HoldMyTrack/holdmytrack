import { useRef, useState } from 'react';
import { API_BASE_URL, removeAvatar, updateSettings, uploadAvatar } from '../api';
import { useAuth } from '../auth/AuthContext';
import { CountryPicker } from './CountryPicker';
import { Header } from './Header';
import { TimezonePicker } from './TimezonePicker';

/**
 * The Settings page (Avatar, Name, Country, Timezone) — reached from the account menu's
 * "Settings" item (UserMenu.tsx), a separate screen from ProfilePage.tsx rather than wired
 * into `profile-v1.png`'s own still-unbuilt "Edit profile" button (a direct user choice, not
 * a default). Same page shell as ProfilePage (`Header` + a back button), since both are
 * private, full-screen detours from the map.
 *
 * Country is the field with a side effect reaching the rest of the app: it's what
 * `units.ts`'s `useUnitSystem()` derives metric-vs-imperial from, so every distance/pace/
 * elevation display everywhere (not just this page) changes the moment it's saved — that's
 * why Save calls `updateUser()` on success rather than leaving the change to a future reload.
 *
 * Avatar and Name/Country/Timezone are two independent save actions, not one combined form
 * submit: the avatar drop-zone commits on drop/pick (matching how a file picker already reads
 * as "done" the instant a file is chosen), while Name/Country/Timezone share one Save button
 * since they're plain text/picker fields with nothing to commit until asked to.
 *
 * Private locations (FR-8.1) aren't edited here — they're circles, and only the map can show
 * one — so this page just links to the map's own window for them.
 */
export interface SettingsPageProps {
  /** Absent in onboarding — there's no map to go back to yet. */
  onBack?: () => void;
  /** Lets the account menu jump straight to Profile without detouring back through the map
   *  first — the same cross-link ProfilePage offers back to Settings. Absent in onboarding. */
  onOpenProfile?: () => void;
  /** First run (FR-1.7): a verified real account with no Country lands here instead of the map
   *  (App.tsx), with no way back to a map it hasn't unlocked yet. Saving with a Country is what
   *  lets App.tsx move on — `updateUser` below changes the very field its gate reads. */
  onboarding?: boolean;
  /** Opens the map with its Private locations window. Absent in onboarding, like onBack. */
  onOpenPrivateLocations?: () => void;
}

export function SettingsPage({ onBack, onOpenProfile, onOpenPrivateLocations, onboarding = false }: SettingsPageProps) {
  const { user, updateUser } = useAuth();
  const [displayName, setDisplayName] = useState(user.displayName);
  const [country, setCountry] = useState(user.country);
  const [timezone, setTimezone] = useState(user.timezone);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // Any further edit after a successful save invalidates the "Saved" confirmation — it
  // should read as "your last save succeeded," not linger once the form no longer matches
  // what was actually saved.
  function editField<T>(setter: (v: T) => void, value: T) {
    setSaved(false);
    setter(value);
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
    // Country is required (the server rejects an empty one too) — it decides metric vs
    // imperial everywhere, so there's no sensible "unset" to save. Timezone always has a
    // value (auto-detected at signup, or UTC), so it needs no check of its own here.
    if (!country) {
      setSaveError('Choose a country before saving.');
      return;
    }
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const profile = await updateSettings({ displayName, country, timezone });
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
      <Header {...(onBack ? { onBrandClick: onBack } : {})} {...(onOpenProfile ? { onOpenProfile } : {})} />
      <main className="profile-page__body">
        {onBack && (
          <button type="button" className="profile-page__back" onClick={onBack}>
            ← Back to map
          </button>
        )}

        <section className="settings-page" aria-label="Settings">
          <h1 className="settings-page__title">{onboarding ? 'Welcome — set up your account' : 'Settings'}</h1>
          {onboarding && (
            <p className="settings-page__intro">
              Two things before your map: your country sets whether distances show in km or miles, and your timezone sets which day
              each activity falls on. You can change both here later.
            </p>
          )}

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

          {/* Country and Timezone are <div>s, not <label>s like their neighbours: a label
              wrapping a picker would forward every click inside its popover (search box
              included) to the trigger. */}
          <div className="settings-page__section">
            <span className="settings-page__label" id="settings-country-label">
              Country
            </span>
            <CountryPicker value={country} onChange={(c) => editField(setCountry, c)} labelledBy="settings-country-label" />
            <p className="settings-page__hint">
              {country ? '' : 'Required. '}Decides whether distance, pace and elevation show in km/m or mi/ft, everywhere in the app.
            </p>
          </div>

          <div className="settings-page__section">
            <span className="settings-page__label" id="settings-timezone-label">
              Timezone
            </span>
            <TimezonePicker value={timezone} onChange={(tz) => editField(setTimezone, tz)} labelledBy="settings-timezone-label" />
            <p className="settings-page__hint">
              Decides which calendar day an activity falls on everywhere in the app (the histogram, the activity graph, date filtering).
              Auto-detected from your browser when you signed up; change it here if you're somewhere else now.
            </p>
          </div>

          {onOpenPrivateLocations && (
            <div className="settings-page__section">
              <span className="settings-page__label">Private locations</span>
              <p className="settings-page__hint">
                Circles on the map — home, work — that your tracks never show inside. Any part of an activity that starts or ends
                in one is hidden everywhere, including your own map.
              </p>
              <div>
                <button type="button" className="settings-page__button" onClick={onOpenPrivateLocations}>
                  Manage on the map
                </button>
              </div>
            </div>
          )}

          {saveError && <p className="settings-page__error">{saveError}</p>}
          <div className="settings-page__save-row">
            <button
              type="button"
              className="settings-page__submit"
              disabled={saving || !country}
              title={country ? undefined : 'Choose a country first'}
              onClick={() => void handleSave()}
            >
              {saving ? 'Saving…' : onboarding ? 'Save and continue' : 'Save'}
            </button>
            {saved && !saving && <span className="settings-page__saved">Saved</span>}
          </div>
        </section>
      </main>
    </div>
  );
}
