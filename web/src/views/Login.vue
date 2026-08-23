<template>
  <div class="login-page" :style="{ background: bgStyle }">
    <div class="login-card">
      <div class="login-header">
        <img v-if="loginLogo" :src="loginLogo" class="login-logo" />
        <h1>{{ loginTitle }}</h1>
        <p>设备管理与控制平台</p>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" size="large" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="form.username" placeholder="请输入用户名" prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="form.password" type="password" placeholder="请输入密码" prefix-icon="Lock" show-password @keyup.enter="handleLogin" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="loading" native-type="submit" style="width: 100%">
            {{ loading ? '登录中...' : '登 录' }}
          </el-button>
        </el-form-item>
      </el-form>
    </div>
    <div class="login-footer">
      <span>{{ copyright }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getConfig, getLoginBackground } from '@/utils/config'
import { ElMessage } from 'element-plus'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)

const config = getConfig()
const loginTitle = config.login?.title || config.site?.name || '通用云控系统'
const loginLogo = config.login?.logo || ''
const copyright = config.site?.copyright || '© 2026 通用云控系统'
const bgStyle = getLoginBackground()

const form = reactive({
  username: '',
  password: '',
})

const rules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

async function handleLogin() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  const result = await authStore.login(form)
  loading.value = false

  if (result.success) {
    ElMessage.success('登录成功')
    const redirect = route.query.redirect || '/'
    router.push(redirect)
  } else {
    ElMessage.error(result.message || '登录失败')
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #0A0E27 0%, #1A1F4B 50%, #0F1535 100%);
}
.login-card {
  width: 400px; max-width: 90vw;
  padding: 44px 36px;
  background: rgba(255,255,255,.97);
  backdrop-filter: blur(20px);
  border-radius: 1rem;
  box-shadow: 0 8px 40px rgba(0,0,0,.25), 0 1px 3px rgba(0,0,0,.1);
  animation: fadeInUp 500ms ease-out;
}
.login-header { text-align: center; margin-bottom: 32px; }
.login-header .login-logo { width: 56px; height: 56px; border-radius: 14px; margin-bottom: 14px; }
.login-header h1 { font-size: 24px; font-weight: 700; margin: 0; letter-spacing: -.02em; }
.login-header p { color: #8E8E93; font-size: 13px; margin: 6px 0 0; }
.login-footer { margin-top: 24px; color: rgba(255,255,255,.45); font-size: 12px; text-align: center; }
@media (max-width: 480px) {
  .login-card { padding: 32px 24px; }
  .login-header h1 { font-size: 20px; }
}
</style>
