import { useCallback, useMemo } from 'react';
import type { TypeFacet } from './activityFacets';
import { formatActivityType } from './format';
import { fold, SearchPicker, type PickerOption } from './SearchPicker';
import { t } from '../i18n';

export interface ActivityTypePickerProps {
  /** The raw `activity_type` value, e.g. "dog_walk" or "Solowheel". */
  value: string;
  onChange: (type: string) => void;
  /** Every type already in this account's loaded activities, with how many use it — the
   *  same facets the Activities panel's Type dropdown is built from (activityFacets.ts). */
  known: TypeFacet[];
  labelledBy: string;
  maxLength: number;
}

function typeOption(type: string, count?: number): PickerOption {
  return {
    value: type,
    label: formatActivityType(type) || type,
    detail: count === undefined ? '' : String(count),
    keywords: [fold(type)],
  };
}

/**
 * EditActivityDialog.tsx's Type field — the same SearchPicker as Settings' Country and
 * Timezone, over this account's existing types (formatted, with how many activities use each),
 * but open-ended: activity_type is free-form (§4.7.2), not a controlled vocabulary, so any
 * search text that doesn't exactly match an existing type offers itself as a last "Add …" row
 * and saves exactly as typed. A value that isn't among the loaded types (just added, or
 * carried by an activity whose type no other loaded activity shares) still appears in the
 * list, so the current choice is always there to see.
 */
export function ActivityTypePicker({ value, onChange, known, labelledBy, maxLength }: ActivityTypePickerProps) {
  const options = useMemo(() => {
    const list = known.map((f) => typeOption(f.type, f.count));
    if (value && !known.some((f) => f.type === value)) list.unshift(typeOption(value));
    return list;
  }, [known, value]);

  const createOption = useCallback(
    (text: string): PickerOption | null =>
      text.length > maxLength ? null : { value: text, label: t('picker.add_type', { type: text }), detail: t('picker.new'), keywords: [] },
    [maxLength],
  );

  return (
    <SearchPicker
      options={options}
      value={value}
      onChange={onChange}
      labelledBy={labelledBy}
      searchLabel={t('picker.search_type')}
      noMatches={(query) => t('picker.no_type_matches', { query })}
      placeholder={t('picker.search_type')}
      createOption={createOption}
    />
  );
}
