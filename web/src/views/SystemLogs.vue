<template>
  <div class="system-logs">
    <el-card>
      <el-input v-model="keyword" placeholder="搜索日志" clearable style="width:240px;margin-bottom:16px" @change="loadData" />
      <el-table :data="logs" v-loading="loading" stripe>
        <el-table-column prop="username" label="操作用户" width="120" />
        <el-table-column prop="action" label="操作类型" width="100" />
        <el-table-column prop="resource" label="资源" width="200" />
        <el-table-column prop="detail" label="详情" min-width="200" />
        <el-table-column prop="ip" label="IP地址" width="140" />
        <el-table-column label="操作时间" width="170">
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
import { getSystemLogs } from '@/api/endpoints'

const logs = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const keyword = ref('')

async function loadData() {
  loading.value = true
  try {
    const res = await getSystemLogs({ page: page.value, size: size.value, keyword: keyword.value })
    logs.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
