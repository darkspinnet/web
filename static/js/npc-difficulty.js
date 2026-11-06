(() => {
  const controls = document.querySelector('[data-npc-difficulty]');
  if (!controls) return;
  const mode = controls.querySelector('[data-npc-mode]');
  const mission = controls.querySelector('[data-npc-mission]');
  const party = controls.querySelector('[data-npc-party-mode]');
  const players = controls.querySelector('[data-npc-players]');
  const summary = controls.querySelector('[data-npc-difficulty-summary]');
  const offsets = { onslaught: 0, invasion: 24, apocalypse: 48 };
  let stored = {};
  try { stored = JSON.parse(localStorage.getItem('npc-difficulty') || '{}') || {}; } catch {}
  const params = new URL(location.href).searchParams;
  const choice = (name, fallback) => params.get(name) ?? stored[name] ?? fallback;
  const initialMode = choice('mode', 'onslaught');
  const integer = (value, min, max, fallback) => Number.isInteger(Number(value)) && Number(value) >= min && Number(value) <= max ? Number(value) : fallback;
  const state = { mode: Object.hasOwn(offsets, initialMode) ? initialMode : 'onslaught', mission: integer(choice('mission', 1), 1, 24, 1), players: integer(choice('players', 1), 1, 4, 1) };
  let tuning;
  const format = value => Number(value).toLocaleString(undefined, {maximumFractionDigits:3});
  const stage = () => tuning.difficulties[offsets[state.mode] + state.mission - 1];
  const damageMultiplier = () => Math.fround(Math.fround(tuning.party_damage[state.players - 1]) * Math.fround(stage().damage));
  const partyHealth = coefficient => Math.fround(1 + Math.fround((state.players - 1) * Math.fround(coefficient)));
  const health = (base, coefficient, policy) => {
    if (!tuning || base == null || policy === 'base') return base;
    const partyBase = Math.fround(Math.fround(base) * partyHealth(coefficient));
    return Math.fround(partyBase * Math.fround(stage().health));
  };
  const urlFor = href => {
    const url = new URL(href, location.href);
    for (const [key, value] of Object.entries(state)) url.searchParams.set(key, value);
    return url.pathname + url.search + url.hash;
  };
  const renderProfile = () => {
    const profile = document.querySelector('[data-npc-profile]');
    if (!profile) return;
    const {baseHp, partyHealth: coefficient, healthPolicy} = profile.dataset;
    if (baseHp !== '') {
      const hp = health(Number(baseHp), Number(coefficient), healthPolicy);
      profile.querySelector('[data-npc-health]').textContent = format(Math.round(hp));
      profile.querySelector('[data-npc-health-basis]').title = healthPolicy === 'base'
        ? `${format(baseHp)} fixed health`
        : `${format(baseHp)} base × ${format(stage().health)} difficulty × ${format(partyHealth(Number(coefficient)))} party`;
    }
    const scalar = healthPolicy === 'base' ? 1 : damageMultiplier();
    profile.querySelectorAll('[data-npc-combat-range]').forEach(container => {
      const value = base => {
        const scaled = Math.fround(Number(base) * scalar);
        return format(container.hasAttribute('data-round') ? Math.round(scaled) : scaled);
      };
      const min = value(container.dataset.min);
      const max = value(container.dataset.max);
      container.textContent = min === max ? min : `${min}–${max}`;
    });
  };
  const render = () => {
    mode.value = state.mode; mission.value = String(state.mission);
    party.value = state.players === 1 ? 'solo' : 'coop';
    players.value = String(state.players === 1 ? 2 : state.players);
    controls.querySelector('[data-npc-player-field]').hidden = state.players === 1;
    const missionLabel = `${Math.floor((state.mission - 1) / 4) + 1}-${(state.mission - 1) % 4 + 1}`;
    summary.textContent = `${mode.selectedOptions[0].textContent} ${missionLabel} · ${state.players === 1 ? 'Solo' : state.players + '-player co-op'} · Damage/healing ×${format(damageMultiplier())}`;
    renderProfile();
    document.querySelectorAll('[data-npc-context-link]').forEach(link => link.href = urlFor(link.href));
    window.dispatchEvent(new CustomEvent('npc-difficulty-change'));
  };
  const change = () => {
    state.mode = mode.value; state.mission = Number(mission.value);
    state.players = party.value === 'solo' ? 1 : Number(players.value);
    try { localStorage.setItem('npc-difficulty', JSON.stringify(state)); } catch {}
    history.replaceState(null, '', urlFor(location.href));
    render();
  };
  controls.querySelectorAll('select').forEach(select => select.addEventListener('change', change));
  const ready = fetch(controls.dataset.scalingUrl).then(response => {
    if (!response.ok) throw new Error('Difficulty data unavailable');
    return response.json();
  }).then(data => {
    if (data.difficulties?.length !== 72 || data.party_damage?.length !== 4) throw new Error('Incomplete difficulty data');
    tuning = data;
    controls.querySelectorAll('select').forEach(select => select.disabled = false);
    render();
  }).catch(() => { summary.textContent = 'Difficulty preview unavailable. Showing base values.'; });
  window.npcDifficulty = {ready, health, urlFor};
})();
