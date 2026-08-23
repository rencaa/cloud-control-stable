<template>
  <div class="data-dashboard">
    <el-card header="数据管理看板">
      <el-row :gutter="16">
        <el-col :span="8">
          <el-statistic title="数据模板数" :value="templates.length" />
        </el-col>
        <el-col :span="8">
          <el-statistic title="数据记录数" :value="totalRecords" />
        </el-col>
        <el-col :span="8">
          <el-statistic title="权限分配数" :value="permissions.length" />
        </el-col>
      </el-row>
    </el-card>

    <el-card header="最近记录" style="margin-top:16px">
      <el-table :data="recentRecords" size="small" max-height="300">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column label="数据" min-width="300">
          <template #default="{ row }">{{ formatJson(row.data) }}</template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getDataTemplates, getDataRecords, getDataPermissions } from '@/api/endpoints'

const templates = ref([])
const totalRecords = ref(0)
const permissions = ref([])
const recentRecords = ref([])

async function loadData() {
  try {
    const [tRes, rRes, pRes] = await Promise.all([
      getDataTemplates(),
      getDataRecords({ page: 1, size: 10 }),
      getDataPermissions(),
    ])
    templates.value = tRes.data || []
    totalRecords.value = rRes.total || 0
    recentRecords.value = rRes.data || []
    permissions.value = pRes.data || []
  } catch {}
}

function formatJson(data) {
  if (!data) return '-'
  try {
    const obj = typeof data === 'string' ? JSON.parse(data) : data
    return JSON.stringify(obj)
  } catch { return String(data) }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
