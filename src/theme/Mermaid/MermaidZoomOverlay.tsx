import React, {useEffect, useRef} from 'react';
import {createPortal} from 'react-dom';
import BrowserOnly from '@docusaurus/BrowserOnly';
import styles from './styles.module.css';

type Props = {
  svg: string | null;
  onClose: () => void;
};

function OverlayBody({svg, onClose}: {svg: string; onClose: () => void}) {
  const overlayRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Move focus into the overlay for screen-reader announcement and to make
    // subsequent keyboard events reach our keydown handler reliably.
    overlayRef.current?.focus({preventScroll: true});

    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  }, [onClose]);

  return createPortal(
    <div
      ref={overlayRef}
      className={`${styles.overlay} ${styles.overlayOpen}`}
      role="dialog"
      aria-modal="true"
      aria-label="Enlarged diagram"
      tabIndex={-1}
      onClick={onClose}>
      <div
        className={styles.inner}
        onClick={(e) => e.stopPropagation()}
        dangerouslySetInnerHTML={{__html: svg}}
      />
    </div>,
    document.body,
  );
}

export default function MermaidZoomOverlay({svg, onClose}: Props) {
  if (svg === null) {
    return null;
  }
  return (
    <BrowserOnly>
      {() => <OverlayBody svg={svg} onClose={onClose} />}
    </BrowserOnly>
  );
}
