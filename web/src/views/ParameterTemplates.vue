<template>
  <div class="parameter-templates">
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-select v-model="filterType" placeholder="模板类型" clearable @change="loadData" style="width:160px">
          <el-option label="脚本参数" value="script" />
          <el-option label="任务参数" value="task" />
          <el-option label="设备参数" value="device" />
        </el-select>
        <el-button type="primary" @click="handleCreate">创建模板</el-button>
      </div>
      <el-table :data="templates" v-loading="loading">
        <el-table-column prop="name" label="模板名称" min-width="150" />
        <el-table-column prop="type" label="类型" width="100">
          <template #default="{ row }">
            <el-tag size="small">{{ { script: '脚本参数', task: '任务参数', device: '设备参数' }[row.type] || row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editing ? '编辑模板' : '创建模板'" width="550px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="模板名称" required>
          <el-input v-model="form.name" placeholder="模板名称" />
        </el-form-item>
        <el-form-item label="类型" required>
          <el-select v-model="form.type" placeholder="选择类型" style="width:100%" :disabled="editing">
            <el-option label="脚本参数" value="script" />
            <el-option label="任务参数" value="task" />
            <el-option label="设备参数" value="device" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" />
        </el-form-item>
        <el-form-item label="参数配置" required>
          <el-input v-model="form.params_str" type="textarea" :rows="8" placeholder='JSON格式参数: {"key": "value", "timeout": 30}' />
          <div style="color:#909399;font-size:12px;margin-top:4px">
            提示: 设备参数优先级最高，任务参数次之，脚本参数最低
          </div>
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
import { getTemplates, createTemplate, updateTemplate, deleteTemplate } from '@/api/endpoints'

const templates = ref([])
const loading = ref(false)
const filterType = ref('')

const showDialog = ref(false)
const editing = ref(false)
const currentId = ref(null)
const form = ref({ name: '', type: 'task', description: '', params_str: '' })

async function loadData() {
  loading.value = true
  try {
    const params = filterType.value ? { type: filterType.value } : {}
    const res = await getTemplates(params)
    templates.value = res.data || []
  } finally { loading.value = false }
}

function handleCreate() {
  editing.value = false
  currentId.value = null
  form.value = { name: '', type: 'task', description: '', params_str: '' }
  showDialog.value = true
}

function handleEdit(row) {
  editing.value = true
  currentId.value = row.id
  form.value = {
    name: row.name,
    type: row.type,
    description: row.description,
    params_str: typeof row.params === 'string' ? row.params : JSON.stringify(row.params, null, 2),
  }
  showDialog.value = true
}

async function handleSave() {
  try {
    let paramsJson = null
    try {
      if (form.value.params_str) paramsJson = JSON.parse(form.value.params_str)
    } catch {
      ElMessage.error('参数格式错误，请输入有效的JSON')
      return
    }

    const data = {
      name: form.value.name,
      type: form.value.type,
      description: form.value.description,
      params: JSON.stringify(paramsJson),
    }

    if (editing.value) {
      await updateTemplate(currentId.value, data)
      ElMessage.success('更新成功')
    } else {
      await createTemplate(data)
      ElMessage.success('创建成功')
    }
    showDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除模板 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteTemplate(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
