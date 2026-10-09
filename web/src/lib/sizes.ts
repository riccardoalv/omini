/** Size of each kind of node on the map (and of a group bubble), in map pixels. */
export const NODE_SIZES: Record<string, { width: number; height: number }> = {
  device: { width: 220, height: 60 },
  unmanaged: { width: 200, height: 56 },
  segment: { width: 240, height: 56 },
  client: { width: 200, height: 38 },
  app: { width: 180, height: 34 },
  wan: { width: 180, height: 56 },
  ssid: { width: 190, height: 26 },
  group: { width: 150, height: 44 },
}
