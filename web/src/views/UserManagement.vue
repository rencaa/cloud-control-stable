<template>
  <div class="user-management">
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-input v-model="keyword" placeholder="搜索用户名/昵称/邮箱" clearable style="width:240px" @change="loadData" />
        <el-button type="primary" @click="handleCreate">创建用户</el-button>
      </div>
      <el-table :data="users" v-loading="loading" stripe>
        <el-table-column prop="username" label="用户名" width="120" />
        <el-table-column prop="nickname" label="昵称" width="120" />
        <el-table-column prop="email" label="邮箱" width="180" />
        <el-table-column prop="phone" label="手机号" width="130" />
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status ? 'success' : 'danger'" size="small">{{ row.status ? '启用' : '禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="150">
          <template #default="{ row }">{{ row.roles?.map(r => r.name).join(', ') || '-' }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="handleAssignRoles(row)">分配角色</el-button>
            <el-button link type="warning" size="small" @click="handleToggleStatus(row)">
              {{ row.status ? '禁用' : '启用' }}
            </el-button>
            <el-button v-if="row.username !== 'admin'" link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[10,20,50]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editing ? '编辑用户' : '创建用户'" width="500px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="用户名" required>
          <el-input v-model="form.username" placeholder="用户名" :disabled="editing" />
        </el-form-item>
        <el-form-item label="密码" :required="!editing">
          <el-input v-model="form.password" type="password" placeholder="留空不修改" show-password />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" placeholder="昵称" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="邮箱" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="form.phone" placeholder="手机号" />
        </el-form-item>
        <el-form-item label="设备配额">
          <el-input-number v-model="form.device_quota" :min="0" placeholder="0不限制" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <!-- 分配角色对话框 -->
    <el-dialog v-model="showRoleDialog" title="分配角色" width="400px">
      <el-checkbox-group v-model="selectedRoleCodes">
        <el-checkbox v-for="r in roles" :key="r.code" :label="r.code" style="display:block;margin-bottom:8px">
          {{ r.name }} ({{ r.description }})
        </el-checkbox>
      </el-checkbox-group>
      <template #footer>
        <el-button @click="showRoleDialog = false">取消</el-button>
        <el-button type="primary" @click="confirmAssignRoles">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getUsers, createUser, updateUser, deleteUser, assignUserRoles, getRoles } from '@/api/endpoints'

const users = ref([])
const roles = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(10)
const total = ref(0)
const keyword = ref('')

const showDialog = ref(false)
const editing = ref(false)
const currentUser = ref(null)
const form = ref({ username: '', password: '', nickname: '', email: '', phone: '', device_quota: 0, status: 1 })

const showRoleDialog = ref(false)
const assignUserId = ref(null)
const selectedRoleCodes = ref([])

async function loadData() {
  loading.value = true
  try {
    const res = await getUsers({ page: page.value, size: size.value, keyword: keyword.value })
    users.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

function handleCreate() {
  editing.value = false
  currentUser.value = null
  form.value = { username: '', password: '', nickname: '', email: '', phone: '', device_quota: 0, status: 1 }
  showDialog.value = true
}

function handleEdit(row) {
  editing.value = true
  currentUser.value = row
  form.value = {
    username: row.username, password: '', nickname: row.nickname,
    email: row.email, phone: row.phone, device_quota: row.device_quota, status: row.status,
  }
  showDialog.value = true
}

async function handleSave() {
  try {
    const data = { ...form.value }
    if (!data.password) delete data.password

    if (editing.value) {
      await updateUser(currentUser.value.id, data)
      ElMessage.success('更新成功')
    } else {
      await createUser(data)
      ElMessage.success('创建成功')
    }
    showDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleToggleStatus(row) {
  try {
    await updateUser(row.id, { status: row.status ? 0 : 1 })
    ElMessage.success(row.status ? '已禁用' : '已启用')
    loadData()
  } catch {}
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除用户 "${row.username}"？`, '确认', { type: 'warning' })
    await deleteUser(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

function handleAssignRoles(row) {
  assignUserId.value = row.id
  selectedRoleCodes.value = row.roles?.map(r => r.code) || []
  showRoleDialog.value = true
}

async function confirmAssignRoles() {
  try {
    await assignUserRoles(assignUserId.value, selectedRoleCodes.value)
    ElMessage.success('角色分配成功')
    showRoleDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(async () => {
  loadData()
  try { roles.value = (await getRoles()).data || [] } catch {}
})
</script>
