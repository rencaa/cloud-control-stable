<template>
  <div class="resource-list">
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-input v-model="keyword" placeholder="搜索资源名称" clearable style="width:240px" @change="loadData" />
        <el-upload :before-upload="handleUpload" :show-file-list="false" accept="*">
          <el-button type="primary">上传资源</el-button>
        </el-upload>
      </div>
      <el-table :data="resources" v-loading="loading" stripe>
        <el-table-column prop="name" label="资源名称" min-width="150" />
        <el-table-column prop="filename" label="文件名" width="180" />
        <el-table-column prop="file_size" label="大小" width="100">
          <template #default="{ row }">{{ formatSize(row.file_size) }}</template>
        </el-table-column>
        <el-table-column prop="mime_type" label="类型" width="120" />
        <el-table-column label="下载次数" width="80" prop="download_count" />
        <el-table-column label="上传时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleDownload(row)">下载</el-button>
            <el-button link type="primary" size="small" @click="handleReplace(row)">替换</el-button>
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

    <!-- 共享对话框 -->
    <el-dialog v-model="showShare" title="共享资源" width="400px">
      <el-select v-model="shareUserIds" multiple placeholder="选择用户" style="width:100%">
        <el-option v-for="u in users" :key="u.id" :label="u.username" :value="u.id" />
      </el-select>
      <template #footer>
        <el-button @click="showShare = false">取消</el-button>
        <el-button type="primary" @click="confirmShare">确定</el-button>
      </template>
    </el-dialog>

    <!-- 隐藏的上传组件用于替换 -->
    <el-upload ref="replaceUpload" :before-upload="handleReplaceUpload" :show-file-list="false" accept="*" style="display:none" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getResources, uploadResource, deleteResource, replaceResource, shareResource } from '@/api/endpoints'
import { getUsers } from '@/api/endpoints'

const resources = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(10)
const total = ref(0)
const keyword = ref('')
const users = ref([])

const showShare = ref(false)
const shareResourceId = ref(null)
const shareUserIds = ref([])
const replaceUpload = ref(null)
const replaceResourceId = ref(null)

async function loadData() {
  loading.value = true
  try {
    const res = await getResources({ page: page.value, size: size.value, keyword: keyword.value })
    resources.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

async function handleUpload(file) {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('name', file.name)
  try {
    await uploadResource(formData)
    ElMessage.success('上传成功')
    loadData()
  } catch (e) { ElMessage.error(e.message) }
  return false
}

function handleDownload(row) {
  window.open(`/api/v1/resources/${row.id}/download`, '_blank')
}

function handleReplace(row) {
  replaceResourceId.value = row.id
  // trigger file input
  const input = document.createElement('input')
  input.type = 'file'
  input.onchange = async (e) => {
    const file = e.target.files[0]
    if (!file) return
    const formData = new FormData()
    formData.append('file', file)
    try {
      await replaceResource(row.id, formData)
      ElMessage.success('替换成功')
      loadData()
    } catch (err) { ElMessage.error(err.message) }
  }
  input.click()
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除资源 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteResource(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

function handleShare(row) {
  shareResourceId.value = row.id
  shareUserIds.value = []
  showShare.value = true
}

async function confirmShare() {
  try {
    await shareResource(shareResourceId.value, shareUserIds.value)
    ElMessage.success('共享成功')
    showShare.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

function formatSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return size.toFixed(2) + ' ' + units[i]
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(async () => {
  loadData()
  try { users.value = (await getUsers({ page: 1, size: 100 })).data || [] } catch {}
})
</script>
