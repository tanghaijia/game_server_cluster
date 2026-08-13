# platform-web

平台 Web 控制台前端（Vue 3 + Vite + TypeScript + Tailwind CSS v4 + shadcn-vue）。
技术决策见 [ADR-0003](../docs/adr/0003-frontend-stack-vue3-tailwind-shadcn-vue.md)、[ADR-0004](../docs/adr/0004-api-contract-and-auth.md)、[ADR-0005](../docs/adr/0005-deployment-spa-nginx-sse.md)。

## 技术栈

- Vue 3（Composition API + `<script setup>`）
- TypeScript（strict）
- Vite 6 + @tailwindcss/vite（Tailwind CSS v4，CSS-first 配置）
- shadcn-vue（Tailwind 原生组件，复制进代码库）
- Pinia + Vue Router（状态 + 路由守卫）
- axios（JWT 拦截器，见 `src/api/http.ts`）
- TanStack Table（建议：表格密集型页面）
- ECharts（建议：仪表盘）

## 快速开始

```bash
npm install
npm run dev        # http://localhost:5173，/api 代理到 127.0.0.1:8081
npm run build      # 产物 dist/
```

## 初始化 shadcn-vue（骨架已含 components.json，首次使用需执行）

```bash
npx shadcn-vue@latest init   # 生成主题变量与 lib/utils
npx shadcn-vue@latest add button card input table ...
```

## 目录结构

```text
src/
├── api/          # axios 实例与接口封装
├── assets/       # 全局样式（Tailwind v4 + 主题变量）
├── components/   # shadcn-vue 组件（init 后生成）
├── layouts/      # 页面布局
├── lib/          # cn() 等工具
├── router/       # 路由 + 守卫
├── stores/       # Pinia 状态
└── views/        # 页面
```

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `VITE_API_BASE` | API 前缀，默认 `/api`（开发环境由 Vite 代理，生产由 Nginx 反代） |

## 生产部署

见 `nginx.conf.example`：静态文件 + `/api` 反代到 platform-service（端口 8081），SSE 已关缓冲。
