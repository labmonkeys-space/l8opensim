import React, {
  type ReactNode,
  useCallback,
  useRef,
  useState,
} from 'react';
import OriginalMermaid from '@theme-original/Mermaid';
import type MermaidType from '@theme/Mermaid';
import type {WrapperProps} from '@docusaurus/types';
import MermaidZoomOverlay from './MermaidZoomOverlay';
import styles from './styles.module.css';

type Props = WrapperProps<typeof MermaidType>;

export default function MermaidWrapper(props: Props): ReactNode {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [overlaySvg, setOverlaySvg] = useState<string | null>(null);

  const handleOpen = useCallback(() => {
    const svg = wrapperRef.current?.querySelector('svg');
    if (!svg) {
      return; // Mermaid hasn't finished rendering yet — no-op click.
    }
    setOverlaySvg(svg.outerHTML);
  }, []);

  const handleClose = useCallback(() => {
    setOverlaySvg(null);
    wrapperRef.current?.focus({preventScroll: true});
  }, []);

  return (
    <>
      <div
        ref={wrapperRef}
        className={styles.wrapper}
        onClick={handleOpen}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            handleOpen();
          }
        }}
        role="button"
        tabIndex={0}
        aria-label="Click to enlarge diagram">
        <OriginalMermaid {...props} />
      </div>
      <MermaidZoomOverlay svg={overlaySvg} onClose={handleClose} />
    </>
  );
}
