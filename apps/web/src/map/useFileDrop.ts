import { useRef, useState, type DragEvent } from 'react';

export interface FileDrop {
  /** Files are being dragged over the map. */
  over: boolean;
  handlers: {
    onDragEnter: (event: DragEvent) => void;
    onDragOver: (event: DragEvent) => void;
    onDragLeave: (event: DragEvent) => void;
    onDrop: (event: DragEvent) => void;
  };
}

/**
 * Files dropped on the map go to the header's Upload menu (`hmt:upload-files`). A demo account
 * has no Upload menu, so with [enabled] off the map takes no drop. The cover counts enters and
 * leaves, since each child the drag crosses fires a pair.
 */
export function useFileDrop(enabled: boolean): FileDrop {
  const [over, setOver] = useState(false);
  const depthRef = useRef(0);
  const carriesFiles = (event: DragEvent) => enabled && Array.from(event.dataTransfer.types).includes('Files');
  return {
    over,
    handlers: {
      onDragEnter: (event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        depthRef.current += 1;
        setOver(true);
      },
      onDragOver: (event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = 'copy';
      },
      onDragLeave: (event) => {
        if (!carriesFiles(event)) return;
        depthRef.current = Math.max(0, depthRef.current - 1);
        if (depthRef.current === 0) setOver(false);
      },
      onDrop: (event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        depthRef.current = 0;
        setOver(false);
        window.dispatchEvent(new CustomEvent('hmt:upload-files', { detail: event.dataTransfer.files }));
      },
    },
  };
}
