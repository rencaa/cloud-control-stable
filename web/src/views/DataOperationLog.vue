<template>
  <div>
    <el-card>
      <el-table :data="logs" v-loading="loading">
        <el-table-column prop="username" label="用户" width="120" />
        <el-table-column prop="action" label="操作" width="80" />
        <el-table-column prop="detail" label="详情" min-width="200" show-overflow-tooltip />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[20,50]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getDataLogs } from '@/api/endpoints'

const logs = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)

async function loadData() {
  loading.value = true
  try {
    const res = await getDataLogs({ page: page.value, size: size.value })
    logs.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
