// src/main.js
import { createApp } from 'vue'
import App from './App.vue'

import router from './router'

import ElementPlus from 'element-plus'
import 'bootstrap-icons/font/bootstrap-icons.min.css'
import 'element-plus/dist/index.css'
import './style.css'

const app = createApp(App)

// 生产环境配置
if (import.meta.env.PROD) {
  app.config.devtools = false
}

app.use(router)
app.use(ElementPlus)
app.mount('#app')
