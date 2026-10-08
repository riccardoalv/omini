import { describe, expect, it } from 'vitest'

import { mapDrawio, xmlAttr } from '@/lib/drawio'

describe('draw.io export', () => {
  it('writes nodes, areas and links as an mxfile', () => {
    const xml = mapDrawio(
      [
        {
          id: 'dev:fw',
          label: 'OPNsense',
          sub: '192.168.1.1',
          kind: 'device',
          x: 10.4,
          y: 20,
          width: 220,
          height: 60,
        },
        {
          id: 'mac:aa',
          label: 'TV <Sala> & co',
          kind: 'client',
          x: 300,
          y: 20,
          width: 200,
          height: 38,
          online: false,
        },
      ],
      [
        { id: 'e1', source: 'dev:fw', target: 'mac:aa', label: 'LAN | 1G', dashed: true },
        { id: 'e2', source: 'dev:fw', target: 'gone' }, // its end is not on the map
      ],
      [{ id: '1', name: 'Rack', color: 'blue', x: 0, y: 0, width: 600, height: 200 }],
    )
    const doc = new DOMParser().parseFromString(xml, 'application/xml')
    expect(doc.querySelector('parsererror')).toBeNull()
    const cells = [...doc.querySelectorAll('mxCell')]
    expect(cells.map((c) => c.getAttribute('id'))).toEqual([
      '0',
      '1',
      'area-1',
      'dev:fw',
      'mac:aa',
      'e-e1',
    ])
    const fw = cells[3]!
    expect(fw.getAttribute('value')).toBe('<b>OPNsense</b><br>192.168.1.1')
    expect(fw.querySelector('mxGeometry')!.getAttribute('x')).toBe('10')
    expect(cells[4]!.getAttribute('value')).toBe('<b>TV &lt;Sala&gt; &amp; co</b>')
    expect(cells[4]!.getAttribute('style')).toContain('opacity=50')
    const edge = cells[5]!
    expect(edge.getAttribute('source')).toBe('dev:fw')
    expect(edge.getAttribute('value')).toBe('LAN | 1G')
    expect(edge.getAttribute('style')).toContain('dashed=1')
    expect(cells[2]!.getAttribute('style')).toContain('fillColor=#dae8fc')
  })

  it('escapes attributes', () => {
    expect(xmlAttr('a"b<c>&\n')).toBe('a&quot;b&lt;c&gt;&amp;&#10;')
  })
})
