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

// Intentionally no id-rewriting on the cloned SVG.
//
// The overlay keeps the inline diagram's ids intact, which technically
// produces duplicate ids in the document while the modal is open. A
// previous iteration of this code tried to suffix every id="…" plus the
// id references in `url(#…)` / `href="#…"` contexts to avoid that, but:
//
//   1. Mermaid emits markers as `url(<absolute-page-url>#<id>)` — the
//      narrow `url(#…)` pattern does not match, so marker references
//      silently break.
//   2. Mermaid ships per-diagram styles in an embedded `<style>` block
//      using `#<root-svg-id>` selectors. Those selectors would also need
//      to be rewritten; doing so via regex risks clobbering hex colour
//      literals inside the same CSS block.
//
// In combination those partial rewrites left the cloned diagram with no
// matching CSS for its edge paths (paths fell back to default black fill)
// and with markers pointing at non-existent ids — edge artefacts and
// missing arrowheads. Keeping the duplicate ids for the brief modal
// lifetime is the pragmatic choice: marker references resolve to the
// first matching element (the inline diagram's marker, which has the
// identical definition), CSS selectors apply to both copies with the
// same result, and the only downside is a transient HTML-validity warning
// that no visible or audible client reacts to.
//
// Sizing: handled by CSS. `.inner` has an explicit `width: 95vw` so
// mermaid's emitted `<svg width="100%">` resolves to a concrete pixel
// width. Without a definite parent width the SVG collapses to its
// foreignObject minimum or to zero.

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
    setOverlaySvg(svg.outerHTML);
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
