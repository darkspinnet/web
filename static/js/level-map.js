document.querySelectorAll('[data-map-viewport]').forEach(tile => {
  const map = tile.querySelector('.level-seeded-map');
  const controls = tile.querySelector('.level-map-navigation');
  const zoomIn = controls.querySelector('[data-map-action="in"]');
  const zoomOut = controls.querySelector('[data-map-action="out"]');
  const reset = controls.querySelector('[data-map-action="reset"]');
  const fullSize = map.viewBox.baseVal.width;
  const minSize = fullSize / 4;
  let size = fullSize;
  let x = 0;
  let y = 0;
  let drag = null;

  const clamp = (value, min, max) => Math.min(Math.max(value, min), max);
  const render = () => {
    x = clamp(x, 0, fullSize - size);
    y = clamp(y, 0, fullSize - size);
    map.setAttribute('viewBox', `${x} ${y} ${size} ${size}`);
    const zoomed = size < fullSize;
    map.classList.toggle('is-zoomed', zoomed);
    zoomOut.disabled = reset.disabled = !zoomed;
    zoomIn.disabled = size === minSize;
  };

  const zoom = (factor, anchorX = 0.5, anchorY = 0.5) => {
    const next = clamp(size * factor, minSize, fullSize);
    if (next === size) return false;
    x += (size - next) * anchorX;
    y += (size - next) * anchorY;
    size = next;
    render();
    return true;
  };
  const resetView = () => {
    size = fullSize;
    render();
  };

  map.addEventListener('wheel', event => {
    if (event.ctrlKey || event.metaKey || !event.deltaY) return;
    const rect = map.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const anchorX = clamp((event.clientX - rect.left) / rect.width, 0, 1);
    const anchorY = clamp((event.clientY - rect.top) / rect.height, 0, 1);
    if (zoom(event.deltaY < 0 ? 0.85 : 1 / 0.85, anchorX, anchorY)) event.preventDefault();
  }, {passive: false});

  map.addEventListener('pointerdown', event => {
    if (size === fullSize || event.button !== 0 || !event.isPrimary) return;
    drag = {id: event.pointerId, clientX: event.clientX, clientY: event.clientY};
    map.setPointerCapture(event.pointerId);
    map.classList.add('is-dragging');
  });
  map.addEventListener('pointermove', event => {
    if (drag?.id !== event.pointerId) return;
    const rect = map.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    x -= (event.clientX - drag.clientX) * size / rect.width;
    y -= (event.clientY - drag.clientY) * size / rect.height;
    drag.clientX = event.clientX;
    drag.clientY = event.clientY;
    render();
  });
  map.addEventListener('lostpointercapture', () => {
    drag = null;
    map.classList.remove('is-dragging');
  });

  map.addEventListener('keydown', event => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === '+' || event.key === '=') zoom(0.8);
    else if (event.key === '-') zoom(1.25);
    else if (event.key === '0' || event.key === 'Escape') resetView();
    else if (size < fullSize) {
      const step = size / 8;
      switch (event.key) {
        case 'ArrowLeft': x -= step; break;
        case 'ArrowRight': x += step; break;
        case 'ArrowUp': y -= step; break;
        case 'ArrowDown': y += step; break;
        default: return;
      }
      render();
    } else return;
    event.preventDefault();
  });

  zoomIn.addEventListener('click', () => zoom(0.8));
  zoomOut.addEventListener('click', () => zoom(1.25));
  reset.addEventListener('click', resetView);
  map.setAttribute('tabindex', '0');
  render();
  controls.hidden = false;
});
