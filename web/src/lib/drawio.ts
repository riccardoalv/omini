/**
 * The map as a draw.io (diagrams.net) file: every node where it is drawn,
 * areas as containers and links with their port name and speed. Opens in
 * draw.io's desktop and web apps, editable.
 */

export interface DrawioNode {
  id: string
  label: string
  /** Second line: IP, model... */
  sub?: string
  kind: string
  x: number
  y: number
  width: number
  height: number
  online?: boolean
}

export interface DrawioEdge {
  id: string
  source: string
  target: string
  label?: string
  dashed?: boolean
}

export interface DrawioArea {
  id: string
  name: string
  color: string
  x: number
  y: number
  width: number
  height: number
}

const AREA_COLORS: Record<string, [string, string]> = {
  gray: ['#f5f5f5', '#666666'],
  blue: ['#dae8fc', '#6c8ebf'],
  green: ['#d5e8d4', '#82b366'],
  yellow: ['#fff2cc', '#d6b656'],
  red: ['#f8cecc', '#b85450'],
  purple: ['#e1d5e7', '#9673a6'],
}

const NODE_STYLE: Record<string, string> = {
  device: 'fillColor=#dae8fc;strokeColor=#6c8ebf;',
  wan: 'fillColor=#e1d5e7;strokeColor=#9673a6;',
  unmanaged: 'fillColor=#f5f5f5;strokeColor=#666666;dashed=1;',
  segment: 'fillColor=#f5f5f5;strokeColor=#666666;dashed=1;',
  client: 'fillColor=#ffffff;strokeColor=#999999;',
  app: 'fillColor=#d5e8d4;strokeColor=#82b366;',
  ssid: 'fillColor=#fff2cc;strokeColor=#d6b656;',
  group: 'fillColor=#ffffff;strokeColor=#999999;dashed=1;',
}

/** Escapes text for an XML attribute. */
export function xmlAttr(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/\n/g, '&#10;')
}

const r = (n: number) => Math.round(n)

export function mapDrawio(
  nodes: DrawioNode[],
  edges: DrawioEdge[],
  areas: DrawioArea[] = [],
  name = 'Omini',
): string {
  const cells: string[] = ['<mxCell id="0"/>', '<mxCell id="1" parent="0"/>']
  // Areas first: drawn behind the nodes.
  for (const a of areas) {
    const [fill, stroke] = AREA_COLORS[a.color] ?? AREA_COLORS.gray!
    const style =
      `rounded=1;arcSize=4;whiteSpace=wrap;html=1;dashed=1;fillColor=${fill};strokeColor=${stroke};` +
      'opacity=60;verticalAlign=top;align=left;spacingLeft=8;fontStyle=1;container=0;'
    cells.push(
      `<mxCell id="${xmlAttr('area-' + a.id)}" value="${xmlAttr(a.name)}" style="${style}" vertex="1" parent="1">` +
        `<mxGeometry x="${r(a.x)}" y="${r(a.y)}" width="${r(a.width)}" height="${r(a.height)}" as="geometry"/></mxCell>`,
    )
  }
  const ids = new Set<string>()
  for (const n of nodes) {
    ids.add(n.id)
    const style =
      'rounded=1;whiteSpace=wrap;html=1;arcSize=20;fontSize=11;' +
      (NODE_STYLE[n.kind] ?? NODE_STYLE.client) +
      (n.online === false ? 'opacity=50;' : '')
    // html=1 values are HTML: the label is escaped once for HTML, once for XML.
    const html = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    const value = n.sub ? `<b>${html(n.label)}</b><br>${html(n.sub)}` : `<b>${html(n.label)}</b>`
    cells.push(
      `<mxCell id="${xmlAttr(n.id)}" value="${xmlAttr(value)}" style="${style}" vertex="1" parent="1">` +
        `<mxGeometry x="${r(n.x)}" y="${r(n.y)}" width="${r(n.width)}" height="${r(n.height)}" as="geometry"/></mxCell>`,
    )
  }
  for (const e of edges) {
    if (!ids.has(e.source) || !ids.has(e.target)) continue
    const style =
      'edgeStyle=orthogonalEdgeStyle;rounded=1;html=1;endArrow=none;strokeColor=#888888;fontSize=10;' +
      'labelBackgroundColor=#ffffff;' +
      (e.dashed ? 'dashed=1;' : '')
    cells.push(
      `<mxCell id="${xmlAttr('e-' + e.id)}" value="${xmlAttr(e.label ?? '')}" style="${style}" edge="1" parent="1" ` +
        `source="${xmlAttr(e.source)}" target="${xmlAttr(e.target)}"><mxGeometry relative="1" as="geometry"/></mxCell>`,
    )
  }
  return (
    '<mxfile host="Omini">' +
    `<diagram id="omini" name="${xmlAttr(name)}">` +
    '<mxGraphModel grid="0" page="0" tooltips="1" connect="1" arrows="0" fold="1">' +
    `<root>${cells.join('')}</root>` +
    '</mxGraphModel></diagram></mxfile>'
  )
}
