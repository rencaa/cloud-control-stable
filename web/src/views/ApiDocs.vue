<template>
  <div class="api-docs">
    <el-card>
      <h2>API 接口文档</h2>
      <p style="color:#909399;margin-bottom:20px">Base URL: /api/v1 | Auth: Bearer Token</p>

      <el-collapse v-model="activeNames">
        <el-collapse-item title="认证模块" name="auth">
          <el-table :data="authApis" size="small">
            <el-table-column prop="method" label="方法" width="80">
              <template #default="{ row }"><el-tag :type="methodType(row.method)" size="small">{{ row.method }}</el-tag></template>
            </el-table-column>
            <el-table-column prop="path" label="路径" width="200" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>

        <el-collapse-item title="设备管理" name="device">
          <el-table :data="deviceApis" size="small">
            <el-table-column prop="method" label="方法" width="80">
              <template #default="{ row }"><el-tag :type="methodType(row.method)" size="small">{{ row.method }}</el-tag></template>
            </el-table-column>
            <el-table-column prop="path" label="路径" width="300" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>

        <el-collapse-item title="脚本管理" name="script">
          <el-table :data="scriptApis" size="small">
            <el-table-column prop="method" label="方法" width="80" />
            <el-table-column prop="path" label="路径" width="250" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>

        <el-collapse-item title="任务管理" name="task">
          <el-table :data="taskApis" size="small">
            <el-table-column prop="method" label="方法" width="80" />
            <el-table-column prop="path" label="路径" width="280" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>

        <el-collapse-item title="资源管理" name="resource">
          <el-table :data="resourceApis" size="small">
            <el-table-column prop="method" label="方法" width="80" />
            <el-table-column prop="path" label="路径" width="280" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>

        <el-collapse-item title="系统管理" name="system">
          <el-table :data="systemApis" size="small">
            <el-table-column prop="method" label="方法" width="80" />
            <el-table-column prop="path" label="路径" width="280" />
            <el-table-column prop="desc" label="说明" />
          </el-table>
        </el-collapse-item>
      </el-collapse>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const activeNames = ref(['auth', 'device', 'task'])

function methodType(m) {
  return { GET: 'success', POST: 'primary', PUT: 'warning', DELETE: 'danger' }[m] || 'info'
}

const authApis = [
  { method: 'POST', path: '/auth/login', desc: '用户登录' },
  { method: 'POST', path: '/auth/logout', desc: '用户登出' },
  { method: 'POST', path: '/auth/refresh', desc: '刷新Token' },
  { method: 'GET', path: '/user/info', desc: '获取当前用户信息' },
  { method: 'POST', path: '/user/change-password', desc: '修改密码' },
  { method: 'PUT', path: '/user/profile', desc: '更新个人资料' },
]

const deviceApis = [
  { method: 'GET', path: '/devices', desc: '设备列表(分页/搜索/筛选)' },
  { method: 'POST', path: '/devices', desc: '注册设备' },
  { method: 'PUT', path: '/devices/:id', desc: '更新设备' },
  { method: 'DELETE', path: '/devices/:id', desc: '删除设备' },
  { method: 'DELETE', path: '/devices/batch', desc: '批量删除' },
  { method: 'POST', path: '/devices/batch/reset', desc: '批量重置状态' },
  { method: 'POST', path: '/devices/batch/add-group', desc: '批量添加分组' },
  { method: 'GET', path: '/device-groups', desc: '分组列表' },
  { method: 'POST', path: '/device-groups', desc: '创建分组' },
]

const scriptApis = [
  { method: 'GET', path: '/scripts', desc: '脚本列表' },
  { method: 'POST', path: '/scripts', desc: '创建脚本' },
  { method: 'PUT', path: '/scripts/:id', desc: '更新脚本' },
  { method: 'DELETE', path: '/scripts/:id', desc: '删除脚本' },
  { method: 'GET', path: '/scripts/:id/content', desc: '获取脚本内容(在线编辑)' },
  { method: 'POST', path: '/scripts/:id/share', desc: '共享脚本' },
]

const taskApis = [
  { method: 'GET', path: '/tasks', desc: '任务列表' },
  { method: 'POST', path: '/tasks', desc: '创建任务(含设备选择)' },
  { method: 'PUT', path: '/tasks/:id', desc: '更新任务' },
  { method: 'DELETE', path: '/tasks/:id', desc: '删除任务' },
  { method: 'POST', path: '/tasks/:id/start', desc: '启动任务' },
  { method: 'POST', path: '/tasks/:id/stop', desc: '停止任务' },
  { method: 'POST', path: '/tasks/:id/reset', desc: '重置任务' },
  { method: 'POST', path: '/tasks/batch/control', desc: '批量控制(start/stop/reset)' },
  { method: 'POST', path: '/tasks/:id/repair', desc: '修复任务(重新下发异常设备)' },
  { method: 'GET', path: '/tasks/:id/logs', desc: '任务日志' },
]

const resourceApis = [
  { method: 'GET', path: '/resources', desc: '资源列表' },
  { method: 'POST', path: '/resources/upload', desc: '上传资源(multipart)' },
  { method: 'DELETE', path: '/resources/:id', desc: '删除资源' },
  { method: 'GET', path: '/resources/:id/download', desc: '下载资源' },
  { method: 'PUT', path: '/resources/:id/replace', desc: '替换资源' },
]

const systemApis = [
  { method: 'GET', path: '/users', desc: '用户列表' },
  { method: 'POST', path: '/users', desc: '创建用户' },
  { method: 'PUT', path: '/users/:id', desc: '更新用户' },
  { method: 'DELETE', path: '/users/:id', desc: '删除用户' },
  { method: 'POST', path: '/users/:id/roles', desc: '分配角色' },
  { method: 'GET', path: '/roles', desc: '角色列表' },
  { method: 'POST', path: '/roles', desc: '创建角色' },
  { method: 'GET', path: '/system/logs', desc: '系统日志' },
  { method: 'GET', path: '/system/config', desc: '获取系统配置' },
  { method: 'PUT', path: '/system/config', desc: '更新系统配置' },
]
</script>
