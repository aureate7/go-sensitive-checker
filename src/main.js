// src/main.js
import { createApp } from 'vue'
import App from './App.vue'

import router from './router'

// 组件 JS 按名导入（可摇树），样式按组件单独引入，避免全量 CSS 打包
import {
  ElAlert, ElButton, ElCard, ElCheckbox, ElCheckboxGroup, ElCol, ElContainer,
  ElEmpty, ElForm, ElFormItem, ElHeader, ElIcon, ElInput, ElMain, ElOption,
  ElRow, ElSelect, ElSkeleton, ElSpace, ElSwitch, ElTag, ElTooltip,
} from 'element-plus'
import 'bootstrap-icons/font/bootstrap-icons.min.css'
import './style.css'

// 按组件引入 Element Plus 样式，替代全量 dist/index.css
import 'element-plus/es/components/alert/style/css'
import 'element-plus/es/components/button/style/css'
import 'element-plus/es/components/card/style/css'
import 'element-plus/es/components/checkbox/style/css'
import 'element-plus/es/components/checkbox-group/style/css'
import 'element-plus/es/components/col/style/css'
import 'element-plus/es/components/container/style/css'
import 'element-plus/es/components/empty/style/css'
import 'element-plus/es/components/form/style/css'
import 'element-plus/es/components/form-item/style/css'
import 'element-plus/es/components/header/style/css'
import 'element-plus/es/components/icon/style/css'
import 'element-plus/es/components/input/style/css'
import 'element-plus/es/components/main/style/css'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/option/style/css'
import 'element-plus/es/components/row/style/css'
import 'element-plus/es/components/select/style/css'
import 'element-plus/es/components/skeleton/style/css'
import 'element-plus/es/components/space/style/css'
import 'element-plus/es/components/switch/style/css'
import 'element-plus/es/components/tag/style/css'
import 'element-plus/es/components/tooltip/style/css'

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
