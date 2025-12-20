<template>
  <el-card class="result-card" shadow="hover">
    <template #header>
      <div class="card-header">
        <span>检测结果</span>
        <span class="status-tag" v-if="result">
          <el-tag v-if="result.has_sensitive" type="danger">
            已发现敏感内容
          </el-tag>
          <el-tag v-else type="success">
            内容安全
          </el-tag>
        </span>
      </div>
    </template>

    <!-- 1. 加载中 -->
    <div v-if="loading" class="loading-wrap">
      <el-skeleton :rows="4" animated />
      <div class="loading-text">正在检测文本内容，请稍候…</div>
    </div>

    <!-- 2. 还没检测 -->
    <div v-else-if="!result" class="empty-wrap">
      <el-empty description="输入文本并点击“开始检测”" :image-size="120" />
    </div>

    <!-- 3. 有检测结果（不管是否有敏感词） -->
    <div v-else class="result-wrap">
      <!-- 3.1 总体提示 -->
      <div class="result-summary">
        <el-alert
          v-if="result.has_sensitive"
          type="error"
          show-icon
          :closable="false"
        >
          检测到
          <strong>{{ result.total_count || (result.detected_words?.length || 0) }}</strong>
          个敏感词，
          风险等级：
          <el-tag size="small" type="danger">
            {{ riskLevelText }}
          </el-tag>
        </el-alert>

        <el-alert v-else type="success" show-icon :closable="false">
          恭喜，未发现敏感词，文本整体安全。
        </el-alert>
      </div>

      <!-- 3.2 分类统计（有敏感词时） -->
      <div
        v-if="result.has_sensitive && result.category_summary && Object.keys(result.category_summary).length"
        class="category-summary"
      >
        <h4>按类别统计</h4>
        <el-row :gutter="8">
          <el-col
            v-for="(stats, cat) in result.category_summary"
            :key="cat"
            :xs="24"
            :sm="12"
          >
            <div class="cat-item">
              <div class="cat-name">{{ categoryMap[cat] || cat }}</div> <!-- 显示中文名称 -->
              <div class="cat-badges">
                <el-tag size="small">总计 {{ stats.total }}</el-tag>
                <el-tag v-if="stats.high > 0" size="small" type="danger">高 {{ stats.high }}</el-tag>
                <el-tag v-if="stats.medium > 0" size="small" type="warning">中 {{ stats.medium }}</el-tag>
                <el-tag v-if="stats.low > 0" size="small" type="info">低 {{ stats.low }}</el-tag>
              </div>
            </div>
          </el-col>
        </el-row>
      </div>

      <!-- 3.3 具体敏感词列表 -->
      <div
        v-if="result.has_sensitive && (result.categories || result.detected_words)"
        class="words-block"
      >
        <h4>命中敏感词</h4>

        <!-- 优先按 categories 分组展示（卡片 + 标签网格） -->
        <template v-if="result.categories">
          <el-row :gutter="12">
            <el-col
              v-for="(data, cat) in result.categories"
              :key="cat"
              :xs="24"
              :sm="12"
              :md="8"
              class="word-col"
            >
              <el-card shadow="never" class="word-card">
                <template #header>
                  <div class="word-card__header">
                    <span class="word-card__title">{{ categoryMap[cat] || cat }}</span>
                    <el-tag size="small" type="info">
                      {{ data.words ? data.words.length : data.count }} 个
                    </el-tag>
                  </div>
                </template>

                <div class="word-grid">
                  <el-tag
                    v-for="w in normalizeWords(data.words || [])"
                    :key="w.word + (w.level || '')"
                    class="word-tag"
                    :type="w.level === 'high' ? 'danger' : (w.level === 'medium' ? 'warning' : 'info')"
                    effect="light"
                    disable-transitions
                  >
                    <span class="word-tag__text">{{ w.word }}</span>
                    <span class="word-tag__count">×{{ w.count_raw || 1 }}</span>
                  </el-tag>
                </div>
              </el-card>
            </el-col>
          </el-row>
        </template>

        <!-- 如果没有 categories 字段，就直接平铺 detected_words -->
        <template v-else-if="result.detected_words && result.detected_words.length">
          <el-space direction="vertical" fill>
            <div
              v-for="w in result.detected_words"
              :key="w.word + w.category"
              class="word-item"
            >
              <span class="word-text">{{ w.word }}</span>
              <span class="word-meta">
                <el-tag size="small">{{ categoryMap[w.category] || w.category }}</el-tag> <!-- 显示中文名称 -->
                <span class="word-count">
                  出现 {{ w.count_raw || 1 }} 次
                </span>
              </span>
            </div>
          </el-space>
        </template>
      </div>

      <!-- 3.4 相似敏感词（在 has_sensitive=false 情况下也可能存在） -->
      <div
        v-if="!result.has_sensitive && result.similar_sensitive && result.similar_words && result.similar_words.length"
        class="similar-block"
      >
        <h4>检测到相似敏感词</h4>
        <el-alert
          type="warning"
          :closable="false"
          show-icon
          description="这些内容未命中严格敏感词，但与敏感词十分相似，建议人工复核。"
        />
        <el-space
          direction="vertical"
          fill
          style="margin-top: 8px"
        >
          <div
            v-for="s in result.similar_words"
            :key="s.sensitive_word + s.matched_text"
            class="word-item"
          >
            <span class="word-text">{{ s.sensitive_word }}</span>
            <span class="word-meta">
              <el-tag size="small" type="info">
                {{ s.category || '相似词' }}
              </el-tag>
              <span class="word-count">
                命中片段：
                <span class="highlight">{{ s.matched_text }}</span>
              </span>
            </span>
          </div>
        </el-space>
      </div>
      <!-- 3.4 敏感词高亮（单独区域 + 滚动条） -->
      <div
        v-if="result.has_sensitive && originalText && detectedWordList.length"
        class="highlight-block"
      >
        <h4>敏感词高亮</h4>
        <div class="preview-scroll">
          <HighlightText
            :text="originalText"
            :words="detectedWordList"
          />
        </div>
      </div>
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue';
import HighlightText from '@/components/HighlightText.vue'   // 新增这一行

const props = defineProps({
  loading: {
    type: Boolean,
    default: false,
  },
  result: {
    type: Object,
    default: null,
  },
  originalText: {
    type: String,
    default: '',
  },
})

const categoryMap = {
  political_high: '政治高敏感',
  political_low: '政治低敏感',
  political_person: '政治敏感人物',
  political_banned_books: '禁书',
  political_prohibited: '政治违禁词',
  violent_high: '暴恐高敏感',
  violent_low: '暴恐低敏感',
  violent_chemical: '化学药剂',
  pornographic_high: '涉黄高敏感',
  pornographic_low: '涉黄低敏感',
  abusive_high: '辱骂高敏感',
  abusive_low: '辱骂低敏感',
  advertising_high: '广告高敏感',
  advertising_low: '广告低敏感',
}

/**
 * 收集所有需要高亮的敏感词
 * 兼容两种结构：
 * 1) result.detected_words: [{ word, ... }, ...]
 * 2) result.categories: { cat: { words: [...] }, ... }
 */

// 词条排序：level(高>中>低) + count_raw(降序)
const normalizeWords = (words = []) => {
  const rank = { high: 3, medium: 2, low: 1 }
  return [...words].sort((a, b) => {
    const ra = rank[a?.level] || 0
    const rb = rank[b?.level] || 0
    if (ra !== rb) return rb - ra

    const ca = Number(a?.count_raw || 1)
    const cb = Number(b?.count_raw || 1)
    if (ca !== cb) return cb - ca

    // 最后按词本身排序，保证稳定输出
    return String(a?.word || '').localeCompare(String(b?.word || ''), 'zh-Hans-CN')
  })
}

const riskLevelText = computed(() => {
  if (!props.result) return '--'
  const mp = {
    high: '高风险',
    medium: '中风险',
    low: '低风险',
    safe: '安全',
  }
  return mp[props.result.risk_level] || props.result.risk_level || '--'
})

/**
 * 收集所有需要高亮的敏感词
 * 兼容两种结构：
 * 1) result.detected_words: [{ word, ... }, ...]
 * 2) result.categories: { cat: { words: [...] }, ... }
 */

// 过滤掉HTML标签
// const cleanText = (text) => {
//   return text.replace(/<\/?[^>]+(>|$)/g, "")
// }

const detectedWordList = computed(() => {
  const r = props.result
  if (!r) return []

  if (Array.isArray(r.detected_words) && r.detected_words.length) {
    return r.detected_words
  }

  if (r.categories) {
    const all = []
    Object.values(r.categories).forEach((cat) => {
      if (Array.isArray(cat.words)) {
        all.push(...cat.words)
      }
    })
    return all
  }
  return []
})

</script>

<style scoped>
.result-card {
  height: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: var(--text-sub);
}

.status-tag {
  font-size: 12px;
}

.loading-wrap {
  padding: 8px 0;
}

.loading-text {
  margin-top: 8px;
  text-align: center;
  color: var(--text-sub);      /* 原来是 #999 */
}

.empty-wrap {
  padding: 24px 0;
  color: var(--text-sub);
}

.result-wrap > * + * {
  margin-top: 16px;
}

.category-summary h4,
.words-block h4,
.similar-block h4,
.preview-block h4 {
  margin: 0 0 8px;
  font-size: 14px;
  color: var(--text-sub);     /* 标题用主文字色 */
}

.highlight-block h4 {
    color: var(--text-sub);
}

.preview-scroll {
  max-height: 240px;       /* 关键：限制高度 */
  overflow-y: auto;        /* 关键：出现滚动条 */
  padding: 10px;
  border-radius: 6px;
  border: 1px solid #eee;
  background: #fafafa;
  font-size: 13px;
  line-height: 1.6;
}

.cat-item {
  padding: 6px 8px;
  border-radius: 6px;
  background: #f7f7fb;
}

.cat-name {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--text-sub);
}

.cat-badges > * + * {
  margin-left: 4px;
}

.word-group + .word-group {
  margin-top: 12px;
}

.group-title {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--text-sub);
}

.group-count {
  margin-left: 6px;
  color: var(--text-sub);
  font-size: 12px;
}

.word-count {
  color: var(--text-sub);
}

.word-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid #eee;
  background: #fff;
}

.word-item + .word-item {
  margin-top: 6px;
}

.word-text {
  font-weight: 600;
  color: var(--text-sub);
}

.word-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.word-count .highlight {
  background: #ffe58f;
  padding: 0 3px;
  border-radius: 3px;
}

/* 预览区域背景用浅 / 深都比较舒服的颜色 */
.preview-scroll,
.preview-box {
  max-height: 240px;
  overflow-y: auto;
  padding: 10px;
  border-radius: 6px;
  border: 1px solid var(--border-soft);
  background: rgba(248, 250, 252, 0.96);
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-sub);
}

/* ===== 命中敏感词：卡片 + 标签网格（更整齐） ===== */
.word-col {
  display: flex;
}

.word-card {
  width: 100%;
  height: 100%;
  border-radius: 12px;
  border: 1px solid rgba(148, 163, 184, 0.25);
  background: rgba(255, 255, 255, 0.92);
}

/* 让卡片内部 padding 更紧凑，整体更像“统计面板” */
.word-card :deep(.el-card__header) {
  padding: 12px 14px;
}

.word-card :deep(.el-card__body) {
  padding: 12px 14px 14px;
}

.word-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.word-card__title {
  font-weight: 600;
  color: var(--text-sub);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 标签改为网格布局：每行对齐、宽度一致 */
.word-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(92px, 1fr));
  gap: 10px;

  /* 类别词多时保持卡片高度更一致 */
  max-height: 170px;
  overflow: auto;
  padding-right: 2px;
}

.word-tag {
  width: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;

  box-sizing: border-box;
  border-radius: 8px;
}

.word-tag__text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.word-tag__count {
  flex: none;
  opacity: 0.75;
  font-size: 12px;
}

/* 深色模式兼容：如果你全局用 CSS 变量做暗色，这里尽量不写死颜色 */
@media (prefers-color-scheme: dark) {
  .word-card {
    background: rgba(15, 23, 42, 0.55);
    border-color: rgba(148, 163, 184, 0.25);
  }
}

.word-grid::-webkit-scrollbar {
  width: 6px;
}
.word-grid::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.35);
  border-radius: 6px;
}
.word-grid::-webkit-scrollbar-track {
  background: transparent;
}
</style>
