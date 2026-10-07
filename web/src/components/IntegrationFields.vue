<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { fieldLabel, inputType } from '@/lib/forms'
import type { Config, FormField } from '@/lib/types'

import SecretInput from './SecretInput.vue'
import ToggleSwitch from './ToggleSwitch.vue'

const props = defineProps<{
  /** Integration type, used to look up translations of field labels. */
  type: string
  fields: FormField[]
  /** Editing an existing integration: saved secrets are kept when left blank. */
  editing?: boolean
  /** Prefix for input ids, so several forms can be on the page. */
  idPrefix?: string
}>()
const values = defineModel<Config>({ required: true })
const { t, te } = useI18n()

// Fields without a group come first, then each group in order of appearance.
const sections = computed(() => {
  const out: { name: string; fields: FormField[] }[] = [{ name: '', fields: [] }]
  for (const f of props.fields) {
    const name = f.group ?? ''
    let section = out.find((s) => s.name === name)
    if (!section) {
      section = { name, fields: [] }
      out.push(section)
    }
    section.fields.push(f)
  }
  return out.filter((s) => s.fields.length > 0)
})

const id = (f: FormField) => `${props.idPrefix ?? 'f'}-${f.key}`

/** Translated label/help when available (fields.<type>.<key>.label), else the integration's own text. */
function label(f: FormField): string {
  const key = `fields.${props.type}.${f.key}.label`
  return te(key) ? t(key) : fieldLabel(f)
}
function help(f: FormField): string | undefined {
  const key = `fields.${props.type}.${f.key}.help`
  return te(key) ? t(key) : f.help
}
function option(f: FormField, o: string): string {
  const key = `fields.${props.type}.${f.key}.options.${o}`
  return te(key) ? t(key) : o
}
function groupName(name: string): string {
  const key = `fieldGroups.${name}`
  return te(key) ? t(key) : name
}
</script>

<template>
  <div class="fields">
    <fieldset v-for="section in sections" :key="section.name" class="section">
      <legend v-if="section.name">{{ groupName(section.name) }}</legend>
      <div
        v-for="f in section.fields"
        :key="f.key"
        class="field"
        :class="{ 'bool-row': f.type === 'bool' }"
      >
        <template v-if="f.type === 'bool'">
          <div class="bool-field">
            <ToggleSwitch
              :model-value="!!values[f.key]"
              :label="label(f)"
              :data-field="f.key"
              @update:model-value="(v: boolean) => (values[f.key] = v)"
            />
            <span>{{ label(f) }}</span>
          </div>
          <span v-if="help(f)" class="help">{{ help(f) }}</span>
        </template>
        <template v-else>
          <label :for="id(f)">{{ label(f) }}<span v-if="f.required"> *</span></label>
          <select
            v-if="f.type === 'select'"
            :id="id(f)"
            v-model="values[f.key]"
            class="select"
            :name="f.key"
          >
            <option v-for="o in f.options" :key="o" :value="o">{{ option(f, o) }}</option>
          </select>
          <SecretInput
            v-else-if="f.type === 'secret'"
            :id="id(f)"
            v-model="values[f.key]"
            :name="f.key"
            :required="f.required && !editing"
            :placeholder="editing ? t('integrations.secretSaved') : undefined"
          />
          <input
            v-else
            :id="id(f)"
            v-model="values[f.key]"
            class="input"
            :name="f.key"
            :type="inputType(f)"
            :required="f.required"
            :placeholder="f.type === 'string' && f.default === undefined ? '' : undefined"
            autocomplete="off"
          />
          <span v-if="help(f)" class="help">{{ help(f) }}</span>
        </template>
      </div>
    </fieldset>
  </div>
</template>

<style scoped>
.section {
  margin: 0 0 6px;
  padding: 0;
  border: 0;
}
legend {
  padding: 0;
  margin: 8px 0 10px;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.bool-field {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 500;
}
.bool-row .help {
  padding-left: 48px;
}
</style>
