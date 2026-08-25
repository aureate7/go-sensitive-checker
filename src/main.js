// src/main.js
import { createApp } from 'vue'
import App from './App.vue'

import router from './router'

import {
  ElAlert, ElButton, ElCard, ElCheckbox, ElCheckboxGroup, ElCol, ElContainer,
  ElEmpty, ElForm, ElFormItem, ElHeader, ElIcon, ElInput, ElMain, ElOption,
  ElRow, ElSelect, ElSkeleton, ElSpace, ElSwitch, ElTag, ElTooltip,
} from 'element-plus'
import 'bootstrap-icons/font/bootstrap-icons.min.css'
import 'element-plus/dist/index.css'
import './style.css'

const app = createApp(App)

// 生产环境配置
if (import.meta.env.PROD) {
  app.config.devtools = false
}

app.use(router)
const elementComponents = [
  ElAlert, ElButton, ElCard, ElCheckbox, ElCheckboxGroup, ElCol, ElContainer,
  ElEmpty, ElForm, ElFormItem, ElHeader, ElIcon, ElInput, ElMain, ElOption,
  ElRow, ElSelect, ElSkeleton, ElSpace, ElSwitch, ElTag, ElTooltip,
]
elementComponents.forEach((component) => app.component(component.name, component))
app.mount('#app')
