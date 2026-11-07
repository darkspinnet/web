(() => {
  const controls = document.querySelector('[data-npc-difficulty]');
  if (!controls) return;
  const mission = controls.querySelector('[data-npc-mission]');
  const playerButtons = controls.querySelectorAll('[data-npc-players]');
  const summary = controls.querySelector('[data-npc-difficulty-summary]');
  const offsets = { onslaught: 0, invasion: 24, apocalypse: 48 };
  let stored = {};
  try { stored = JSON.parse(localStorage.getItem('npc-difficulty') || '{}') || {}; } catch {}
  const params = new URL(location.href).searchParams;
  const choice = (name, fallback) => params.get(name) ?? stored[name] ?? fallback;
  const integer = (value, min, max, fallback) => Number.isInteger(Number(value)) && Number(value) >= min && Number(value) <= max ? Number(value) : fallback;
  const legacyStage = (mode, mission) => (Object.hasOwn(offsets, mode) ? offsets[mode] : 0) + integer(mission, 1, 24, 1);
  let initialStage = integer(stored.stage, 1, 72, legacyStage(stored.mode, stored.mission));
  if (params.has('stage')) {
    initialStage = integer(params.get('stage'), 1, 72, 1);
  } else if (params.has('mode') || params.has('mission')) {
    initialStage = legacyStage(params.get('mode') ?? stored.mode, params.get('mission') ?? stored.mission);
  }
  if (controls.dataset.stage) initialStage = integer(controls.dataset.stage, 1, 72, 1);
  const state = { stage: initialStage, players: integer(choice('players', 1), 1, 4, 1) };
  let tuning;
  const format = value => Number(value).toLocaleString(undefined, {maximumFractionDigits:3});
  const stage = () => tuning.difficulties[state.stage - 1];
  const damageMultiplier = () => Math.fround(Math.fround(tuning.party_damage[state.players - 1]) * Math.fround(stage().damage));
  const partyHealth = coefficient => Math.fround(1 + Math.fround((state.players - 1) * Math.fround(coefficient)));
  const health = (base, coefficient, policy) => {
    if (!tuning || base == null || policy === 'base') return base;
    const partyBase = Math.fround(Math.fround(base) * partyHealth(coefficient));
    return Math.fround(partyBase * Math.fround(stage().health));
  };
  const urlFor = href => {
    const url = new URL(href, location.href);
    url.searchParams.delete('mode');
    url.searchParams.delete('mission');
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
  const renderCampaign = () => {
    document.querySelectorAll('[data-campaign-enemy]').forEach(enemy => {
      const fixed = enemy.dataset.healthPolicy === 'base';
      const healthScale = fixed ? 1 : partyHealth(Number(enemy.dataset.partyHealth));
      const hp = enemy.querySelector('[data-campaign-health]');
      const hpText = format(Math.round(Number(hp.dataset.campaignHealth) * healthScale));
      hp.querySelector('strong').textContent = hpText;
      hp.setAttribute('aria-label', `${hpText} estimated HP, ${hp.dataset.baseHp} base`);
      const damage = enemy.querySelector('[data-campaign-damage]');
      if (damage) {
        const scalar = fixed ? 1 : tuning.party_damage[state.players - 1];
        const minimum = format(Math.round(Number(damage.dataset.min) * scalar));
        const maximum = format(Math.round(Number(damage.dataset.max) * scalar));
        const text = `${minimum}–${maximum}`;
        damage.querySelector('strong').textContent = text;
        damage.setAttribute('aria-label', `Estimated damage ${text}. ${damage.title}`);
      }
    });
    document.querySelectorAll('[data-campaign-health-multiplier]').forEach(value => {
      value.textContent = `${Math.round(stage().health * partyHealth(1) * 100)}%`;
      value.parentElement.title = `Health = base × ${format(stage().health)} difficulty × ${format(partyHealth(1))} party. level_modifier ${controls.dataset.levelModifier}`;
    });
    document.querySelectorAll('[data-campaign-damage-multiplier]').forEach(value => {
      value.textContent = `${Math.round(damageMultiplier() * 100)}%`;
      value.parentElement.title = `Damage = base × ${format(stage().damage)} difficulty × ${format(tuning.party_damage[state.players - 1])} party. level_modifier ${controls.dataset.levelModifier}`;
    });
  };
  const render = () => {
    if (mission) mission.value = String(state.stage);
    playerButtons.forEach(button => {
      button.setAttribute('aria-pressed', String(Number(button.dataset.npcPlayers) === state.players));
    });
    const missionLabel = `${Math.floor((state.stage - 1) / 4) + 1}-${(state.stage - 1) % 4 + 1}`;
    if (summary) summary.textContent = `Mission ${missionLabel} · ${state.players === 1 ? 'Solo' : state.players + '-player co-op'} · Damage/healing ×${format(damageMultiplier())}`;
    renderProfile();
    renderCampaign();
    document.querySelectorAll('[data-npc-context-link]').forEach(link => link.href = urlFor(link.href));
    document.querySelectorAll('[data-campaign-context-link]').forEach(link => {
      const url = new URL(link.href, location.href);
      url.searchParams.set('players', state.players);
      link.href = url.pathname + url.search + url.hash;
    });
    window.dispatchEvent(new CustomEvent('npc-difficulty-change'));
  };
  const change = () => {
    if (mission) state.stage = Number(mission.value);
    try { localStorage.setItem('npc-difficulty', JSON.stringify(state)); } catch {}
    history.replaceState(null, '', urlFor(location.href));
    render();
  };
  controls.querySelectorAll('select').forEach(select => select.addEventListener('change', change));
  playerButtons.forEach(button => button.addEventListener('click', () => {
    state.players = Number(button.dataset.npcPlayers);
    change();
  }));
  const ready = fetch(controls.dataset.scalingUrl).then(response => {
    if (!response.ok) throw new Error('Difficulty data unavailable');
    return response.json();
  }).then(data => {
    if (data.difficulties?.length !== 72 || data.party_damage?.length !== 4) throw new Error('Incomplete difficulty data');
    tuning = data;
    controls.querySelectorAll('select, [data-npc-players]').forEach(control => control.disabled = false);
    render();
  }).catch(() => {
    if (summary) summary.textContent = 'Difficulty preview unavailable. Showing base values.';
  });
  window.npcDifficulty = {ready, health, urlFor};
})();
