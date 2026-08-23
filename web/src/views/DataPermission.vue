<template>
  <div>
    <el-card header="数据权限管理">
      <div style="margin-bottom:16px">
        <el-select v-model="filterTemplateId" placeholder="选择数据模板" filterable clearable @change="loadData" style="width:240px">
          <el-option v-for="t in templates" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
        <el-button type="primary" style="margin-left:12px" @click="showAddDialog = true">添加权限</el-button>
      </div>
      <el-table :data="permissions" v-loading="loading">
        <el-table-column prop="template_id" label="模板ID" width="80" />
        <el-table-column prop="user_id" label="用户ID" width="80" />
        <el-table-column label="可读" width="60">
          <template #default="{ row }">{{ row.can_read ? '✓' : '✗' }}</template>
        </el-table-column>
        <el-table-column label="可写" width="60">
          <template #default="{ row }">{{ row.can_write ? '✓' : '✗' }}</template>
        </el-table-column>
        <el-table-column label="可删除" width="60">
          <template #default="{ row }">{{ row.can_delete ? '✓' : '✗' }}</template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="showAddDialog" title="设置数据权限" width="450px">
      <el-form :model="permForm" label-width="80px">
        <el-form-item label="数据模板" required>
          <el-select v-model="permForm.template_id" placeholder="选择模板" style="width:100%">
            <el-option v-for="t in templates" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="用户" required>
          <el-select v-model="permForm.user_id" placeholder="选择用户" style="width:100%" filterable>
            <el-option v-for="u in users" :key="u.id" :label="u.username" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="可读"><el-switch v-model="permForm.can_read" /></el-form-item>
        <el-form-item label="可写"><el-switch v-model="permForm.can_write" /></el-form-item>
        <el-form-item label="可删除"><el-switch v-model="permForm.can_delete" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSavePerm">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getDataTemplates, getDataPermissions, setDataPermission } from '@/api/endpoints'
import { getUsers } from '@/api/endpoints'

const templates = ref([])
const users = ref([])
const permissions = ref([])
const loading = ref(false)
const filterTemplateId = ref(null)
const showAddDialog = ref(false)
const permForm = ref({ template_id: null, user_id: null, can_read: true, can_write: false, can_delete: false })

async function loadData() {
  loading.value = true
  try {
    const params = filterTemplateId.value ? { template_id: filterTemplateId.value } : {}
    const res = await getDataPermissions(params)
    permissions.value = res.data || []
  } finally { loading.value = false }
}

async function handleSavePerm() {
  try {
    await setDataPermission({
      template_id: permForm.value.template_id,
      user_id: permForm.value.user_id,
      can_read: permForm.value.can_read ? 1 : 0,
      can_write: permForm.value.can_write ? 1 : 0,
      can_delete: permForm.value.can_delete ? 1 : 0,
    })
    ElMessage.success('权限设置成功')
    showAddDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

onMounted(async () => {
  loadData()
  try { templates.value = (await getDataTemplates()).data || [] } catch {}
  try { users.value = (await getUsers({ page: 1, size: 100 })).data || [] } catch {}
})
</script>
