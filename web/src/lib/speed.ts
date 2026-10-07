/**
 * Link speed tiers, shared by the port panel, the legend and the map links.
 * Colors follow what homelab users know from switch UIs: faster is cooler.
 */
export type SpeedTier = '10g' | '5g' | '2.5g' | '1g' | '100m' | 'unknown'

export const SPEED_TIERS: SpeedTier[] = ['10g', '5g', '2.5g', '1g', '100m', 'unknown']

export function speedTier(mbps?: number): SpeedTier {
  if (!mbps) return 'unknown'
  if (mbps >= 10_000) return '10g'
  if (mbps >= 5000) return '5g'
  if (mbps >= 2500) return '2.5g'
  if (mbps >= 1000) return '1g'
  return '100m'
}

/** CSS color variable of a tier (defined in main.css). */
export const speedColor = (mbps?: number) => `var(--speed-${speedTier(mbps).replace('.', '_')})`
