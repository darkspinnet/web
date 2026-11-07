# Darkspin Archive

A Hugo field archive generated from the decoded Darkspore content database and original client packages.

The site currently publishes:

- a 25-family hero landing page with one canonical portrait per family and interactive stat profiles on each detail page;
- a flat, searchable Enemy Glossary of spawnable creature and destructable nouns, using localized names, base HP, element filters, and lazy-loaded original portraits where recovered;
- 72 campaign missions in authored order across Onslaught, Invasion, and Apocalypse, with creature abilities, Destructor encounters, and seeded spawn minimaps. Auxiliary reports cover PvP, tutorial, arena, small-map, Spectra/TNX, and test levels where prepared navigation exists;
- an Item Archive containing decoded base equipment rigblocks, prefix/suffix applicability, and procedural medal, chain, and epic-chance rules;
- a Game Systems reference generated from the server runtime, covering combat and resistance caps, diminishing returns, enemy drop probabilities, capsule weighting, the full 100-level XP curve, hero milestones, and Cash Out rewards;
- an Ability Archive covering hero loadouts and enemy AI abilities, with ownership links, combat data where decoded, and a Heroes only filter;
- a Status Effect Archive sourced from client localization and correlated with recovered abilities;
- a general Glossary for combat, hero, equipment, and archive terminology;
- a searchable Buff catalog for localized gameplay conditions and a raw Effects index decoded from packaged VFX resources;
- a lazy-loaded global search index for hero families, enemies, levels, base items, abilities, buffs, effects, and glossary terms.

## Prerequisites

- Go 1.25+
- Hugo
- ImageMagick (`magick`) for WebP portrait conversion
- a Darkspore install and a Darkspin-generated `content.db`

The module uses a local replacement for `github.com/darkspinnet/darkspin` in `go.mod`. Change that path if the repositories are not checked out at `d:\src\darkspin` and `d:\src\web`.

## Mage targets

Build Hugo from the current generated inputs:

```powershell
mage build
```

Start Hugo's development server without regenerating anything:

```powershell
mage server
```

Refresh everything and then build Hugo:

```powershell
mage all
```

Individual generators are available as `mage hero`, `mage ability`, `mage buff`, `mage effect`, `mage glossary`, `mage npc`, `mage level`, `mage item`, `mage system`, `mage combat`, `mage asset`, and `mage search`. `mage generate` runs the data and asset generators without building Hugo.

## Combat and resistance report

`/system/combat/` documents hero avoidance and mitigation, NPC ordinary and temporary-reduction caps, multiplicative stacking, flat armor, and separate shield/immunity rules. It also covers party auras, companion defenses, multiplayer debuffs, and hit reactions. It includes a rating curve explorer, worked examples, recent combat commits, and links to the exact source revision.

Run `mage combat` after committing Darkspin tuning changes, then `mage build`. The combat generator resolves the sibling repository's `HEAD` once and reads committed files with `git show`; uncommitted combat work is excluded. It writes `data/generated/combat.json` independently of the loot and reward generators. `go run ./scripts/combat -server D:/src/darkspin` can also generate the report directly; follow it with `mage search` to refresh the global index.

Caps are extracted from source constants. Decision functions are compared with the reviewed revision in `scripts/combat/main.go`, with formatting and comments ignored. Cap-only changes refresh the generated numbers and curves. Changes to those functions stop generation before replacing the catalog; review the report text, formulas, calculator, and examples against the new implementation before advancing `reviewedCombatRevision`. Builds use the committed generated catalog, so Firebase CI does not need a sibling Darkspin checkout. Updating Darkspin alone does not republish the website.

## Navigation minimaps

The sibling Darkspin CLI lists both campaign shortcuts and every prepared miscellaneous level:

```powershell
d:\src\darkspin\bin\game\darkrun.exe map --config d:\src\darkspin\bin\game\darkspin.toml
```

Run `mage level` to generate the campaign minimaps and refresh the level catalog. The maps show the standing enemy population and any Destructor encounter for a solo first clear with Darkspin's default spawn rules. To preview a different run seed, use `go run ./scripts/level -campaign-seed 42`. Darkspin derives the first-clear population seed from the stage and uses the run seed for layout selection. Run `mage search` to refresh the global index after regenerating level data.

For auxiliary maps, render a selection into the directory for its website level slug, then refresh the level catalog. The selection may be the `tutorial` shortcut or a canonical PvP, arena, small-map, Spectra/TNX, or test level name.

```powershell
d:\src\darkspin\bin\game\darkrun.exe map cryos_2_PVP --config d:\src\darkspin\bin\game\darkspin.toml --output static\maps\cryos-2-pvp\map.webp --metadata static\maps\cryos-2-pvp\map.json
mage level
```

Build the current sibling CLI with `mage darkrun:build` in the Darkspin repository before regenerating auxiliary maps. Each CLI run emits one WebP and JSON pair per reachable navigation component. The level generator matches auxiliary maps to their pages using the level name recorded in `static/maps/*/map.json`.

The generators default to:

- database: `d:\src\darkspin\bin\game\darkspin\cache\content.db`
- client root: `d:\src\darkspin\bin\game`

Override either path when needed:

```powershell
go run ./scripts/all -db D:\Darkspin\content.db -game D:\Darkspore
```

Use `-skip-assets` for a fast data-only refresh.

## Individual generators

```powershell
go run ./scripts/hero
go run ./scripts/ability
go run ./scripts/buff
go run ./scripts/effect
go run ./scripts/glossary
go run ./scripts/npc
go run ./scripts/level
go run ./scripts/item
go run ./scripts/system
go run ./scripts/combat
go run ./scripts/assets
go run ./scripts/search
```

The data tools write deterministic, formatted JSON under `data/generated`. The asset tool keeps the final (Delta) portrait for each of the 25 hero families from `PreBaked.package` and recovers referenced equipment and ability icons from `UI.package`, then converts them to WebP under `static/image/hero`, `static/image/item`, and `static/image/ability`.

Hero detail pages pair the canonical portrait with the original localized epithet and biography from the client. Alpha is the baseline; Beta, Gamma, and Delta switch the package-backed profile fields while the image remains fixed. The selected profile is stored in the URL fragment (for example, `#variant=beta`), and each numeric field shows its delta from Alpha.

The ability tool deduplicates assets referenced by all 100 hero profiles and by the enemy catalog's AI definitions and phase gambits. It correlates packaged Lua namespaces with authored UI icons, links exact localized names and descriptions where their identity is proven, and projects rank-one combat fields from Lua registrations supported by the current decoder. Each asset has a page with hero or enemy ownership, variant or AI-use context, source provenance, and available combat data. Hero loadouts and enemy records link directly to these pages. The archive defaults to all abilities and offers a Heroes only filter, including abilities shared with enemies.

Curated enemy ability text lives in Darkspin's `notes/abilities.json`. The web generator reads the checked-in copy at `data/npc_ability_text.json`. After editing the Darkspin lookup, copy it over and run `mage ability` to refresh the creature and ability data, ability pages, and search. The lookup uses ability asset names as keys. Each entry can supply a full `label`, a `short_label` for creature lists, and a `description`. Text applies to abilities with recovered enemy ownership, including abilities shared with heroes. Routine attacks still use the Melee and Ranged labels on creature lists.

The buff tool resolves curated gameplay conditions from the English client localization and links matching recovered ability text. When a modifier's native `SPID` icon reference has been verified, the asset generator extracts its 32-pixel HUD artwork from `UI.package` into `static/image/buff`; unverified buffs retain the lightweight CSS glyph. The effect tool merges raw names from the packaged VFX resource type with identifiers recovered from compiled Lua `ServerEventDefs` preload tables, preserving references such as `character_beam_in_plasma_electric` while distinguishing referenced-only effects from concrete package records. The glossary is a compact, hand-maintained terminology layer that explains broader combat, hero, equipment, and archive concepts while linking back to the relevant generated sections.

The item tool decodes the RefPack-compressed rigblock, prefix, and suffix resources in `AssetData_Binary.package`, joins their localization records, emits compact browser lookup data, and creates item detail pages. `scripts/itemprobe` is a read-only format inspection utility for continued loot research. The recovered `nLootRules` bytecode contains global procedural medal, chain, and epic-chance behavior; no authored enemy-to-item ownership table has been identified yet.

The system tool reads named operands and progression arrays from the sibling Darkspin source tree, evaluates the server's public drop and reward decisions, and writes `data/generated/system.json`. It fails closed if the expected source shape changes, preventing stale tables from silently publishing after runtime tuning is refactored.

Item search indexes one localized equipment family (for example, Safeguard) rather than every package rigblock. Detail pages group case-insensitive duplicate assets, visual styles, elemental records, and the compatible primary-prefix, secondary-prefix, and suffix pool without generating duplicate searchable item combinations.

Each item family page includes a client-side configurator. It stores the selected rigblock, primary prefix, secondary prefix, and suffix IDs in the URL fragment, so configurations are shareable without generating additional Hugo pages or involving a server.

## Production build

```powershell
mage build
```

The generated static site is written to `public/`.


Navigation, catalog search results, images, and client-side archive links respect
the configured base path. The production `baseURL` in `config.toml` is unchanged.

Enemy portraits are extracted by `mage npc` (or `go run ./scripts/npc -db <content.db> -game <client-root>`). The generator joins actual non-player nouns to their classes and authored noun types; placement-only helpers and player pets are excluded. Each noun's assembled-creature asset ID (the little-endian uint64 at offset 152) is matched to the big-endian asset ID in version-13 metadata in `PreBaked.package`. The metadata's resource key identifies the corresponding PNG. Animation names and noun suffixes do not determine appearance: rigs can be shared, and variants carry their own creature IDs. Missing artwork stays visibly unavailable. The enemy index is flat and independent of authored level references, so it also supports randomized spawns.

Campaign pages preserve existing Onslaught URLs and add `invasion-` and `apocalypse-` mission URLs. Each pass uses the corresponding difficulty tuning and first-clear spawn configuration, including its own noun variants. Map position candidates use that same configuration. Live randomizer, party scaling, buffs, and encounter rolls can change the result.

Replay groups the 24 randomized destinations by planet. Each `replay-` map page lists the enemies eligible under the default replay rules across all threat bands, then shows its authored encounter anchors. The population is a pool of candidates across runs.

Item pages show supported hero families and variants for the selected visual/element. Compatibility is computed per rigblock from its class and element masks, explicit hero noun restrictions, and the hero’s hand/foot slots. The hero list updates with the configurator and restores from its shared URL fragment. `mage item` reads the committed hero catalog (`-heroes` can override it) and original client resources under `-game`; refresh heroes first when those profiles change.

The ability generator also walks every cataloged enemy’s authored AI definition and phase gambits, preserving action, passive, death, alert, and aggro uses. Shared hero/enemy abilities retain both ownership lists and remain visible under Heroes only. Enemy pages link back to their recovered abilities. Unavailable phase references are recorded in `ownership_notes` without discarding known callbacks. `mage ability` refreshes the enemy catalog first; full generation uses the same dependency order.

Enemy list and detail previews share a continuous mission selector from 1-1 through 18-4 and Solo/co-op (2–4 players) controls. Invasion begins at 7-1 and Apocalypse at 13-1. `mage npc` also writes all 72 authored health/damage tuning rows to `static/data/npc/scaling.json` and exports each class's player-count coefficient. HP follows the runtime formula `base HP × (1 + (players − 1) × class coefficient) × mission health multiplier`; default destructable fixture HP remains unscaled. Damage/healing party multipliers come from `sim.ProjectNonPlayerDamageForParty` (1, 1.4, 1.8, 2.2). Ability previews use recovered runtime action profiles, the noun's effective NPC rank, and source attributes; missing numerical values stay unavailable. Rank remains attached to the selected noun variant rather than changing its identity with the selected mission. Elite affixes, temporary buffs, Star Mode, and target defenses are excluded. The selected context persists in the URL and browser preferences and follows enemy links.

Enemy detail pages show the union of the selected creature family's authored ability sets, including later variants in an Onslaught preview. Invasion and Apocalypse badges mark the earliest variant that references an ability; shared abilities remain unbadged. Variant families require an existing rank-1 base noun with a matching category, so unrelated suffixes are not grouped. The ability archive also badges enemy abilities whose known users are all later-rank variants; abilities shared with heroes are not marked as difficulty-specific.
