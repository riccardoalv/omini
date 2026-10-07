<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { session, signedIn } from '@/lib/session'

import Logo from './LogoMark.vue'

const { t } = useI18n()
const username = ref('admin')
const password = ref('')
const confirm = ref('')
const error = ref('')
const busy = ref(false)

const setup = computed(() => session.setupRequired)

async function submit() {
  error.value = ''
  if (setup.value && password.value !== confirm.value) {
    error.value = t('auth.passwordsDiffer')
    return
  }
  busy.value = true
  try {
    const res = setup.value
      ? await api.setup(username.value, password.value)
      : await api.login(username.value, password.value)
    signedIn(res.username ?? username.value)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth">
    <form class="card auth-card" @submit.prevent="submit">
      <Logo class="logo" />
      <h1>{{ t('auth.welcome') }}</h1>
      <p class="muted">{{ setup ? t('auth.setupHint') : t('auth.loginHint') }}</p>

      <div class="field">
        <label for="username">{{ t('auth.username') }}</label>
        <input id="username" v-model="username" class="input" autocomplete="username" required />
      </div>
      <div class="field">
        <label for="password">{{ t('auth.password') }}</label>
        <input
          id="password"
          v-model="password"
          class="input"
          type="password"
          :autocomplete="setup ? 'new-password' : 'current-password'"
          :minlength="setup ? 8 : undefined"
          required
        />
        <span v-if="setup" class="help">{{ t('auth.passwordHint') }}</span>
      </div>
      <div v-if="setup" class="field">
        <label for="confirm">{{ t('auth.confirmPassword') }}</label>
        <input
          id="confirm"
          v-model="confirm"
          class="input"
          type="password"
          autocomplete="new-password"
          required
        />
      </div>

      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <button class="btn primary submit" type="submit" :disabled="busy">
        {{ setup ? t('auth.createAccount') : t('auth.signIn') }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.auth {
  min-height: 100%;
  display: grid;
  place-items: center;
  padding: 24px 16px;
  background: radial-gradient(circle at 20% 10%, var(--accent-soft), transparent 40%), var(--bg);
}
.auth-card {
  width: 100%;
  max-width: 380px;
  padding: 32px 28px;
  box-shadow: var(--shadow);
}
.logo {
  width: 40px;
  height: 40px;
  margin-bottom: 16px;
}
h1 {
  font-size: 22px;
}
p.muted {
  margin: 6px 0 22px;
}
.submit {
  width: 100%;
  justify-content: center;
  height: 38px;
  margin-top: 6px;
}
.alert {
  margin: 0 0 12px;
}
</style>
