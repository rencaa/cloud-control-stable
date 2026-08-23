<template>
  <div class="profile-page">
    <el-card>
      <el-tabs v-model="activeTab">
        <el-tab-pane label="个人资料" name="info">
          <el-form :model="profileForm" label-width="100px" style="max-width:500px">
            <el-form-item label="用户名">
              <el-input :model-value="authStore.user?.username || ''" disabled />
            </el-form-item>
            <el-form-item label="昵称">
              <el-input v-model="profileForm.nickname" placeholder="请输入昵称" />
            </el-form-item>
            <el-form-item label="邮箱">
              <el-input v-model="profileForm.email" placeholder="请输入邮箱" />
            </el-form-item>
            <el-form-item label="手机号">
              <el-input v-model="profileForm.phone" placeholder="请输入手机号" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="handleSaveProfile" :loading="saving">保存修改</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="修改密码" name="password">
          <el-form :model="passwordForm" label-width="100px" style="max-width:400px">
            <el-form-item label="原密码">
              <el-input v-model="passwordForm.old_password" type="password" show-password />
            </el-form-item>
            <el-form-item label="新密码">
              <el-input v-model="passwordForm.new_password" type="password" show-password />
            </el-form-item>
            <el-form-item label="确认新密码">
              <el-input v-model="passwordForm.confirm_password" type="password" show-password />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="handleChangePassword" :loading="changing">修改密码</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="系统配置" name="config" v-if="authStore.isAdmin">
          <el-form label-width="140px" style="max-width:600px">
            <el-form-item label="站点名称">
              <el-input v-model="systemConfig.site_name" />
            </el-form-item>
            <el-form-item label="站点Logo URL">
              <el-input v-model="systemConfig.site_logo" placeholder="/logo.png" />
            </el-form-item>
            <el-form-item label="登录背景色">
              <el-color-picker v-model="systemConfig.login_background" />
            </el-form-item>
            <el-form-item label="主题色">
              <el-color-picker v-model="systemConfig.theme_primary_color" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="handleSaveConfig" :loading="savingConfig">保存配置</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ElMessage } from 'element-plus'
import { updateProfileApi, changePasswordApi, getSystemConfig, updateSystemConfig } from '@/api/endpoints'

const route = useRoute()
const authStore = useAuthStore()
const activeTab = ref(route.query.tab || 'info')
const saving = ref(false)
const changing = ref(false)
const savingConfig = ref(false)

const profileForm = reactive({
  nickname: authStore.user?.nickname || '',
  email: authStore.user?.email || '',
  phone: authStore.user?.phone || '',
})

const passwordForm = reactive({
  old_password: '',
  new_password: '',
  confirm_password: '',
})

const systemConfig = reactive({
  site_name: '', site_logo: '', login_background: '#000000', theme_primary_color: '#1890ff',
})

async function handleSaveProfile() {
  saving.value = true
  try {
    const res = await updateProfileApi({ nickname: profileForm.nickname, email: profileForm.email, phone: profileForm.phone })
    authStore.user = res.data
    ElMessage.success('资料更新成功')
  } catch (e) { ElMessage.error(e.message) }
  saving.value = false
}

async function handleChangePassword() {
  if (passwordForm.new_password !== passwordForm.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }
  changing.value = true
  try {
    await changePasswordApi({
      old_password: passwordForm.old_password,
      new_password: passwordForm.new_password,
    })
    ElMessage.success('密码修改成功')
    passwordForm.old_password = ''
    passwordForm.new_password = ''
    passwordForm.confirm_password = ''
  } catch (e) { ElMessage.error(e.message) }
  changing.value = false
}

async function loadConfig() {
  try {
    const res = await getSystemConfig()
    const cfg = res.data || {}
    Object.assign(systemConfig, {
      site_name: cfg.site_name || '通用云控系统',
      site_logo: cfg.site_logo || '',
      login_background: cfg.login_background || '#000000',
      theme_primary_color: cfg.theme_primary_color || '#1890ff',
    })
  } catch {}
}

async function handleSaveConfig() {
  savingConfig.value = true
  try {
    await updateSystemConfig(systemConfig)
    ElMessage.success('配置保存成功')
  } catch (e) { ElMessage.error(e.message) }
  savingConfig.value = false
}

onMounted(() => {
  if (authStore.isAdmin) loadConfig()
})
</script>
