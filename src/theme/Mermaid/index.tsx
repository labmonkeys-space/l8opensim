// --wrap swizzle of @docusaurus/theme-mermaid.
//
// Depends on theme-mermaid internal structure: we query the rendered <svg>
// from the wrapper's DOM and clone its outerHTML. If upstream renames the
// Mermaid component, changes how it renders the SVG (e.g. to a shadow root),
// or removes the inline <svg> from the output, handleOpen becomes a silent
// no-op. The --danger swizzle flag was required for exactly this reason.

import React, {type ReactNode, useCallback, useRef, useState} from 'react';
import OriginalMermaid from '@theme-original/Mermaid';
import type MermaidType from '@theme/Mermaid';
import type {WrapperProps} from '@docusaurus/types';
import MermaidZoomOverlay from './MermaidZoomOverlay';
import styles from './styles.module.css';

type Props = WrapperProps<typeof MermaidType>;

// Suffix every id="..." attribute on the cloned SVG so the inline diagram
// and the overlay copy don't both claim the same id while the overlay is
// open (HTML validity + WCAG 4.1.1). Also rewrites the matching #id
// references inside the SVG's internal CSS so mermaid's per-diagram styles
// still apply to the clone.
function uniquifySvgIds(svgHtml: string): string {
  const suffix = '-zoom';
  return svgHtml
    .replace(/id="([^"]+)"/g, `id="$1${suffix}"`)
    .replace(/#([\w-]+)/g, `#$1${suffix}`);
}

export default function MermaidWrapper(props: Props): ReactNode {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const [overlaySvg, setOverlaySvg] = useState<string | null>(null);

  const handleOpen = useCallback((e?: React.SyntheticEvent) => {
    // Let interactive children (mermaid `click` directives render <a>
    // elements inside the SVG) keep their own behaviour rather than
    // hijacking the click to open the overlay.
    const target = e?.target as Element | undefined;
    if (target?.closest('a, [onclick]')) {
      return;
    }
    const svg = wrapperRef.current?.querySelector('svg');
    if (!svg) {
      return; // Mermaid hasn't finished rendering yet — no-op click.
    }
    setOverlaySvg(uniquifySvgIds(svg.outerHTML));
  }, []);

  // Stable identity: MermaidZoomOverlay's keydown effect lists this in its
  // dep array (Escape handler). Dropping useCallback would reinstall the
  // window listener on every parent render while the overlay is open.
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
        // Keyboard activation: Enter only. Space is deliberately omitted —
        // when focus returns to this wrapper after Esc-closing the overlay,
        // a subsequent Space press would normally scroll the page; treating
        // it as an activator here re-opens the overlay unexpectedly.
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
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
