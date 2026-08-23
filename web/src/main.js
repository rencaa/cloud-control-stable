import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './styles/global.css'
import {
  Connection, DataBoard, Document, Expand, Fold, FolderOpened, HomeFilled,
  Link, List, Loading, Monitor, MoreFilled, Search, Setting, SetUp, User, VideoPlay,
} from '@element-plus/icons-vue'

import App from './App.vue'
import router from './router'
import { setupConfig } from './utils/config'

const app = createApp(App)

// 只注册页面实际使用的图标，避免把整个图标库发送到每个浏览器。
for (const [key, component] of Object.entries({
  Connection, DataBoard, Document, Expand, Fold, FolderOpened, HomeFilled,
  Link, List, Loading, Monitor, MoreFilled, Search, Setting, SetUp, User, VideoPlay,
})) {
  app.component(key, component)
}

// 加载外部配置
setupConfig().then(() => {
  app.use(createPinia())
  app.use(router)
  app.mount('#app')
})
