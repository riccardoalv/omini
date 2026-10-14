// Writes src/assets/world-110m.json: the world's countries (Natural Earth,
// public domain, via world-atlas) keyed by ISO 3166 alpha-2, as DB-IP names
// them. Run with `npm run world` after updating world-atlas.
import { readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const countries = require('i18n-iso-countries')
const topo = JSON.parse(readFileSync(require.resolve('world-atlas/countries-110m.json'), 'utf8'))
const geoms = topo.objects.countries.geometries
for (const g of geoms) {
  const alpha2 = countries.numericToAlpha2(g.id)
  g.id = alpha2 ?? g.id
  g.properties = {}
}
topo.objects.countries.geometries = geoms.filter((g) => g.id && g.id !== 'AQ') // Antarctica: no traffic, much room
writeFileSync(new URL('../src/assets/world-110m.json', import.meta.url), JSON.stringify(topo))
console.log(`${topo.objects.countries.geometries.length} countries`)
