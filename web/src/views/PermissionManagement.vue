<template>
  <div class="permission-management">
    <el-card>
      <el-table :data="permissions" v-loading="loading" stripe>
        <el-table-column prop="name" label="权限名称" min-width="180" />
        <el-table-column prop="code" label="权限编码" width="180" />
        <el-table-column prop="resource" label="资源" width="100" />
        <el-table-column prop="action" label="操作" width="80" />
        <el-table-column prop="description" label="描述" min-width="150" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getPermissions } from '@/api/endpoints'

const permissions = ref([])
const loading = ref(false)

async function loadData() {
  loading.value = true
  try {
    const res = await getPermissions()
    permissions.value = res.data || []
  } finally { loading.value = false }
}

onMounted(loadData)
</script>
