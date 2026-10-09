<script setup lang="ts">
/** A temporary dashed frame around devices the VLAN/subnet filter highlights. */
defineProps<{ name: string }>()
</script>

<template>
  <div class="net-frame" data-test="net-frame">
    <span>{{ name }}</span>
  </div>
</template>

<style scoped>
.net-frame {
  width: 100%;
  height: 100%;
  border: 2px dashed var(--accent);
  border-radius: var(--radius);
  background: color-mix(in srgb, var(--accent) 6%, transparent);
  pointer-events: none;
}
.net-frame span {
  position: absolute;
  bottom: calc(100% + 3px);
  left: 0;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent) 22%, var(--bg-elevated));
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}
</style>
