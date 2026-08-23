<template>
  <div>
    <el-card>
      <el-table :data="shares" v-loading="loading">
        <el-table-column prop="script_name" label="脚本名称" min-width="150" />
        <el-table-column prop="from_user" label="分享者" width="120" />
        <el-table-column label="分享时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getScriptShares } from '@/api/endpoints'

const shares = ref([])
const loading = ref(false)

async function loadData() {
  loading.value = true
  try {
    const res = await getScriptShares()
    shares.value = res.data || []
  } finally { loading.value = false }
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(loadData)
</script>
