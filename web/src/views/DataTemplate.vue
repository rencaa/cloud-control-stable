<template>
  <div class="data-template">
    <el-card>
      <div style="margin-bottom:16px">
        <el-button type="primary" @click="handleCreate">创建数据模板</el-button>
      </div>
      <el-table :data="templates" v-loading="loading">
        <el-table-column prop="name" label="模板名称" min-width="150" />
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column label="字段定义" min-width="200">
          <template #default="{ row }">{{ formatFields(row.fields) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="handleManageRecords(row)">管理数据</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 创建/编辑对话框 -->
    <el-dialog v-model="showDialog" :title="editing ? '编辑模板' : '创建模板'" width="600px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="模板名称" required>
          <el-input v-model="form.name" placeholder="模板名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" />
        </el-form-item>
        <el-form-item label="字段定义" required>
          <div style="margin-bottom:8px">
            <el-button size="small" @click="addField">添加字段</el-button>
          </div>
          <div v-for="(field, index) in form.fields" :key="index" style="display:flex;gap:8px;margin-bottom:8px;align-items:center">
            <el-input v-model="field.name" placeholder="字段名" size="small" style="width:120px" />
            <el-select v-model="field.type" placeholder="类型" size="small" style="width:100px">
              <el-option label="文本" value="text" />
              <el-option label="数字" value="number" />
              <el-option label="日期" value="date" />
              <el-option label="布尔" value="boolean" />
              <el-option label="JSON" value="json" />
            </el-select>
            <el-input v-model="field.label" placeholder="显示名" size="small" style="width:120px" />
            <el-button size="small" type="danger" @click="form.fields.splice(index, 1)" :icon="'Delete'">删除</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <!-- 管理数据对话框 -->
    <el-dialog v-model="showRecordsDialog" title="管理数据记录" width="800px">
      <div style="margin-bottom:12px">
        <el-button type="primary" size="small" @click="handleAddRecord">添加记录</el-button>
      </div>
      <el-table :data="records" v-loading="loadingRecords" max-height="400">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column label="数据" min-width="300">
          <template #default="{ row }">{{ formatJson(row.data) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEditRecord(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="handleDeleteRecord(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <!-- 添加/编辑记录对话框 -->
    <el-dialog v-model="showRecordForm" :title="editingRecord ? '编辑记录' : '添加记录'" width="500px">
      <el-form-item v-for="f in currentTemplateFields" :key="f.name" :label="f.label || f.name">
        <el-input v-if="f.type === 'text'" v-model="recordForm[f.name]" />
        <el-input-number v-else-if="f.type === 'number'" v-model="recordForm[f.name]" style="width:100%" />
        <el-input v-else v-model="recordForm[f.name]" type="textarea" :rows="3" :placeholder="`请输入${f.type}格式数据`" />
      </el-form-item>
      <template #footer>
        <el-button @click="showRecordForm = false">取消</el-button>
        <el-button type="primary" @click="handleSaveRecord">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getDataTemplates, createDataTemplate, updateDataTemplate, deleteDataTemplate, getDataRecords, createDataRecord, updateDataRecord, deleteDataRecord } from '@/api/endpoints'

const templates = ref([])
const loading = ref(false)
const showDialog = ref(false)
const editing = ref(false)
const currentId = ref(null)
const form = ref({ name: '', description: '', fields: [] })

const showRecordsDialog = ref(false)
const records = ref([])
const loadingRecords = ref(false)
const currentTemplateId = ref(null)
const currentTemplateFields = ref([])

const showRecordForm = ref(false)
const editingRecord = ref(null)
const recordForm = ref({})

async function loadData() {
  loading.value = true
  try {
    const res = await getDataTemplates()
    templates.value = res.data || []
  } finally { loading.value = false }
}

function formatFields(fields) {
  if (!fields) return '-'
  try {
    const arr = typeof fields === 'string' ? JSON.parse(fields) : fields
    return arr.map(f => `${f.label || f.name}(${f.type})`).join(', ')
  } catch { return String(fields) }
}

function formatJson(data) {
  if (!data) return '-'
  try { return JSON.stringify(typeof data === 'string' ? JSON.parse(data) : data) } catch { return String(data) }
}

function handleCreate() {
  editing.value = false; currentId.value = null
  form.value = { name: '', description: '', fields: [] }
  showDialog.value = true
}

function handleEdit(row) {
  editing.value = true; currentId.value = row.id
  let fields = row.fields
  try { fields = typeof fields === 'string' ? JSON.parse(fields) : fields } catch { fields = [] }
  form.value = { name: row.name, description: row.description, fields: fields || [] }
  showDialog.value = true
}

function addField() {
  form.value.fields.push({ name: '', type: 'text', label: '' })
}

async function handleSave() {
  try {
    const data = { ...form.value, fields: JSON.stringify(form.value.fields) }
    if (editing.value) {
      await updateDataTemplate(currentId.value, data)
    } else {
      await createDataTemplate(data)
    }
    ElMessage.success(editing.value ? '更新成功' : '创建成功')
    showDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteDataTemplate(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

async function handleManageRecords(row) {
  currentTemplateId.value = row.id
  try {
    currentTemplateFields.value = typeof row.fields === 'string' ? JSON.parse(row.fields) : row.fields
  } catch { currentTemplateFields.value = [] }
  loadingRecords.value = true
  showRecordsDialog.value = true
  try {
    const res = await getDataRecords({ template_id: row.id, page: 1, size: 100 })
    records.value = res.data || []
  } finally { loadingRecords.value = false }
}

function handleAddRecord() {
  editingRecord.value = null
  recordForm.value = {}
  showRecordForm.value = true
}

function handleEditRecord(row) {
  editingRecord.value = row
  let data = row.data
  try { data = typeof data === 'string' ? JSON.parse(data) : data } catch {}
  recordForm.value = data || {}
  showRecordForm.value = true
}

async function handleSaveRecord() {
  try {
    const data = {
      template_id: currentTemplateId.value,
      data: JSON.stringify(recordForm.value),
    }
    if (editingRecord.value) {
      await updateDataRecord(editingRecord.value.id, { data: data.data })
    } else {
      await createDataRecord(data)
    }
    ElMessage.success(editingRecord.value ? '更新成功' : '创建成功')
    showRecordForm.value = false
    handleManageRecords({ id: currentTemplateId.value, fields: JSON.stringify(currentTemplateFields.value) })
  } catch (e) { ElMessage.error(e.message) }
}

async function handleDeleteRecord(row) {
  try {
    await ElMessageBox.confirm('确定删除此记录？', '确认', { type: 'warning' })
    await deleteDataRecord(row.id)
    ElMessage.success('删除成功')
    handleManageRecords({ id: currentTemplateId.value, fields: JSON.stringify(currentTemplateFields.value) })
  } catch {}
}

onMounted(loadData)
</script>
