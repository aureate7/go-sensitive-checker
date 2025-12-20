<template>
  <!-- 这里通过 app-shell / app-shell--dark 挂载全局 CSS 变量 -->
  <div :class="['app-shell', { 'app-shell--dark': isDark }]">
    <el-container class="layout">
      <!-- 顶部导航栏（ChatGPT 风格） -->
      <el-header class="nav">
        <!-- 左侧：品牌区 -->
        <div class="nav-left">
          <div class="brand">
            <el-icon class="brand-icon"><Lock /></el-icon>
            <div class="brand-text">
              <div class="brand-title">敏感词检测系统</div>
              <div class="brand-subtitle">Golang · Gin · Vue3</div>
            </div>
          </div>
        </div>

        <!-- 右侧：菜单 -->
        <div class="nav-right">
          <button class="nav-btn" @click="goHome">首页</button>

          <button class="nav-btn ghost" @click="toggleDark">
            <el-icon>
              <component :is="isDark ? Moon : Sunny"></component>
            </el-icon>
            <span>{{ isDark ? '深色模式' : '亮色模式' }}</span>
          </button>

          <a
            class="nav-btn primary"
            href="https://github.com/aureate7/sensitive-word-checker"
            target="_blank"
          >
            GitHub
          </a>
        </div>
      </el-header>

      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>

    <footer class="app-footer">
      <div class="footer-content">
        <div class="copyright">
          © 2025
          <a class="beian" href="http://www.codemo.xin" target="_blank" rel="noopener noreferrer">
            Codemo Lab
          </a>
          . All rights reserved.
        </div>
        <a
          class="beian"
          href="https://beian.miit.gov.cn/"
          target="_blank"
          rel="noopener noreferrer"
        >
          粤ICP备2025455791号
        </a>
      </div>
    </footer>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Moon, Sunny, Lock } from '@element-plus/icons-vue'

const isDark = ref(false)
const toggleDark = () => (isDark.value = !isDark.value)
const goHome = () => {
  window.scrollTo({
    top: 0,
    behavior: 'smooth',
  })
}
</script>

<!-- /* 不要 scoped */ -->
<style>
/* 🌞 默认：浅色主题（挂在 .app-shell 上） */
.app-shell {
  /* 页面背景 & 文字 */
  --bg-page:
    radial-gradient(circle at 10% 10%, rgba(129, 140, 248, 0.16), transparent 55%),
    radial-gradient(circle at 90% 30%, rgba(56, 189, 248, 0.14), transparent 55%),
    radial-gradient(circle at 30% 90%, rgba(34, 197, 94, 0.1), transparent 60%), #f7f8fc;
  --text-main: #111827;
  --text-sub: #4b5563;

  /* 导航栏 */
  --nav-bg: rgba(255, 255, 255, 0.96);
  --nav-border: rgba(15, 23, 42, 0.06);
  --nav-shadow: 0 8px 24px rgba(15, 23, 42, 0.08);

  --brand-title: #0f172a;
  --brand-sub: #6b7280;

  --nav-btn-bg: rgba(15, 23, 42, 0.04);
  --nav-btn-border: rgba(15, 23, 42, 0.12);
  --nav-btn-text: #111827;

  --nav-btn-primary-bg: rgba(22, 163, 74, 0.12);
  --nav-btn-primary-border: rgba(22, 163, 74, 0.35);
  --nav-btn-primary-text: #15803d;

  transition:
    background-color 0.3s ease,
    color 0.3s ease,
    box-shadow 0.3s ease;
}

/* 🌙 深色主题：在 .app-shell--dark 上覆盖 */
.app-shell.app-shell--dark {
  --bg-page: #020617;
  --text-main: #e2e8f0;
  --text-sub: #94a3b8;

  --nav-bg: rgba(17, 25, 40, 0.78);
  --nav-border: rgba(148, 163, 184, 0.28);
  --nav-shadow: 0 4px 20px rgba(0, 0, 0, 0.5), 0 0 32px rgba(99, 102, 241, 0.2);

  --brand-title: #f1f5f9;
  --brand-sub: rgba(148, 163, 184, 0.85);

  --nav-btn-bg: rgba(255, 255, 255, 0.06);
  --nav-btn-border: rgba(255, 255, 255, 0.16);
  --nav-btn-text: #e5e7eb;

  --nav-btn-primary-bg: rgba(16, 185, 129, 0.22);
  --nav-btn-primary-border: rgba(16, 185, 129, 0.5);
  --nav-btn-primary-text: #6ee7b7;
}

/* 让 body 也吃到变量（防止有旧样式残留） */
body {
  margin: 0;
  background: var(--bg-page);
  color: var(--text-main);
}
</style>

<!-- ========= 导航 / 布局具体样式（scoped） ========= -->
<style scoped>
.app-shell {
  min-height: 100vh;
  background: var(--bg-page);
  color: var(--text-main);
}

/* 让主内容居中一点，看起来更像控制台 */
.layout {
  max-width: 1440px;
  margin: 0 auto;
}

.app-footer {
  padding: 28px 0 32px;
  text-align: center;
  font-size: 14px;
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0), var(--bg-page));
}

.footer-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.beian {
  color: #1e90ff;
  text-decoration: none;
  font-size: 14px;
}

.beian:hover {
  text-decoration: underline;
}

/* ===== 导航栏（使用主题变量） ===== */
.nav {
  height: 70px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 32px;
  margin-bottom: 12px;

  background: var(--nav-bg);
  border-bottom: 1px solid var(--nav-border);
  box-shadow: var(--nav-shadow);
}

/* 左侧品牌 */
.nav-left {
  flex: 1 1 auto;
  min-width: 0;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.brand-icon {
  font-size: 28px;
  color: #93c5fd;
  filter: drop-shadow(0 0 8px rgba(96, 165, 250, 0.4));
}

.brand-text {
  min-width: 0;
}

.brand-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--brand-title);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.brand-subtitle {
  font-size: 12px;
  color: var(--brand-sub);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ===== 导航按钮 ===== */
.nav-right {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: nowrap;
}

.nav-btn {
  padding: 8px 18px;
  border-radius: 999px;

  background: var(--nav-btn-bg);
  border: 1px solid var(--nav-btn-border);

  color: var(--nav-btn-text);
  font-weight: 500;
  letter-spacing: 0.5px;

  display: inline-flex;
  align-items: center;
  gap: 6px;

  cursor: pointer;
  transition: 0.22s ease;
  text-decoration: none;
  white-space: nowrap;
  flex: 0 0 auto;
}

.nav-btn:hover {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.22);
  transform: translateY(-2px);
  box-shadow: 0 0 12px rgba(148, 163, 255, 0.32);
}

.nav-btn.primary {
  background: var(--nav-btn-primary-bg);
  border-color: var(--nav-btn-primary-border);
  color: var(--nav-btn-primary-text);
}

.nav-btn.primary:hover {
  background: rgba(16, 185, 129, 0.3);
  box-shadow: 0 0 16px rgba(52, 211, 153, 0.55);
}

/* 主内容区 */
.main {
  padding: 12px 24px 24px;
}

/* ================== Mobile Responsive ================== */
@media (max-width: 768px) {
  .nav {
    padding: 0 12px;
    gap: 10px;
    flex-wrap: nowrap;
  }

  .brand-title {
    font-size: 16px;
  }

  .brand-subtitle {
    font-size: 12px;
  }

  /* 关键：不要横向滚动隐藏按钮，优先让按钮“变小但保持一行” */
  .nav-right {
    gap: 8px;
    overflow: visible;
    max-width: none;
    white-space: nowrap;
  }

  .nav-btn {
    padding: 6px 10px;
    font-size: 13px;
  }
}

@media (max-width: 520px) {
  .brand-subtitle {
    display: none;
  }

  /* 主题按钮仅保留图标，避免竖排/换行 */
  .nav-btn.ghost span {
    display: none;
  }

  .nav-btn {
    padding: 6px 8px;
    font-size: 12px;
  }
}

@media (max-width: 420px) {
  .brand-title {
    font-size: 15px;
  }

  .nav-right {
    gap: 6px;
  }
}
</style>
