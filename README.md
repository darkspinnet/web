# Darkspin Archive

A Hugo field archive generated from the decoded Darkspore content database and original client packages.

The site currently publishes:

- a 25-family hero landing page with one canonical portrait per family and interactive stat profiles on each detail page;
- an Enemy Glossary limited to verified killable combat classes, plus generated enemy detail pages;
- seven level groups—Level 1 through Level 6 and Other—with dedicated reports and connected-component minimaps for campaign, PvP, tutorial, arena, small-map, Spectra/TNX, and test levels where prepared navigation exists;
- an Item Archive containing decoded base equipment rigblocks, prefix/suffix applicability, and procedural medal, chain, and epic-chance rules;
- a Game Systems reference generated from the server runtime, covering combat and resistance caps, diminishing returns, enemy drop probabilities, capsule weighting, the full 100-level XP curve, hero milestones, and Cash Out rewards;
- an Ability Archive grouping recovered loadout assets by hero, variant, and slot;
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
d:\src\darkspin\bin\server\darkrun.exe map --config d:\src\darkspin\bin\darkspinner\darkspin.toml
```

Render a selection into the directory for its website level slug, then refresh the level catalog. The selection may be a campaign slot such as `1-1`, the `tutorial` shortcut, or a canonical PvP, arena, small-map, Spectra/TNX, or test level name.

```powershell
d:\src\darkspin\bin\server\darkrun.exe map cryos_2_PVP --config d:\src\darkspin\bin\darkspinner\darkspin.toml --output static\maps\cryos-2-pvp\map.webp --metadata static\maps\cryos-2-pvp\map.json
mage level
```

Build the current sibling CLI with `mage darkrun:build` in the Darkspin repository before regenerating maps. Each run emits one WebP and JSON pair per reachable navigation component. The level generator discovers every `static/maps/*/map.json` file by its recorded level name, publishes the component images on the matching level page, and imports the command's current marker-label explanations.

The generators default to:

- database: `d:\src\darkspin\bin\darkspinner\darkspin\cache\content.db`
- client root: `d:\src\darkspin\bin\darkspinner`

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

The ability tool deduplicates the ability assets referenced by all 100 hero profiles. It correlates packaged Lua namespaces with the authored 40-pixel UI icons, links exact localized names and descriptions where their identity is proven, and projects rank-one combat fields from the Lua registrations supported by the current decoder. It generates one page per asset with loadout slot, hero ownership, variant coverage, source provenance, and available combat data. Hero loadouts show the same icons and brief descriptions and link directly to these pages.

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
