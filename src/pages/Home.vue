<template>
  <div class="home-page">
    <!-- 顶部 Hero 区域 -->
    <header class="hero-bar">
      <div class="hero-title">
        <span class="hero-badge">敏感词检测 · 控制台</span>
        <h1>让内容审核，像对话一样自然。</h1>
        <p>
          输入一段文本，系统会按照不同类别给出敏感词命中情况和风险评级，
          同时提供高亮预览，帮助你快速做出决策。
        </p>
      </div>

      <div class="hero-meta">
        <div class="meta-pill">
          <span class="dot"></span> 实时检测
        </div>
        <div class="meta-pill">
          <span class="dot dot-green"></span> 多类别策略
        </div>
      </div>
    </header>

    <!-- 主体两栏布局 -->
    <section class="layout-shell glass-card">
      <!-- 左侧：统计 + 描述 -->
      <aside class="pane-left">
        <div class="left-card">
          <h2>
            <el-icon><Histogram /></el-icon>
            词库概览
          </h2>
          <p class="left-sub">
            当前敏感词库实时载入自后端服务，
            支持政治、暴恐、涉黄、辱骂、广告等多种分类。
          </p>
          <div class="stats-wrap">
            <StatisticsCard />
          </div>

          <div class="left-footer">
            <span class="tag">Go · Gin · Vue3</span>
            <span class="tag soft">Sensitive Check Engine</span>
          </div>
        </div>
      </aside>

      <!-- 右侧：输入 + 结果（上下两块，类似 Chat 对话区） -->
      <main class="pane-right">
        <!-- 输入区 -->
        <section class="panel panel-input">
          <div class="panel-header">
            <div>
              <h3><el-icon><Edit /></el-icon> 文本检测</h3>
              <p>在这里粘贴文本，并选择需要启用的检测类别。</p>
            </div>
            <span
              class="chip chip-idle"
              v-if="!loading"
            >待检测</span>
            <span
              class="chip chip-loading"
              v-else
            >检测中…</span>
          </div>

          <CategoryPanel @detect="handleDetect" />
        </section>

        <!-- 结果区 -->
        <section class="panel panel-result" ref="resultPanel">
          <div class="panel-header">
            <div>
              <h3>
                <el-icon><MessageBox /></el-icon>
                检测结果
              </h3>
              <p v-if="!result">尚未开始检测，提交文本后将展示详细结果。</p>
              <p v-else>下方为本次检测的风险评分、分类统计以及高亮预览。</p>
            </div>

            <div v-if="result" class="chip-group">
              <span :class="['chip', 'chip-level', riskLevelClass]">
                风险：{{ riskLevelText }}
              </span>
              <span class="chip chip-count">
                命中：{{ displayHitCount }} 词
              </span>
            </div>
          </div>

          <div class="result-shell">
            <ResultPanel
              :loading="loading"
              :result="result"
              :original-text="originalText"
            />
          </div>
        </section>

      </main>
    </section>
  </div>
</template>

<script setup>
defineOptions({ name: 'DetectionHome' })

import { computed, ref, nextTick } from 'vue'
import axios from 'axios'

import StatisticsCard from '@/components/StatisticsCard.vue'
import CategoryPanel from '@/components/CategoryPanel.vue'
import ResultPanel from '@/components/ResultPanel.vue'
import {Edit, Histogram, MessageBox} from '@element-plus/icons-vue'

const loading = ref(false)
const result = ref(null)
const originalText = ref('')
const resultPanel = ref(null)

const riskLevelText = computed(() => {
  if (!result.value) return '--'
  const mp = {
    high: '高风险',
    medium: '中风险',
    low: '低风险',
    safe: '安全',
  }
  return mp[result.value.risk_level] || result.value.risk_level || '--'
})

const riskLevelClass = computed(() => {
  if (!result.value) return 'level-safe'  // 默认安全

  switch (result.value.risk_level) {
    case 'high':
      return 'level-high';  // 高风险
    case 'medium':
      return 'level-medium';  // 中风险
    case 'low':
      return 'level-low';  // 低风险
    default:
      return 'level-safe';  // 安全
  }
})

const displayHitCount = computed(() => {
  if (!result.value) return 0
  const evidences = result.value.hit_evidences
  if (Array.isArray(evidences) && evidences.length) {
    const uniq = new Set()
    evidences.forEach((ev) => {
      const start = Number(ev?.start)
      const end = Number(ev?.end)
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return
      uniq.add(`${start}:${end}`)
    })
    if (uniq.size > 0) return uniq.size
  }
  return result.value.total_count || (result.value.detected_words?.length ?? 0)
})

// 接收 CategoryPanel 发来的检测请求
const handleDetect = async ({ text, categories, options }) => {
  if (!text.trim()) return

  loading.value = true
  originalText.value = text

  try {
    const resp = await axios.post('/api/detect', {
      text,
      categories,
      options,
    })
    result.value = resp.data
  } catch (err) {
    console.error('检测失败', err)
    result.value = {
      has_sensitive: false,
      error: err.message || '检测失败',
    }
  } finally {
    loading.value = false

    // 等 DOM 更新完，再滚动
    await nextTick()

    // 自动滚动到结果面板
    if (resultPanel.value) {
      resultPanel.value.scrollIntoView({
        behavior: 'smooth',
        block: 'start',
      })
    }
    // await nextTick()

    // const top = resultPanel.value?.getBoundingClientRect().top
    // if (top !== undefined) {
    //   window.scrollTo({
    //     top: window.scrollY + top - 20,
    //     behavior: "smooth",
    //   })
    // }

  }
}
</script>

<style scoped>
.home-page {
  padding-bottom: 12px;
  color: var(--text-main);
}

/* 顶部 Hero 区域 */
.hero-bar {
  margin-bottom: 18px;
  color: var(--text-main);
  display: flex;
  justify-content: space-between;
  gap: 24px;
}

.hero-title h1 {
  color: var(--text-main);
  font-size: 28px;
  line-height: 1.25;
  margin: 10px 0 8px;
}

.hero-title p {
  margin: 0;
  color: var(--text-sub);
  font-size: 13px;
  /* max-width: 520px; */
}

.hero-badge {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 11px;
  background: rgba(15, 23, 42, 0.03);
  border: 1px solid rgba(148, 163, 184, 0.5);
  color: var(--text-sub);
}

.hero-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 8px;
}

.meta-pill {
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 11px;
  border: 1px solid rgba(148, 163, 184, 0.45);
  color: var(--text-sub);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: rgba(15, 23, 42, 0.03);
}

.app-shell--dark .meta-pill {
  background: rgba(15, 23, 42, 0.85);
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: #f97316;
}
.dot-green {
  background: #22c55e;
}

/* 主体两栏布局整体壳子 */
.layout-shell {
  display: grid;
  grid-template-columns: minmax(260px, 320px) minmax(0, 1fr);
  gap: 20px;
  padding: 20px;
  background: var(--panel-shell-bg);
  border-radius: 18px;
  border: 1px solid var(--border-subtle);
  box-shadow: var(--shadow-subtle);
}

/* 左侧面板 */
.pane-left {
  border-right: 1px solid var(--border-subtle);
  padding-right: 16px;
}

.left-card {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.left-card h2 {
  color: var(--text-main);
  font-size: 18px;
  margin: 0 0 6px;
}

.left-sub {
  margin: 0 0 14px;
  font-size: 12px;
  color: var(--text-sub);
}

.hero-title p {
  margin: 0;
  color: var(--text-sub);
}

.stats-wrap {
  flex: 1;
  min-height: 0;
  padding: 10px 0;
}

/* 左下角标签 */
.left-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

.tag {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid rgba(148, 163, 184, 0.5);
  color: var(--text-sub);
}

.tag.soft {
  background: rgba(34, 197, 94, 0.08);
  border-color: rgba(34, 197, 94, 0.45);
  color: #16a34a;
}

.app-shell--dark .tag.soft {
  background: rgba(34, 197, 94, 0.17);
  color: #bbf7d0;
}

/* 右侧：输入 + 结果 */
.pane-right {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* 面板通用样式 */
.panel {
  background: var(--bg-card-soft);
  border-radius: 16px;
  border: 1px solid var(--border-soft);
  padding: 14px 16px;
  box-shadow: var(--shadow-subtle);
}

.app-shell--dark .panel {
  background: var(--bg-card-soft);
  box-shadow: var(--shadow-subtle);
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 5px;
}

.panel-header h3 {
  margin: 0 0 4px;
  font-size: 15px;
  color: var(--text-main);
}

.panel-header p {
  margin: 0;
  font-size: 12px;
  color: var(--text-sub);
}


/* 右上角 chips */
.chip-group {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid rgba(148, 163, 184, 0.45);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}

.chip-idle {
  color: var(--text-sub);
  background: rgba(15, 23, 42, 0.02);
}

.app-shell--dark .chip-idle {
  background: rgba(15, 23, 42, 0.85);
}

.chip-loading {
  border-color: rgba(56, 189, 248, 0.7);
  background: rgba(56, 189, 248, 0.16);
  color: #0ea5e9;
}

.chip-level {
  border-color: rgba(249, 115, 22, 0.7);
  background: rgba(249, 115, 22, 0.12);
  color: #c2410c;
}

.app-shell--dark .chip-level {
  color: #fed7aa;
}

.chip-count {
  border-color: rgba(148, 163, 184, 0.6);
  background: rgba(15, 23, 42, 0.03);
}

.app-shell--dark .chip-count {
  background: rgba(15, 23, 42, 0.95);
}

/* 结果区域：限制高度 + 内部滚动由 ResultPanel 控制 */
.panel-result {
  flex: 1;
  min-height: 260px;
  display: flex;
  flex-direction: column;
}

.result-shell {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* 风险等级颜色（使用渐变色） */
/* 风险等级颜色（使用渐变色） */
.level-high {
  background: linear-gradient(135deg, #f87171, #ef4444);  /* 红色渐变 */
  color: #fff;
  border: none; /* 移除边框 */
  box-shadow: 0 4px 12px rgba(239, 68, 68, 0.3);
  
  transition-property: background, box-shadow;
  transition-duration: 0.3s;
  transition-timing-function: ease;
}

.level-medium {
  background: linear-gradient(135deg, #fbbf24, #f59e0b);  /* 黄色渐变 */
  color: #fff;
  border: none; /* 移除边框 */
  box-shadow: 0 4px 12px rgba(249, 158, 11, 0.3);
  
  transition-property: background, box-shadow;
  transition-duration: 0.3s;
  transition-timing-function: ease;
}

.level-low {
  background: linear-gradient(135deg, #93c5fd, #3b82f6);  /* 蓝色渐变 */
  color: #fff;
  border: none; /* 移除边框 */
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.3);
  
  transition-property: background, box-shadow;
  transition-duration: 0.3s;
  transition-timing-function: ease;
}

.level-safe {
  background: linear-gradient(135deg, #34d399, #10b981);  /* 绿色渐变 */
  color: #fff;
  border: none; /* 移除边框 */
  box-shadow: 0 4px 12px rgba(16, 185, 129, 0.3);
  
  transition-property: background, box-shadow;
  transition-duration: 0.3s;
  transition-timing-function: ease;
}


.chip-level {
  padding: 4px 12px;
  border-radius: 999px;
  font-weight: bold;
  transition: background-color 0.3s ease, box-shadow 0.3s ease;
  color: #fff; /* 使用全局文本颜色变量 */
}

/* 深色模式下的颜色修正 */
.app-shell--dark .chip-level {
  color: var(--text-main); /* 确保深色模式下文字颜色一致 */
}




/* 响应式处理 */
@media (max-width: 960px) {
  .layout-shell {
    grid-template-columns: minmax(0, 1fr);
    padding: 16px;
  }

  .pane-left {
    border-right: none;
    border-bottom: 1px solid var(--border-subtle);
    padding-right: 0;
    padding-bottom: 14px;
    margin-bottom: 8px;
  }
}

@media (max-width: 640px) {
  .hero-bar {
    flex-direction: column;
  }
}

/* ① 外层容器：作为“黑/灰色外壳” */
.panel-input,
.panel-result {
  background: var(--result-shell-bg);  /* 浅色：灰 · 深色：黑 */
  padding: 18px;                       /* 露出一圈外壳 */
  border-radius: 18px;
  border: none;
  box-shadow: none;
}

/* ② 内层白卡片：CategoryPanel / ResultPanel 里的 el-card */
/* 这里一定要用 :deep，否则 scoped 打不到子组件 */
.panel-input :deep(.category-card),
.panel-result :deep(.result-card) {
  background: #ffffff;
  border-radius: 14px;
  border: 1px solid rgba(148, 163, 184, 0.35);
  box-shadow:
    0 18px 40px rgba(15, 23, 42, 0.16),
    0 0 0 1px rgba(255, 255, 255, 0.9);
}

/* ③ 暗色模式下，白卡片里的文字偏亮一点 */
.app-shell--dark .panel-input :deep(.category-card),
.app-shell--dark .panel-result :deep(.result-card) {
  color: #e5e7eb;
}

/* ④ 浅色模式下，白卡片里的文字用深色 */
.app-shell:not(.app-shell--dark) .panel-input :deep(.category-card),
.app-shell:not(.app-shell--dark) .panel-result :deep(.result-card) {
  color: #111827;
}

</style>
