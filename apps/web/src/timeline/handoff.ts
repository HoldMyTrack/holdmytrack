// A Timeline file chosen in the header's Upload menu on a page other than the map: the menu
// (services/server/internal/web/static/upload.js) stores the File in IndexedDB, which keeps a
// File whole, and goes to /?import=timeline, where the map takes it from here. The names below
// are the script's HANDOFF_* too. A browser without IndexedDB, or a failed read, leaves the
// window to ask for the file again.
const DB = 'hmt-handoff';
const STORE = 'files';
const KEY = 'timeline';

export function takeHandedOffFile(): Promise<File | null> {
  return new Promise((resolve) => {
    let req: IDBOpenDBRequest;
    try {
      req = indexedDB.open(DB, 1);
    } catch {
      resolve(null);
      return;
    }
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onerror = () => resolve(null);
    req.onsuccess = () => {
      const db = req.result;
      try {
        const tx = db.transaction(STORE, 'readwrite');
        const store = tx.objectStore(STORE);
        const get = store.get(KEY);
        get.onsuccess = () => {
          store.delete(KEY);
          resolve(get.result instanceof File ? get.result : null);
        };
        get.onerror = () => resolve(null);
        tx.oncomplete = () => db.close();
      } catch {
        db.close();
        resolve(null);
      }
    };
  });
}
