<template>
  <div class="device-groups">
    <el-card>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
        <span>共 {{ groups.length }} 个分组</span>
        <el-button type="primary" @click="showDialog = true">创建分组</el-button>
      </div>
      <el-table :data="groups" v-loading="loading">
        <el-table-column prop="name" label="分组名称" />
        <el-table-column prop="description" label="描述" />
        <el-table-column prop="created_at" label="创建时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="300">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="handleManage(row)">管理设备</el-button>
            <el-button link type="warning" size="small" @click="handleReset(row)">重置状态</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editing ? '编辑分组' : '创建分组'" width="450px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="分组名称" required>
          <el-input v-model="form.name" placeholder="请输入分组名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" placeholder="描述信息" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <!-- 管理设备对话框 -->
    <el-dialog v-model="showManageDialog" title="管理分组设备" width="700px">
      <el-table :data="groupDevices" v-loading="loadingDevices" max-height="400">
        <el-table-column prop="name" label="设备名称" />
        <el-table-column prop="device_id" label="设备ID" />
        <el-table-column prop="ip" label="IP地址" />
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status ? 'success' : 'info'" size="small">
              {{ row.status ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getDeviceGroups, createDeviceGroup, updateDeviceGroup, deleteDeviceGroup, resetGroupDevices, getGroupDevices } from '@/api/endpoints'

const groups = ref([])
const loading = ref(false)
const showDialog = ref(false)
const editing = ref(false)
const currentGroup = ref(null)
const form = ref({ name: '', description: '' })

const showManageDialog = ref(false)
const groupDevices = ref([])
const loadingDevices = ref(false)

async function loadData() {
  loading.value = true
  try {
    const res = await getDeviceGroups()
    groups.value = res.data || []
  } finally { loading.value = false }
}

function handleEdit(row) {
  editing.value = true
  currentGroup.value = row
  form.value = { name: row.name, description: row.description }
  showDialog.value = true
}

async function handleSave() {
  try {
    if (editing.value) {
      await updateDeviceGroup(currentGroup.value.id, form.value)
      ElMessage.success('更新成功')
    } else {
      await createDeviceGroup(form.value)
      ElMessage.success('创建成功')
    }
    showDialog.value = false
    editing.value = false
    form.value = { name: '', description: '' }
    loadData()
  } catch (e) { ElMessage.error(e.message || '操作失败') }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除分组 "${row.name}" 吗？`, '确认', { type: 'warning' })
    await deleteDeviceGroup(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

async function handleReset(row) {
  try {
    await resetGroupDevices(row.id)
    ElMessage.success('重置成功')
  } catch {}
}

async function handleManage(row) {
  showManageDialog.value = true
  loadingDevices.value = true
  try {
    const res = await getGroupDevices(row.id)
    groupDevices.value = res.data || []
  } finally { loadingDevices.value = false }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
