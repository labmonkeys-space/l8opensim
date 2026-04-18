(function () {
  'use strict';

  // Material 9.x renders Mermaid into a closed shadow DOM
  // (`attachShadow({mode: "closed"})`), which blocks both external CSS
  // from styling the SVG and external JS from querying it. Force
  // `mode: "open"` so the overlay can clone the rendered SVG. Scoped in
  // practice because Material only calls attachShadow for Mermaid.
  const origAttachShadow = Element.prototype.attachShadow;
  Element.prototype.attachShadow = function (opts) {
    return origAttachShadow.call(this, Object.assign({}, opts, { mode: 'open' }));
  };

  let overlay;
  let lastFocus;

  function ensureOverlay() {
    if (overlay) return overlay;
    overlay = document.createElement('div');
    overlay.className = 'mermaid-zoom-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-label', 'Enlarged diagram');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('tabindex', '-1');
    overlay.innerHTML = '<div class="mermaid-zoom-inner"></div>';
    overlay.addEventListener('click', closeOverlay);
    document.body.appendChild(overlay);
    return overlay;
  }

  function openOverlay(host) {
    const svg = host.shadowRoot && host.shadowRoot.querySelector('svg');
    if (!svg) return;
    const o = ensureOverlay();
    const inner = o.querySelector('.mermaid-zoom-inner');
    inner.innerHTML = '';
    const clone = svg.cloneNode(true);
    clone.setAttribute('preserveAspectRatio', 'xMidYMid meet');
    clone.removeAttribute('style');
    inner.appendChild(clone);
    lastFocus = document.activeElement;
    o.classList.add('open');
    o.focus();
    document.body.style.overflow = 'hidden';
  }

  function closeOverlay() {
    if (!overlay) return;
    overlay.classList.remove('open');
    overlay.querySelector('.mermaid-zoom-inner').innerHTML = '';
    document.body.style.overflow = '';
    if (lastFocus && typeof lastFocus.focus === 'function') lastFocus.focus();
    lastFocus = null;
  }

  function enhance(host) {
    if (host.dataset.zoomEnhanced === '1') return;
    if (!host.shadowRoot || !host.shadowRoot.querySelector('svg')) return;
    host.dataset.zoomEnhanced = '1';
    host.addEventListener('click', function (e) {
      e.stopPropagation();
      openOverlay(host);
    });
  }

  function enhanceAll() {
    document.querySelectorAll('div.mermaid').forEach(enhance);
  }

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && overlay && overlay.classList.contains('open')) closeOverlay();
  });

  new MutationObserver(function (mutations) {
    for (const m of mutations) {
      for (const node of m.addedNodes) {
        if (node.nodeType !== 1) continue;
        if (node.matches && node.matches('div.mermaid')) {
          enhance(node);
        } else if (node.querySelectorAll) {
          node.querySelectorAll('div.mermaid').forEach(enhance);
        }
      }
    }
  }).observe(document.body, { childList: true, subtree: true });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', enhanceAll);
  } else {
    enhanceAll();
  }
})();
