(() => {
  "use strict";

  // Generated catalogs store paths relative to the site root.
  const baseURL = document.body.dataset.baseUrl || "/";
  const siteURL = (path) => path.startsWith("/") && !path.startsWith("//")
    ? baseURL + path.slice(1)
    : path;

  const jsonCache = new Map();
  const normalize = (text) => (text || "").toLowerCase().trim();
  const loadJSON = (url) => {
    if (!jsonCache.has(url)) {
      jsonCache.set(url, fetch(url, { cache: "no-cache" }).then((response) => {
        if (!response.ok) throw new Error(`lookup ${response.status}`);
        return response.json();
      }));
    }
    return jsonCache.get(url);
  };

  const initializeFilters = () => {
    const input = document.querySelector("[data-search-input]");
    const list = document.querySelector("[data-filter-list]");
    if (!input || !list) return;

    const items = [...list.querySelectorAll(`.${input.dataset.searchTarget}`)];
    const counter = document.querySelector("[data-result-count]");
    const empty = document.querySelector("[data-empty-state]");
    const filterGroups = [...document.querySelectorAll("[data-filter-group]")];
    const state = { query: "", filters: {} };

    const apply = () => {
      let visible = 0;
      items.forEach((item) => {
        const matchesQuery = !state.query || normalize(item.dataset.search).includes(state.query);
        const matchesFilters = Object.entries(state.filters).every(([key, selected]) => selected === "all" || item.dataset[key] === selected);
        item.hidden = !(matchesQuery && matchesFilters);
        if (!item.hidden) visible += 1;
      });
      if (counter) counter.textContent = visible.toLocaleString();
      if (empty) empty.hidden = visible !== 0;
    };

    input.addEventListener("input", () => {
      state.query = normalize(input.value);
      apply();
    });

    filterGroups.forEach((group) => {
      const key = group.dataset.filterGroup;
      state.filters[key] = "all";
      group.addEventListener("click", (event) => {
        const button = event.target.closest("button[data-filter]");
        if (!button) return;
        group.querySelectorAll("button").forEach((candidate) => candidate.classList.toggle("is-active", candidate === button));
        state.filters[key] = button.dataset.filter;
        apply();
      });
    });
  };

  const npcRecord = (entry) => Array.isArray(entry) ? {
    name: entry[0], asset: entry[1], level: entry[2], hp: entry[3], combat: entry[4], sources: (entry[5] || "").split(",").filter(Boolean), slug: entry[6],
  } : {
    name: entry.name, asset: entry.asset_name, level: "", hp: entry.hit_points, combat: entry.is_combatant, sources: entry.sources || [], slug: entry.slug,
  };

  const renderNPC = (container, entries) => {
    const template = document.querySelector("#npc-row-template");
    const fragment = document.createDocumentFragment();
    entries.forEach((entry) => {
      const npc = npcRecord(entry);
      const row = template.content.firstElementChild.cloneNode(true);
      const status = row.querySelector('[data-field="status"]');
      status.classList.toggle("is-combat", npc.combat);
      status.setAttribute("aria-label", npc.combat ? "Verified combatant" : "Placed noun");
      const name = row.querySelector('[data-field="name"]');
      name.textContent = npc.name;
      name.href = siteURL(`/npc/${npc.slug}/`);
      row.querySelector('[data-field="asset"]').textContent = npc.asset;
      const source = row.querySelector('[data-field="source"]');
      [...(npc.level ? [npc.level] : []), ...npc.sources].forEach((label) => {
        const tag = document.createElement("i");
        tag.textContent = label;
        source.append(tag);
      });
      const hp = row.querySelector('[data-field="hp"]');
      if (npc.hp !== null && npc.hp !== undefined) {
        const number = document.createElement("b");
        number.textContent = Math.round(npc.hp).toLocaleString();
        hp.append(number, " HP");
      } else {
        hp.textContent = "—";
      }
      fragment.append(row);
    });
    container.replaceChildren(fragment);
  };

  const initializeNPC = () => {
    const input = document.querySelector("[data-npc-search]");
    if (!input) return;
    const levelList = document.querySelector("[data-npc-level-list]");
    const searchPanel = document.querySelector("[data-npc-search-results]");
    const resultList = document.querySelector("[data-npc-results]");
    const counter = document.querySelector("[data-result-count]");
    const limit = document.querySelector("[data-result-limit]");
    let timer;

    document.querySelectorAll("[data-npc-source]").forEach((group) => {
      group.addEventListener("toggle", async () => {
        if (!group.open || group.dataset.loaded) return;
        const container = group.querySelector("[data-npc-list]");
        container.textContent = "Loading entity shard…";
        try {
          const data = await loadJSON(group.dataset.npcSource);
          renderNPC(container, data.npcs);
          group.dataset.loaded = "true";
        } catch (error) {
          container.textContent = "Entity shard unavailable.";
        }
      });
    });

    const search = async () => {
      const query = normalize(input.value);
      if (!query) {
        levelList.hidden = false;
        searchPanel.hidden = true;
        counter.textContent = Number(input.dataset.total).toLocaleString();
        return;
      }
      levelList.hidden = true;
      searchPanel.hidden = false;
      resultList.textContent = "Loading compact lookup cache…";
      try {
        const index = await loadJSON(input.dataset.npcSearch);
        const matches = index.filter((entry) => normalize(`${entry[0]} ${entry[1]} ${entry[2]}`).includes(query));
        renderNPC(resultList, matches.slice(0, 250));
        counter.textContent = matches.length.toLocaleString();
        limit.hidden = matches.length <= 250;
      } catch (error) {
        resultList.textContent = "Search cache unavailable.";
        counter.textContent = "0";
      }
    };

    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(search, 70);
    });
  };

  const setField = (root, field, text) => {
    root.querySelector(`[data-field="${field}"]`).textContent = text === null || text === undefined || text === "" ? "—" : text;
  };

  const slug = (text) => normalize(text).replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

  const initializeLevel = () => {
    const template = document.querySelector("#level-report-template");
    if (!template) return;
    document.querySelectorAll("[data-level-source]").forEach((report) => {
      report.addEventListener("toggle", async () => {
        if (!report.open || report.dataset.loaded) return;
        const body = report.querySelector("[data-level-body]");
        body.textContent = "Loading report shard…";
        try {
          const data = await loadJSON(report.dataset.levelSource);
          const content = template.content.firstElementChild.cloneNode(true);
          setField(content, "marker-set", data.marker_set_count);
          setField(content, "event", data.event_count);
          setField(content, "script", data.script_count);
          setField(content, "type", `${data.primary_type}.${data.secondary_type}`);
          setField(content, "planet", data.planet_config);
          setField(content, "rendering", data.rendering_config);
          setField(content, "music", data.music);
          setField(content, "navigation", data.navigation_mesh);
          setField(content, "physics", data.physics_mesh);
          const noun = content.querySelector('[data-field="noun"]');
          data.npcs.slice(0, 40).forEach((name) => {
            const tag = document.createElement("a");
            tag.textContent = name.replace(/\.Noun$/, "");
            tag.href = siteURL(`/npc/${slug(name)}/`);
            noun.append(tag);
          });
          if (data.npcs.length > 40) {
            const more = document.createElement("span");
            more.textContent = `+${data.npcs.length - 40} MORE`;
            noun.append(more);
          }
          const alias = content.querySelector('[data-field="alias"]');
          data.aliases.forEach((name) => {
            const code = document.createElement("code");
            code.textContent = name;
            alias.append(code, " ");
          });
          body.replaceChildren(content);
          report.dataset.loaded = "true";
        } catch (error) {
          body.textContent = "Report shard unavailable.";
        }
      });
    });
  };

  const initializeGlobalSearch = () => {
    const input = document.querySelector("[data-global-search]");
    const result = document.querySelector("[data-global-result]");
    if (!input || !result) return;
    let timer;

    const close = () => {
      result.hidden = true;
      result.replaceChildren();
    };

    const search = async () => {
      const query = normalize(input.value);
      if (!query) {
        close();
        return;
      }
      result.hidden = false;
      result.textContent = "Loading archive index…";
      try {
        const index = await loadJSON(input.dataset.globalSearch);
        const matches = index
          .filter((entry) => entry[4].includes(query))
          .sort((left, right) => Number(normalize(right[0]).startsWith(query)) - Number(normalize(left[0]).startsWith(query)) || left[0].localeCompare(right[0]))
          .slice(0, 12);
        const fragment = document.createDocumentFragment();
        matches.forEach((entry) => {
          const link = document.createElement("a");
          link.href = siteURL(entry[3]);
          const kind = document.createElement("i");
          kind.textContent = entry[1];
          const text = document.createElement("span");
          const name = document.createElement("strong");
          name.textContent = entry[0];
          const meta = document.createElement("small");
          meta.textContent = entry[2];
          text.append(name, meta);
          const arrow = document.createElement("b");
          arrow.textContent = "→";
          link.append(kind, text, arrow);
          fragment.append(link);
        });
        if (!matches.length) {
          const empty = document.createElement("p");
          empty.textContent = "No archive records match this query.";
          fragment.append(empty);
        }
        result.replaceChildren(fragment);
      } catch (error) {
        result.textContent = "Global search index unavailable.";
      }
    };

    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(search, 60);
    });
    input.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        input.value = "";
        close();
        input.blur();
      }
    });
    document.addEventListener("pointerdown", (event) => {
      if (!event.target.closest(".global-search")) close();
    });
  };

  const initializeEffectCopy = () => {
    const list = document.querySelector(".effect-index");
    if (!list) return;
    list.addEventListener("click", async (event) => {
      const button = event.target.closest("[data-copy-effect]");
      if (!button) return;
      const label = button.querySelector("span");
      try {
        await navigator.clipboard.writeText(`/effect ${button.dataset.copyEffect}`);
        button.classList.add("is-copied");
        label.textContent = "COPIED";
      } catch (error) {
        label.textContent = "FAILED";
      }
      setTimeout(() => {
        button.classList.remove("is-copied");
        label.textContent = "COPY";
      }, 1400);
    });
  };

  const itemRecord = (entry) => ({
    id: entry[0],
    name: entry[1],
    category: entry[2],
    classes: entry[3] || [],
    elements: entry[4] || [],
    asset: entry[5] || "",
    icon: entry[6] || "",
    slug: entry[7],
  });

  const initializeItem = async () => {
    const input = document.querySelector("[data-item-search]");
    const results = document.querySelector("[data-item-results]");
    const template = document.querySelector("#item-row-template");
    if (!input || !results || !template) return;
    const counter = document.querySelector("[data-item-count]");
    const limit = document.querySelector("[data-item-limit]");
    const filters = document.querySelector("[data-item-filter]");
    let records = [];
    let category = "all";
    let timer;

    const render = () => {
      const query = normalize(input.value);
      const matches = records.filter((item) => {
        if (category !== "all" && normalize(item.category) !== category) return false;
        return !query || normalize([item.name, item.category, item.classes.join(" "), item.elements.join(" "), item.asset, item.id].join(" ")).includes(query);
      });
      const fragment = document.createDocumentFragment();
      matches.slice(0, 200).forEach((item) => {
        const row = template.content.firstElementChild.cloneNode(true);
        row.href = siteURL(`/item/${item.slug}/`);
        setField(row, "name", item.name);
        setField(row, "category", item.category);
        setField(row, "affinity", [...item.classes, ...item.elements].join(" / "));
        setField(row, "id", item.id);
        fragment.append(row);
      });
      if (!matches.length) {
        const empty = document.createElement("p");
        empty.className = "item-loading";
        empty.textContent = "No decoded item records match these controls.";
        fragment.append(empty);
      }
      results.replaceChildren(fragment);
      counter.textContent = matches.length.toLocaleString();
      limit.hidden = matches.length <= 200;
    };

    input.addEventListener("input", () => {
      clearTimeout(timer);
      timer = setTimeout(render, 60);
    });
    filters.addEventListener("click", (event) => {
      const button = event.target.closest("button[data-category]");
      if (!button) return;
      filters.querySelectorAll("button").forEach((candidate) => candidate.classList.toggle("is-active", candidate === button));
      category = button.dataset.category;
      render();
    });
    try {
      records = (await loadJSON(input.dataset.itemSource)).map(itemRecord);
      render();
    } catch (error) {
      results.textContent = "Item lookup cache unavailable.";
      counter.textContent = "0";
    }
  };

  const initializeItemConfigurator = () => {
    const root = document.querySelector("[data-item-configurator]");
    if (!root) return;
    const primary = root.querySelector("[data-config-primary]");
    const secondary = root.querySelector("[data-config-secondary]");
    const rig = root.querySelector("[data-config-rig]");
    const suffix = root.querySelector("[data-config-suffix]");
    const affixDataNode = root.querySelector("[data-item-affix-data]");
    const affixData = affixDataNode ? JSON.parse(affixDataNode.textContent) : { prefix: {}, suffix: {} };
    const icon = root.querySelector("[data-config-icon]");
    const title = root.querySelector("[data-config-title]");
    const code = root.querySelector("[data-config-code]");
    const command = root.querySelector("[data-config-command]");
    const copy = root.querySelector("[data-config-copy]");
    const copyStatus = root.querySelector("[data-config-copy-status]");
    const summary = {
      icon: root.querySelector("[data-item-summary-icon]"),
      title: root.querySelector("[data-item-summary-title]"),
      rig: root.querySelector("[data-item-summary-rig]"),
      style: root.querySelector("[data-item-summary-style]"),
      element: root.querySelector("[data-item-summary-element]"),
      primary: root.querySelector("[data-item-summary-primary]"),
      secondary: root.querySelector("[data-item-summary-secondary]"),
      suffix: root.querySelector("[data-item-summary-suffix]"),
      stats: root.querySelector("[data-item-summary-stats]"),
    };
    const controls = [primary, secondary, rig, suffix];

    const controlValue = (control) => control.matches("select") ? control.value : control.dataset.value;
    const controlOption = (control) => control.matches("select") ? control.selectedOptions[0] : control;
    const selectValue = (control, value) => {
      if (control.matches("select") && value && control.querySelector(`option[value="${CSS.escape(value)}"]`)) control.value = value;
    };
    const cleanLabel = (control) => {
      if (controlValue(control) === "0") return "";
      return (controlOption(control).dataset.label || controlOption(control).textContent).replace(/\s+\[\d+\]$/, "");
    };
    const scaledStatDivisors = new Map([
      [0, 10], [1, 10], [2, 10], [4, 1], [5, 3], [7, 0], [9, 0], [10, 1],
      [102, 20], [103, 0], [104, 8], [105, 8], [108, 20],
    ]);
    const nativeStats = {
      defense: { index: 4, name: "Max Health", authored_value: 75 },
      offense: { index: 10, name: "Critical Rating", authored_value: 22 },
      utility: { index: 5, name: "Power", authored_value: 25 },
    };
    const compactNumber = (value) => Number.isInteger(value) ? String(value) : String(Math.round(value * 100) / 100);
    const resolveStats = () => {
      // Recovered from corrupted.c sub_9CD3D0/sub_9C9D50. Basic items weight
      // suffix and rigblock vectors at 100%, both prefix scaled vectors at 0%.
      const suffixStats = affixData.suffix?.[controlValue(suffix)] || [];
      const primaryStats = affixData.prefix?.[controlValue(primary)] || [];
      const secondaryStats = affixData.prefix?.[controlValue(secondary)] || [];
      const native = nativeStats[(root.dataset.itemCategory || "").toLowerCase()];
      const baseStats = native ? [native] : [];
      const vectors = [suffixStats, primaryStats, secondaryStats, baseStats];
      const uniqueIndices = new Set(vectors.flatMap((stats) => stats
        .filter((stat) => stat.authored_value > 0)
        .map((stat) => stat.index)));
      const clientScale = 1.05 * ((uniqueIndices.size * 0.2 + 1) * 50);
      const resolved = new Map();
      const add = (stat, value, evidence) => {
        if (!value) return;
        const current = resolved.get(stat.index);
        if (current) current.value += value;
        else resolved.set(stat.index, { index: stat.index, name: stat.name, value, evidence });
      };
      vectors.forEach((stats) => stats.forEach((stat) => {
        if (!scaledStatDivisors.has(stat.index)) add(stat, stat.authored_value, "EXACT PACKAGE VALUE");
      }));
      if (native) {
        const weightedVectors = [suffixStats, baseStats];
        scaledStatDivisors.forEach((divisor, index) => {
          const authored = weightedVectors.reduce((sum, stats) => sum
            + (stats.find((stat) => stat.index === index)?.authored_value || 0), 0);
          const value = Math.trunc((authored * 0.01 * clientScale) / (divisor > 0 ? divisor : 1));
          const exemplar = vectors.flatMap((stats) => stats).find((stat) => stat.index === index);
          if (exemplar) add(exemplar, value, "CLIENT FORMULA · LEVEL 1 BASIC");
        });
      }
      return [...resolved.values()].sort((left, right) => left.index - right.index);
    };
    const statLabel = (stat) => {
      if (scaledStatDivisors.has(stat.index)) return `+${compactNumber(stat.value)} ${stat.name}`;
      if (stat.index >= 43 && stat.index <= 47) return `-${compactNumber(stat.value)}% ${stat.name}`;
      return `+${compactNumber(stat.value)}% ${stat.name}`;
    };
    const renderStats = () => {
      const selected = resolveStats();
      summary.stats.replaceChildren();
      selected.forEach((stat) => {
        const row = document.createElement("div");
        const value = document.createElement("strong");
        const evidence = document.createElement("small");
        const sourceLabel = document.createElement("span");
        sourceLabel.textContent = "ITEM";
        value.textContent = statLabel(stat);
        evidence.textContent = stat.evidence;
        row.append(sourceLabel, value, evidence);
        summary.stats.append(row);
      });
      if (!summary.stats.childElementCount) {
        const empty = document.createElement("p");
        empty.textContent = "Select one or more modifiers to inspect their decoded stats.";
        summary.stats.append(empty);
      }
    };
    const applyHash = () => {
      const state = new URLSearchParams(location.hash.slice(1));
      selectValue(rig, state.get("rig"));
      selectValue(primary, state.get("primary"));
      selectValue(secondary, state.get("secondary"));
      selectValue(suffix, state.get("suffix"));
    };
    const render = (writeHash = false) => {
      const selectedRig = controlOption(rig);
      const names = [cleanLabel(primary), cleanLabel(secondary), selectedRig.dataset.variant || root.dataset.family, cleanLabel(suffix)].filter(Boolean);
      title.textContent = names.join(" ");
      icon.src = selectedRig.dataset.icon;
      icon.alt = `${selectedRig.dataset.variant || root.dataset.family} item icon`;
      summary.icon.src = selectedRig.dataset.icon;
      summary.icon.alt = icon.alt;
      summary.title.textContent = names.join(" ");
      summary.rig.textContent = `RIGBLOCK ${controlValue(rig)}`;
      summary.style.textContent = selectedRig.dataset.variant || root.dataset.family;
      summary.element.textContent = selectedRig.dataset.element || "UNKNOWN ELEMENT";
      summary.primary.textContent = cleanLabel(primary) || "NONE SELECTED";
      summary.secondary.textContent = cleanLabel(secondary) || "NONE SELECTED";
      summary.suffix.textContent = cleanLabel(suffix) || "NONE SELECTED";
      [summary.primary, summary.secondary, summary.suffix].forEach((field) => field.closest("li").classList.toggle("is-empty", field.textContent === "NONE SELECTED"));
      renderStats();
      const state = new URLSearchParams();
      state.set("rig", controlValue(rig));
      if (controlValue(primary) !== "0") state.set("primary", controlValue(primary));
      if (controlValue(secondary) !== "0") state.set("secondary", controlValue(secondary));
      if (controlValue(suffix) !== "0") state.set("suffix", controlValue(suffix));
      code.textContent = `RIG ${controlValue(rig)} // P1 ${controlValue(primary)} // P2 ${controlValue(secondary)} // SUFFIX ${controlValue(suffix)}`;
      command.textContent = `/summon ${controlValue(rig) || "0"} ${controlValue(primary) || "0"} ${controlValue(secondary) || "0"} ${controlValue(suffix) || "0"}`;
      if (writeHash) history.replaceState(null, "", `${location.pathname}${location.search}#${state}`);
    };

    applyHash();
    render(!location.hash);
    controls.filter((control) => control.matches("select")).forEach((control) => control.addEventListener("change", () => render(true)));
    window.addEventListener("hashchange", () => {
      applyHash();
      render(false);
    });
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(command.textContent);
        copyStatus.textContent = "COPIED";
        copy.classList.add("is-copied");
      } catch (error) {
        copyStatus.textContent = "COPY FAILED";
      }
      setTimeout(() => {
        copyStatus.textContent = "";
        copy.classList.remove("is-copied");
      }, 1600);
    });
  };

  const initializeHeroConfigurator = () => {
    const root = document.querySelector("[data-hero-configurator]");
    const dataNode = document.querySelector("[data-hero-profile-data]");
    const abilityDataNode = document.querySelector("[data-hero-ability-data]");
    if (!root || !dataNode) return;
    const variants = JSON.parse(dataNode.textContent);
    const abilityCatalog = abilityDataNode ? JSON.parse(abilityDataNode.textContent) : {};
    const baseline = variants.find((entry) => normalize(entry.variant) === root.dataset.baseline) || variants[0];
    const buttons = [...root.querySelectorAll("[data-hero-variant]")];
    const statKeys = ["health", "power", "strength", "dexterity", "mind", "critical_rating", "resist_rating", "dodge_rating"];
    const fields = {
      metric: document.querySelector("[data-hero-active-metric]"),
      variant: root.querySelector("[data-hero-active-variant]"),
      name: root.querySelector("[data-hero-active-name]"),
      id: root.querySelector("[data-hero-active-id]"),
      min: root.querySelector("[data-hero-min]"),
      max: root.querySelector("[data-hero-max]"),
      abilityCount: root.querySelector("[data-hero-ability-count]"),
      unlock: root.querySelector("[data-hero-unlock]"),
      unlockDelta: root.querySelector("[data-hero-unlock-delta]"),
      variantAbility: root.querySelector("[data-hero-variant-ability]"),
      minDelta: root.querySelector("[data-hero-min-delta]"),
      maxDelta: root.querySelector("[data-hero-max-delta]"),
      abilityDelta: root.querySelector("[data-hero-ability-delta]"),
      hand: root.querySelector("[data-hero-hand]"),
      foot: root.querySelector("[data-hero-foot]"),
      locale: root.querySelector("[data-hero-locale]"),
      abilities: root.querySelector("[data-hero-ability]"),
      fragment: root.querySelector("[data-hero-fragment]"),
      stats: Object.fromEntries(statKeys.map((key) => [key, root.querySelector(`[data-hero-stat="${key}"]`)])),
      statDeltas: Object.fromEntries(statKeys.map((key) => [key, root.querySelector(`[data-hero-stat-delta="${key}"]`)])),
    };

    const hashVariant = () => {
      const fragment = location.hash.slice(1);
      if (!fragment) return root.dataset.baseline;
      if (!fragment.includes("=")) return normalize(fragment);
      return normalize(new URLSearchParams(fragment).get("variant"));
    };
    const delta = (value, baselineValue, isBaseline) => {
      if (isBaseline) return "BASELINE";
      const difference = Math.round(value - baselineValue);
      return `${difference > 0 ? "+" : difference < 0 ? "−" : "±"}${Math.abs(difference)} VS ${baseline.variant.toUpperCase()}`;
    };
    const render = (requested, writeHash = false) => {
      const active = variants.find((entry) => normalize(entry.variant) === requested) || baseline;
      const isBaseline = active === baseline;
      fields.metric.textContent = active.variant;
      fields.variant.textContent = active.variant.toUpperCase();
      fields.name.textContent = active.name;
      fields.id.textContent = `ID ${active.id}`;
      fields.min.textContent = Math.round(active.weapon_min_damage).toLocaleString();
      fields.max.textContent = Math.round(active.weapon_max_damage).toLocaleString();
      fields.abilityCount.textContent = active.abilities.length;
      fields.unlock.textContent = active.unlock_level;
      fields.unlockDelta.textContent = delta(active.unlock_level, baseline.unlock_level, isBaseline);
      const variantAbility = active.abilities.find((ability) => ability.slot === "random");
      fields.variantAbility.textContent = variantAbility ? (abilityCatalog[variantAbility.slug]?.name || variantAbility.name) : "UNRESOLVED";
      if (variantAbility) fields.variantAbility.href = siteURL(`/ability/${variantAbility.slug}/`);
      else fields.variantAbility.removeAttribute("href");
      fields.minDelta.textContent = delta(active.weapon_min_damage, baseline.weapon_min_damage, isBaseline);
      fields.maxDelta.textContent = delta(active.weapon_max_damage, baseline.weapon_max_damage, isBaseline);
      fields.abilityDelta.textContent = delta(active.abilities.length, baseline.abilities.length, isBaseline);
      statKeys.forEach((key) => {
        fields.stats[key].textContent = Math.round(active.stats[key]).toLocaleString();
        fields.statDeltas[key].textContent = delta(active.stats[key], baseline.stats[key], isBaseline);
      });
      fields.hand.textContent = active.is_hand_present ? "AVAILABLE" : "LOCKED";
      fields.foot.textContent = active.is_foot_present ? "AVAILABLE" : "LOCKED";
      fields.locale.textContent = active.name_locale_key;
      fields.fragment.textContent = `#variant=${normalize(active.variant)}`;
      fields.abilities.replaceChildren(...active.abilities.map((ability) => {
        const record = abilityCatalog[ability.slug] || ability;
        const row = document.createElement("li");
        const slot = document.createElement("b");
        slot.textContent = ability.slot.replaceAll("_", " ").toUpperCase();
        const asset = document.createElement("a");
        asset.href = siteURL(`/ability/${ability.slug}/`);
        if (record.icon_path) {
          const icon = document.createElement("img");
          icon.src = siteURL(record.icon_path);
          icon.alt = "";
          icon.width = 40;
          icon.height = 40;
          icon.loading = "lazy";
          asset.append(icon);
        }
        const info = document.createElement("span");
        const name = document.createElement("strong");
        name.textContent = record.name || ability.name;
        info.append(name);
        if (record.description) {
          const description = document.createElement("small");
          description.textContent = record.description;
          info.append(description);
        }
        asset.append(info);
        row.append(slot, asset);
        return row;
      }));
      buttons.forEach((button) => button.classList.toggle("is-active", button.dataset.heroVariant === normalize(active.variant)));
      if (writeHash) history.replaceState(null, "", `${location.pathname}${location.search}#variant=${normalize(active.variant)}`);
    };

    buttons.forEach((button) => button.addEventListener("click", () => render(button.dataset.heroVariant, true)));
    window.addEventListener("hashchange", () => render(hashVariant(), false));
    render(hashVariant(), !location.hash);
  };

  initializeFilters();
  initializeNPC();
  initializeLevel();
  initializeItem();
  initializeItemConfigurator();
  initializeHeroConfigurator();
  initializeGlobalSearch();
  initializeEffectCopy();
})();
