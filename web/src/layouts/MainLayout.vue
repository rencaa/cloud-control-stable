<template>
  <el-container class="main-layout">
    <!-- 侧边栏 -->
    <el-aside :width="isCollapse ? '64px' : '240px'" class="sidebar">
      <div class="logo-container">
        <img v-if="logoUrl" :src="logoUrl" class="logo-img" />
        <span v-if="!isCollapse" class="logo-title">{{ siteName }}</span>
      </div>

      <el-menu
        :default-active="activeMenu"
        :collapse="isCollapse"
        :collapse-transition="false"
        router
        background-color="#001529"
        text-color="#ffffffb3"
        active-text-color="#ffffff"
      >
        <el-menu-item index="/dashboard">
          <el-icon><HomeFilled /></el-icon>
          <span>系统概览</span>
        </el-menu-item>
        <el-menu-item index="/screen-wall">
          <el-icon><Monitor /></el-icon>
          <span>屏幕墙</span>
        </el-menu-item>

        <el-sub-menu index="/device">
          <template #title>
            <el-icon><Monitor /></el-icon>
            <span>设备管理</span>
          </template>
          <el-menu-item index="/device/list">设备列表</el-menu-item>
          <el-menu-item index="/device/groups">设备分组</el-menu-item>
          <el-menu-item index="/device/logs">运行日志</el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="/script">
          <template #title>
            <el-icon><Document /></el-icon>
            <span>脚本管理</span>
          </template>
          <el-menu-item index="/script/list">脚本列表</el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="/task">
          <template #title>
            <el-icon><List /></el-icon>
            <span>任务管理</span>
          </template>
          <el-menu-item index="/task/list">任务列表</el-menu-item>
          <el-menu-item index="/task/logs">任务日志</el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="/resource">
          <template #title>
            <el-icon><FolderOpened /></el-icon>
            <span>资源管理</span>
          </template>
          <el-menu-item index="/resource/list">资源列表</el-menu-item>
        </el-sub-menu>

        <el-sub-menu index="/more">
          <template #title>
            <el-icon><MoreFilled /></el-icon>
            <span>更多</span>
          </template>
          <el-menu-item index="/template/parameter">
            <el-icon><SetUp /></el-icon>
            <span>参数模板</span>
          </el-menu-item>
          <template v-if="authStore.dataManagerEnabled !== false">
            <el-menu-item index="/data/dashboard">数据看板</el-menu-item>
            <el-menu-item index="/data/template">数据模板</el-menu-item>
            <el-menu-item index="/data/permission">数据权限</el-menu-item>
            <el-menu-item index="/data/logs">数据日志</el-menu-item>
          </template>
          <el-menu-item index="/api-docs">
            <el-icon><Link /></el-icon>
            <span>API 文档</span>
          </el-menu-item>
        </el-sub-menu>

        <el-sub-menu v-if="authStore.isAdmin" index="/system">
          <template #title>
            <el-icon><Setting /></el-icon>
            <span>系统管理</span>
          </template>
          <el-menu-item index="/system/users">用户管理</el-menu-item>
          <el-menu-item index="/system/roles">角色管理</el-menu-item>
          <el-menu-item index="/system/permissions">权限管理</el-menu-item>
          <el-menu-item index="/system/logs">系统日志</el-menu-item>
        </el-sub-menu>

        <el-menu-item index="/profile">
          <el-icon><User /></el-icon>
          <span>个人中心</span>
        </el-menu-item>

      </el-menu>
      <div class="version-tag">v72</div>
    </el-aside>

    <!-- 主体 -->
    <el-container class="main-container">
      <!-- 头部 -->
      <el-header class="header">
        <div class="header-left">
          <el-button v-if="isMobile" text @click="drawerVisible = true">
            <el-icon :size="20"><Expand /></el-icon>
          </el-button>
          <el-button v-else text @click="isCollapse = !isCollapse">
            <el-icon :size="20"><Fold v-if="!isCollapse" /><Expand v-else /></el-icon>
          </el-button>
          <el-breadcrumb separator="/">
            <el-breadcrumb-item :to="{ path: '/' }">首页</el-breadcrumb-item>
            <el-breadcrumb-item v-if="currentTitle">{{ currentTitle }}</el-breadcrumb-item>
          </el-breadcrumb>
        </div>
        <div class="header-right">
          <el-dropdown @command="handleCommand">
            <span class="user-info">
              <el-avatar :size="32" :icon="UserFilled" />
              <span class="username">{{ authStore.user?.nickname || authStore.user?.username }}</span>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="profile">个人中心</el-dropdown-item>
                <el-dropdown-item command="password">修改密码</el-dropdown-item>
                <el-dropdown-item divided command="logout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <!-- 内容区 -->
      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>

    <!-- 移动端抽屉菜单 -->
    <el-drawer v-model="drawerVisible" direction="ltr" size="240px" :with-header="false">
      <div class="drawer-logo">{{ siteName }}</div>
      <el-menu :default-active="activeMenu" router @select="drawerVisible = false"
               background-color="#001529" text-color="#ffffffb3" active-text-color="#ffffff">
        <el-menu-item index="/dashboard"><el-icon><HomeFilled /></el-icon>系统概览</el-menu-item>
        <el-menu-item index="/screen-wall"><el-icon><Monitor /></el-icon>屏幕墙</el-menu-item>
        <el-sub-menu index="/device"><template #title><el-icon><Monitor /></el-icon>设备管理</template>
          <el-menu-item index="/device/list">设备列表</el-menu-item>
          <el-menu-item index="/device/groups">设备分组</el-menu-item>
          <el-menu-item index="/device/logs">运行日志</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="/script"><template #title><el-icon><Document /></el-icon>脚本管理</template>
          <el-menu-item index="/script/list">脚本列表</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="/task"><template #title><el-icon><List /></el-icon>任务管理</template>
          <el-menu-item index="/task/list">任务列表</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="/resource"><template #title><el-icon><FolderOpened /></el-icon>资源管理</template>
          <el-menu-item index="/resource/list">资源列表</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="/more"><template #title><el-icon><MoreFilled /></el-icon>更多</template>
          <el-menu-item index="/template/parameter">参数模板</el-menu-item>
          <template v-if="authStore.dataManagerEnabled !== false">
            <el-menu-item index="/data/dashboard">数据看板</el-menu-item>
            <el-menu-item index="/data/template">数据模板</el-menu-item>
            <el-menu-item index="/data/permission">数据权限</el-menu-item>
            <el-menu-item index="/data/logs">数据日志</el-menu-item>
          </template>
          <el-menu-item index="/api-docs">API 文档</el-menu-item>
        </el-sub-menu>
      </el-menu>
    </el-drawer>
  </el-container>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getSiteName } from '@/utils/config'
import { UserFilled } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const isCollapse = ref(window.innerWidth < 768)
const isMobile = ref(window.innerWidth < 768)
const drawerVisible = ref(false)
const siteName = getSiteName()
const logoUrl = ref('')

function onResize() {
  isMobile.value = window.innerWidth < 768
  if (window.innerWidth < 768) isCollapse.value = true
}
onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

function navTo(path) {
  router.push(path)
  if (isMobile.value) drawerVisible.value = false
}

const currentTitle = computed(() => route.meta?.title || '')

const activeMenu = computed(() => {
  const path = route.path
  // Match parent paths for sub-menus
  if (path.startsWith('/device')) return '/device'
  if (path.startsWith('/script')) return '/script'
  if (path.startsWith('/task')) return '/task'
  if (path.startsWith('/resource')) return '/resource'
  if (path.startsWith('/data')) return '/more'
  if (path.startsWith('/template') || path === '/api-docs') return '/more'
  if (path.startsWith('/system')) return '/system'
  return path
})

function handleCommand(command) {
  switch (command) {
    case 'profile':
      router.push('/profile')
      break
    case 'password':
      router.push('/profile?tab=password')
      break
    case 'logout':
      authStore.logout()
      router.push('/login')
      break
  }
}
</script>

<style scoped>
.main-layout { height: 100vh; }
.sidebar { background-color: #001529; overflow-y: auto; overflow-x: hidden; display: flex; flex-direction: column; }
.version-tag { text-align: center; padding: 10px; color: #ffffff40; font-size: 12px; margin-top: auto; }
.logo-container { height: 60px; display: flex; align-items: center; justify-content: center; padding: 0 16px; background: #002140; }
.logo-img { width: 32px; height: 32px; border-radius: 4px; }
.logo-title { color: #fff; font-size: 16px; font-weight: 600; margin-left: 10px; white-space: nowrap; }
.drawer-logo { color: #fff; font-size: 18px; font-weight: 600; padding: 18px 20px; background: #002140; }
.main-container { flex-direction: column; }
.header { display: flex; align-items: center; justify-content: space-between; background: #fff; border-bottom: 1px solid #e8e8e8; padding: 0 16px; height: 56px; }
.header-left { display: flex; align-items: center; gap: 8px; }
.header-right { display: flex; align-items: center; }
.user-info { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.username { font-size: 14px; }
.content { background: #f0f2f5; min-height: calc(100vh - 56px); padding: 12px; }

/* ========== 移动端自适应 ========== */
@media (max-width: 767px) {
  .sidebar { display: none !important; }
  .header { padding: 0 10px; height: 50px; }
  .content { padding: 8px; min-height: calc(100vh - 50px); }
  .username { display: none; }
  .header-left { gap: 4px; }
  .el-breadcrumb { display: none; }
}
</style>
