<template>
  <div>
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-select v-model="logTaskId" placeholder="选择任务" filterable clearable @change="loadData" style="width:240px">
          <el-option v-for="t in tasks" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
      </div>
      <el-table :data="logs" v-loading="loading" stripe>
        <el-table-column prop="log_type" label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.log_type === 'error' ? 'danger' : 'info'" size="small">{{ row.log_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="消息内容" min-width="300" show-overflow-tooltip />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[20,50,100]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getTaskLogs, getTasks } from '@/api/endpoints'

const route = useRoute()
const logs = ref([])
const tasks = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const logTaskId = ref(route.query.task_id ? Number(route.query.task_id) : null)

async function loadData() {
  if (!logTaskId.value) return
  loading.value = true
  try {
    const res = await getTaskLogs(logTaskId.value, { page: page.value, size: size.value })
    logs.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(async () => {
  try { tasks.value = (await getTasks({ page: 1, size: 200 })).data || [] } catch {}
  if (logTaskId.value) loadData()
})
</script>
