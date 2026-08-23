import { defineStore } from 'pinia'
import { loginApi, logoutApi, getUserInfoApi, refreshTokenApi } from '@/api/endpoints'
import { ElMessage } from 'element-plus'
import router from '@/router'

let authErrorFlag = false

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('token') || '',
    refreshToken: localStorage.getItem('refreshToken') || '',
    user: null,
    isAuthenticated: !!localStorage.getItem('token'),
    tokenCheckTimer: null,
    tokenCheckInterval: 30000,
    dataManagerEnabled: localStorage.getItem('dataManagerEnabled') !== 'false',
  }),

  getters: {
    isAdmin(state) {
      if (!state.user?.roles?.length) return false
      const codes = state.user.roles.map(r => r.code)
      return codes.includes('system_admin') || codes.includes('admin')
    },
    isSystemAdmin(state) {
      return state.user?.roles?.some(r => r.code === 'system_admin') || false
    },
    userRole(state) {
      if (!state.user?.roles?.length) return null
      const codes = state.user.roles.map(r => r.code)
      return codes.includes('system_admin') || codes.includes('admin') ? 'admin' : 'user'
    },
  },

  actions: {
    async login(credentials) {
      try {
        const res = await loginApi(credentials)
        if (res.code === 200) {
          this.token = res.data.token
          this.refreshToken = res.data.refresh_token
          this.user = res.data.user
          this.isAuthenticated = true
          this.dataManagerEnabled = res.data.data_manager_enabled !== false
          localStorage.setItem('token', this.token)
          localStorage.setItem('refreshToken', this.refreshToken)
          localStorage.setItem('dataManagerEnabled', this.dataManagerEnabled)
          this.startTokenCheck()
          return { success: true }
        }
        return { success: false, message: res.message || '登录失败' }
      } catch (e) {
        return { success: false, message: e.message || '登录失败' }
      }
    },

    async logout(silent = false) {
      this.stopTokenCheck()
      if (!silent) {
        try { await logoutApi() } catch {}
      }
      this.token = ''
      this.refreshToken = ''
      this.user = null
      this.isAuthenticated = false
      this.dataManagerEnabled = true
      localStorage.removeItem('token')
      localStorage.removeItem('refreshToken')
      localStorage.removeItem('dataManagerEnabled')
    },

    async fetchUserInfo() {
      try {
        const res = await getUserInfoApi()
        if (res.code === 200) {
          this.user = res.data
          this.isAuthenticated = true
          return true
        }
        throw new Error(res.message || '获取用户信息失败')
      } catch {
        this.token = ''
        this.refreshToken = ''
        this.isAuthenticated = false
        this.user = null
        localStorage.removeItem('token')
        localStorage.removeItem('refreshToken')
        throw new Error('获取用户信息失败')
      }
    },

    async refreshTokenAction() {
      try {
        if (!this.refreshToken) throw new Error('没有刷新令牌')
        const res = await refreshTokenApi(this.refreshToken)
        if (res.code === 200) {
          this.token = res.data.token
          localStorage.setItem('token', this.token)
          if (res.data.refresh_token) {
            this.refreshToken = res.data.refresh_token
            localStorage.setItem('refreshToken', this.refreshToken)
          }
          return true
        }
        this.logout()
        return false
      } catch {
        this.logout()
        return false
      }
    },

    async initAuth() {
      if (this.token) {
        this.isAuthenticated = true
        try { await this.fetchUserInfo() } catch {}
        this.startTokenCheck()
      }
    },

    startTokenCheck() {
      this.stopTokenCheck()
      if (this.token) {
        this.tokenCheckTimer = setInterval(() => this.checkToken(), this.tokenCheckInterval)
      }
    },

    stopTokenCheck() {
      if (this.tokenCheckTimer) {
        clearInterval(this.tokenCheckTimer)
        this.tokenCheckTimer = null
      }
    },

    async checkToken() {
      if (!this.token) return
      try {
        const res = await getUserInfoApi()
        if (res.code !== 200) {
          this.stopTokenCheck()
          this.logout(true)
        }
      } catch {
        // silent
      }
    },
  },
})

// 全局401处理
export function handleAuthError() {
  if (authErrorFlag) return
  authErrorFlag = true
  ElMessage.error('未授权，请重新登录')
  const authStore = useAuthStore()
  authStore.logout(true)
  router.push('/login')
  setTimeout(() => { authErrorFlag = false }, 3000)
}
