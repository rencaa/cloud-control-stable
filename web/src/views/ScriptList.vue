<template>
  <div class="script-list">
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-input v-model="keyword" placeholder="搜索脚本名称" clearable style="width:240px" @change="loadData" />
        <el-button type="primary" @click="handleCreate">上传脚本</el-button>
      </div>
      <el-table :data="scripts" v-loading="loading" stripe>
        <el-table-column prop="name" label="脚本名称" min-width="150" />
        <el-table-column prop="description" label="描述" min-width="200" />
        <el-table-column prop="filename" label="文件名" width="150" />
        <el-table-column prop="is_shared" label="共享" width="80">
          <template #default="{ row }">
            <el-tag :type="row.is_shared ? 'success' : 'info'" size="small">{{ row.is_shared ? '是' : '否' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="170">
          <template #default="{ row }">{{ formatDate(row.updated_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEditCode(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="handleEditInfo(row)">信息</el-button>
            <el-button link type="success" size="small" @click="handleShare(row)">共享</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[10,20,50]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>

    <!-- 脚本编辑器对话框 -->
    <el-dialog v-model="showEditor" title="编辑脚本" width="900px" fullscreen>
      <div style="display:flex;gap:12px;margin-bottom:12px">
        <el-input v-model="editorForm.name" placeholder="脚本名称" style="width:200px" />
        <el-input v-model="editorForm.filename" placeholder="文件名" style="width:200px" />
        <el-input v-model="editorForm.description" placeholder="描述" style="flex:1" />
      </div>
      <div style="border:1px solid #dcdfe6;border-radius:4px;height:500px">
        <textarea v-model="editorForm.content" style="width:100%;height:100%;padding:12px;font-family:monospace;font-size:14px;border:none;outline:none;resize:none;background:#1e1e1e;color:#d4d4d4" placeholder="// 编写脚本代码..." />
      </div>
      <template #footer>
        <el-button @click="showEditor = false">取消</el-button>
        <el-button type="primary" @click="handleSaveContent" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 上传/编辑信息对话框 -->
    <el-dialog v-model="showUpload" :title="editingScript ? '编辑脚本信息' : '上传脚本'" width="500px">
      <el-form :model="uploadForm" label-width="80px">
        <el-form-item label="脚本名称" required>
          <el-input v-model="uploadForm.name" placeholder="脚本名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="uploadForm.description" type="textarea" />
        </el-form-item>
        <el-form-item label="文件名">
          <el-input v-model="uploadForm.filename" placeholder="如: test.js" />
        </el-form-item>
        <el-form-item v-if="!editingScript" label="脚本内容">
          <el-input v-model="uploadForm.content" type="textarea" :rows="8" placeholder="脚本代码" />
        </el-form-item>
        <el-form-item label="默认参数">
          <el-input v-model="uploadForm.params_str" type="textarea" :rows="3" placeholder='JSON格式参数' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showUpload = false">取消</el-button>
        <el-button type="primary" @click="handleSaveUpload">保存</el-button>
      </template>
    </el-dialog>

    <!-- 共享对话框 -->
    <el-dialog v-model="showShare" title="共享脚本" width="400px">
      <el-select v-model="shareUserIds" multiple placeholder="选择要共享的用户" style="width:100%">
        <el-option v-for="u in users" :key="u.id" :label="u.username" :value="u.id" />
      </el-select>
      <template #footer>
        <el-button @click="showShare = false">取消</el-button>
        <el-button type="primary" @click="handleConfirmShare">确定共享</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getScripts, createScript, updateScript, deleteScript, getScriptContent, shareScript } from '@/api/endpoints'
import { getUsers } from '@/api/endpoints'

const scripts = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const size = ref(10)
const total = ref(0)
const keyword = ref('')
const users = ref([])

const showEditor = ref(false)
const editorForm = ref({ id: null, name: '', filename: '', description: '', content: '' })

const showUpload = ref(false)
const editingScript = ref(null)
const uploadForm = ref({ name: '', description: '', filename: '', content: '', params_str: '' })

const showShare = ref(false)
const shareScriptId = ref(null)
const shareUserIds = ref([])

async function loadData() {
  loading.value = true
  try {
    const res = await getScripts({ page: page.value, size: size.value, keyword: keyword.value })
    scripts.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

async function loadUsers() {
  try {
    const res = await getUsers({ page: 1, size: 100 })
    users.value = res.data || []
  } catch {}
}

function handleCreate() {
  editingScript.value = null
  uploadForm.value = { name: '', description: '', filename: '', content: '', params_str: '' }
  showUpload.value = true
}

function handleEditInfo(row) {
  editingScript.value = row
  uploadForm.value = {
    name: row.name, description: row.description, filename: row.filename,
    content: '', params_str: row.params || '',
  }
  showUpload.value = true
}

async function handleEditCode(row) {
  try {
    const res = await getScriptContent(row.id)
    editorForm.value = {
      id: res.data.id, name: res.data.name,
      filename: res.data.filename, description: row.description,
      content: res.data.content || '',
    }
    showEditor.value = true
  } catch { ElMessage.error('获取脚本内容失败') }
}

async function handleSaveContent() {
  saving.value = true
  try {
    await updateScript(editorForm.value.id, {
      name: editorForm.value.name,
      filename: editorForm.value.filename,
      description: editorForm.value.description,
      content: editorForm.value.content,
    })
    ElMessage.success('保存成功')
    showEditor.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message || '保存失败') }
  saving.value = false
}

async function handleSaveUpload() {
  try {
    if (editingScript.value) {
      await updateScript(editingScript.value.id, {
        name: uploadForm.value.name,
        description: uploadForm.value.description,
        filename: uploadForm.value.filename,
        params: uploadForm.value.params_str || null,
      })
    } else {
      await createScript({
        name: uploadForm.value.name,
        description: uploadForm.value.description,
        filename: uploadForm.value.filename || 'script.js',
        content: uploadForm.value.content,
        params: uploadForm.value.params_str || null,
      })
    }
    ElMessage.success(editingScript.value ? '更新成功' : '创建成功')
    showUpload.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除脚本 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteScript(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

function handleShare(row) {
  shareScriptId.value = row.id
  shareUserIds.value = []
  showShare.value = true
}

async function handleConfirmShare() {
  try {
    await shareScript(shareScriptId.value, shareUserIds.value)
    ElMessage.success('共享成功')
    showShare.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(() => { loadData(); loadUsers() })
</script>
