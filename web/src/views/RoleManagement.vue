<template>
  <div class="role-management">
    <el-card>
      <div style="margin-bottom:16px">
        <el-button type="primary" @click="handleCreate">创建角色</el-button>
      </div>
      <el-table :data="roles" v-loading="loading" stripe>
        <el-table-column prop="name" label="角色名称" width="150" />
        <el-table-column prop="code" label="角色编码" width="150" />
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column prop="is_system" label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.is_system ? 'warning' : 'info'" size="small">{{ row.is_system ? '系统' : '自定义' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="权限数" width="80">
          <template #default="{ row }">{{ row.permissions?.length || 0 }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)" :disabled="row.is_system">编辑</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)" :disabled="row.is_system || row.code === 'system_admin'">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="showDialog" :title="editing ? '编辑角色' : '创建角色'" width="500px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="角色名称" required>
          <el-input v-model="form.name" placeholder="角色名称" />
        </el-form-item>
        <el-form-item label="角色编码" required>
          <el-input v-model="form.code" placeholder="唯一编码,如: operator" :disabled="editing" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getRoles, createRole, updateRole, deleteRole } from '@/api/endpoints'

const roles = ref([])
const loading = ref(false)
const showDialog = ref(false)
const editing = ref(false)
const currentRole = ref(null)
const form = ref({ name: '', code: '', description: '' })

async function loadData() {
  loading.value = true
  try {
    const res = await getRoles()
    roles.value = res.data || []
  } finally { loading.value = false }
}

function handleCreate() {
  editing.value = false
  currentRole.value = null
  form.value = { name: '', code: '', description: '' }
  showDialog.value = true
}

function handleEdit(row) {
  editing.value = true
  currentRole.value = row
  form.value = { name: row.name, code: row.code, description: row.description }
  showDialog.value = true
}

async function handleSave() {
  try {
    if (editing.value) {
      await updateRole(currentRole.value.id, { name: form.value.name, description: form.value.description })
    } else {
      await createRole(form.value)
    }
    ElMessage.success(editing.value ? '更新成功' : '创建成功')
    showDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除角色 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteRole(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

onMounted(loadData)
</script>
