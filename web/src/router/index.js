import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    redirect: '/dashboard',
    meta: { requiresAuth: true },
    children: [
      // 仪表盘
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/Dashboard.vue'),
	        meta: { title: '系统概览' },
	      },
	      {
	        path: 'dashboard/fullscreen',
	        name: 'FullscreenDashboard',
	        component: () => import('@/views/FullscreenDashboard.vue'),
	        meta: { title: '数据大屏' },
	      },
	      {
	        path: 'screen-wall',
	        name: 'ScreenWall',
	        component: () => import('@/views/ScreenWall.vue'),
	        meta: { title: '屏幕墙' },
	      },
	      // 设备管理
      {
        path: 'device',
        name: 'DeviceManagement',
        redirect: '/device/list',
        meta: { title: '设备管理' },
        children: [
          {
            path: 'list',
            name: 'DeviceList',
            component: () => import('@/views/DeviceList.vue'),
	        meta: { title: '设备列表' },
	      },
	      {
	        path: 'detail/:id',
	        name: 'DeviceDetail',
	        component: () => import('@/views/DeviceDetail.vue'),
	        meta: { title: '设备详情' },
	      },
	      {
            path: 'groups',
            name: 'DeviceGroups',
            component: () => import('@/views/DeviceGroups.vue'),
            meta: { title: '设备分组' },
          },
          {
            path: 'logs',
            name: 'DeviceLogs',
            component: () => import('@/views/DeviceLogs.vue'),
            meta: { title: '运行日志' },
          },
      {
        path: 'screenshots',
        name: 'DeviceScreenshots',
        component: () => import('@/views/DeviceScreenshots.vue'),
        meta: { title: '设备截图' },
      },
      {
        path: 'built-in-task-logs',
            name: 'BuiltInTaskLogs',
            component: () => import('@/views/BuiltInTaskLogs.vue'),
            meta: { title: '内置任务日志' },
          },
        ],
      },
      // 脚本管理
      {
        path: 'script',
        name: 'ScriptManagement',
        redirect: '/script/list',
        meta: { title: '脚本管理' },
        children: [
          {
            path: 'list',
            name: 'ScriptList',
            component: () => import('@/views/ScriptList.vue'),
            meta: { title: '脚本列表' },
          },
          {
            path: 'shares',
            name: 'ScriptShares',
            component: () => import('@/views/ScriptShares.vue'),
            meta: { title: '共享管理' },
          },
        ],
      },
      // 任务管理
      {
        path: 'task',
        name: 'TaskManagement',
        redirect: '/task/list',
        meta: { title: '任务管理' },
        children: [
          {
            path: 'list',
            name: 'TaskList',
            component: () => import('@/views/TaskList.vue'),
            meta: { title: '任务列表' },
          },
          {
            path: 'logs',
            name: 'TaskLogs',
            component: () => import('@/views/TaskLogs.vue'),
            meta: { title: '任务日志' },
          },
          {
            path: 'shares',
            name: 'TaskShares',
            component: () => import('@/views/TaskShares.vue'),
            meta: { title: '共享管理' },
          },
        ],
      },
      // 资源管理
      {
        path: 'resource',
        name: 'ResourceManagement',
        redirect: '/resource/list',
        meta: { title: '资源管理' },
        children: [
          {
            path: 'list',
            name: 'ResourceList',
            component: () => import('@/views/ResourceList.vue'),
            meta: { title: '资源列表' },
          },
          {
            path: 'shares',
            name: 'ResourceShares',
            component: () => import('@/views/ResourceShares.vue'),
            meta: { title: '共享管理' },
          },
        ],
      },
      // 模板管理
      {
        path: 'template',
        name: 'TemplateManagement',
        redirect: '/template/parameter',
        meta: { title: '模板管理' },
        children: [
          {
            path: 'parameter',
            name: 'ParameterTemplates',
            component: () => import('@/views/ParameterTemplates.vue'),
            meta: { title: '参数模板' },
          },
        ],
      },
      // 数据管理
      {
        path: 'data',
        name: 'DataManagement',
        redirect: '/data/dashboard',
        meta: { title: '数据管理' },
        children: [
          {
            path: 'dashboard',
            name: 'DataDashboard',
            component: () => import('@/views/DataDashboard.vue'),
            meta: { title: '数据看板' },
          },
          {
            path: 'template',
            name: 'DataTemplate',
            component: () => import('@/views/DataTemplate.vue'),
            meta: { title: '模板管理' },
          },
          {
            path: 'permission',
            name: 'DataPermission',
            component: () => import('@/views/DataPermission.vue'),
            meta: { title: '权限管理' },
          },
          {
            path: 'logs',
            name: 'DataOperationLog',
            component: () => import('@/views/DataOperationLog.vue'),
            meta: { title: '操作日志' },
          },
        ],
      },
      // 系统管理
      {
        path: 'system',
        name: 'SystemManagement',
        redirect: '/system/users',
        meta: { title: '系统管理', requiresAdmin: true },
        children: [
          {
            path: 'users',
            name: 'SystemUsers',
            component: () => import('@/views/UserManagement.vue'),
            meta: { title: '用户管理', requiresAdmin: true },
          },
          {
            path: 'roles',
            name: 'SystemRoles',
            component: () => import('@/views/RoleManagement.vue'),
            meta: { title: '角色管理', requiresAdmin: true },
          },
          {
            path: 'permissions',
            name: 'SystemPermissions',
            component: () => import('@/views/PermissionManagement.vue'),
            meta: { title: '权限管理', requiresAdmin: true },
          },
          {
            path: 'logs',
            name: 'SystemLogs',
            component: () => import('@/views/SystemLogs.vue'),
            meta: { title: '系统日志' },
          },
        ],
      },
      // 个人中心
      {
        path: 'profile',
        name: 'Profile',
        component: () => import('@/views/Profile.vue'),
        meta: { title: '个人中心' },
      },
      // API文档
      {
        path: 'api-docs',
        name: 'ApiDocs',
        component: () => import('@/views/ApiDocs.vue'),
        meta: { title: 'API文档' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/login',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth !== false) {
    if (authStore.token && !authStore.isAuthenticated) {
      try {
        await authStore.fetchUserInfo()
      } catch {
        authStore.logout(true)
        next({ name: 'Login', query: { redirect: to.fullPath } })
        return
      }
    }
    if (!authStore.isAuthenticated) {
      next({ name: 'Login', query: { redirect: to.fullPath } })
      return
    }
  }

  if (to.path === '/login' && authStore.isAuthenticated) {
    next({ path: '/' })
    return
  }

  if (to.meta.requiresAdmin) {
    if (!authStore.isAdmin) {
      next({ path: '/' })
      return
    }
  }

  next()
})

export default router
