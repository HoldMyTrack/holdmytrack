import { useEffect, useState } from 'react';
import { getPersonalBests, type PersonalBest } from '../api';

export interface PersonalBestsState {
  personalBests: PersonalBest[] | null;
  error: string | null;
}

/**
 * Fetches VISION.md §5.3's personal bests once on mount — unlike useTrends/
 * useBestEfforts, there's no toggle here (one account has exactly one set of bests), so
 * there's nothing to refetch on.
 */
export function usePersonalBests(): PersonalBestsState {
  const [personalBests, setPersonalBests] = useState<PersonalBest[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    getPersonalBests(controller.signal)
      .then(setPersonalBests)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, []);

  return { personalBests, error };
}
