# node_agent 发布二进制托管迁移：controller 本地 → asset_service（对象存储）—— 补充设计

> 状态：**Implemented**（P1~P5 已落地，2026-09-05）  日期：2026-09-05
>
> 关联：docs/node-agent-upgrade-design.md（原始方案，本稿为其 §3.1/§7 的**落地修订**——原始方案曾设计 S3 直连，P1 实施时降级为 controller 本地 `LocalReleaseStore`，本文档收回该降级并改为 asset_service 托管）
>
> 触发事故：controller 中转下载端点使用默认 `AGENT_UPDATE_BASE_URL=http://127.0.0.1:8090`，在「controller 本地、agent 远端」拓扑下 agent 连接失败 → 更新卡「重启中」120s 后 failed。
>
> 实现状态（2026-09-05）：
> - P1 asset_service：`PutAgentRelease` 客户端流式上传 RPC（边收边算 sha256，1 GiB 上限）+ `AgentReleaseStore`（S3/MinIO `S3AgentReleaseStore` / 内存 `InMemoryAgentReleaseStore`）+ 装配（env `ASSET_RELEASE_BUCKET` 默认 cluster；S3 配置与 node_agent 快照同款）✅
> - P2 controller：发布改流式转发 asset_service（face client `PutAgentRelease`）；`agent_releases` +bucket（000033）；storage_key 语义 = object_key；删本地下载端点/`OpenBinary`；发布单测改 fake uploader ✅
> - P3 node_agent：`UpdateNodeAgentRequest` +object_key/bucket（download_url 保留弃用回退）；`update.rs` 拉取改 `ObjectStore.get_object`；grpc server 注入 object_store ✅
> - P4 退役：编排器下发 object_key/bucket；删除 `LocalReleaseStore`/下载路由/`AGENT_UPDATE_BASE_URL`/`AGENT_RELEASE_DIR` ✅
> - P5 验证：本地端到端冒烟（controller face client → asset_service 内存模式流式上传）sha256/size/object_key 全一致；编译/单测全绿（node_agent cargo test 14、controller go build+test biz/handler、asset_service cargo check）；**完整 S3 链路（agent 直连对象存储拉取→替换→exit42→心跳回归）需节点部署验证**（清单见 §6）。
>
> 同批事故修复（2026-09-05）：发布 v0.1.1 二进制未 bump Cargo 版本（自报 v0.1.0）；节点已通过「bump 0.1.1 + glibc 2.36 兼容构建（rust:1.93-slim-bookworm）」升级完成，心跳实测 v0.1.1。

## 1. 背景与目标

真实部署拓扑（2026-09-05 用户确认）：

| 组件 | 位置 |
| --- | --- |
| controller-go（:8090）+ platform-service/web | **Windows 本地**（controller 暂只能本地启动） |
| asset_service（gRPC 50053/9091） | 节点侧 |
| S3 / 快照对象存储 | 节点侧 |
| node_agent | Linux 节点（systemd 托管，`ASSET_SERVICE_ADDR=127.0.0.1:50053`） |

**矛盾**：node_agent 一键更新（原设计）依赖「agent 从 controller HTTP 下载 release」；该下载 URL 由 `AGENT_UPDATE_BASE_URL` 决定（默认 `127.0.0.1:8090`）。controller 在 Windows 本地 → 节点上的 agent 永远连不上（连的是自己）→ 更新必失败，且要等 120s 超时才暴露（2026-09-05 实测）。

**目标**：发布二进制（node_agent release）的**存储与分发宿主迁移到节点侧可达的 asset_service + S3**；agent 更新改走自身已有的对象存储链路（与快照上传同构，agent ⇄ S3 直连）；**删除「节点必须可达 controller 下载端点」这一前提**，`AGENT_UPDATE_BASE_URL` 相关配置/路由整体退役。

## 2. 现状盘点

| 层 | 现状 | 差距 |
| --- | --- | --- |
| asset_service（Rust，:50053 双 service） | 只有快照/构建/业务 CRUD **元数据**；无 S3 client（快照数据面在 node_agent 直连 S3）；无 release 域 | 需加 S3 写能力 + release 上传 RPC |
| controller | `LocalReleaseStore`（本地 `./agent-releases`）+ `GET /api/node-agents/releases/:id/download` + `AGENT_UPDATE_BASE_URL`；清单 `agent_releases` 表 | 改为经 asset_service 写 S3；清单记 object_key；下载路由退役 |
| controller↔asset_service | `AssetServiceFaceClient` / `BusinessServiceFaceClient`（grpc，`ASSET_SERVICE_ADDR` 可配） | 直接复用，加方法 |
| node_agent | 已有 `ObjectStore` 抽象 + `S3ObjectStore`（aws-sdk-s3，快照/增量上传在用）；`update.rs` 目前用 hyper HTTP 拉 download_url | 改 `ObjectStore.get_object(bucket, key)` 拉取；UpdateNodeAgentRequest 携带 object_key |
| platform-web / platform-service | 发布上传 UI/透传（走 controller multipart） | 无感（controller 内部换存储后端） |

## 3. 目标架构

```
发布（写侧，admin 触发）：
admin → platform → controller(multipart 收流)
      → gRPC 客户端流 PutReleaseObject(version, os, arch, chunks)
      → asset_service 边收边算 sha256 → S3 put  object_key=agent-release/{version}/{os}-{arch}/node-agent
      → 返回 {bucket, object_key, sha256, size_bytes} → controller 记 agent_releases(object_key)

更新（读侧，agent 触发）：
controller 下发 {version, sha256, size_bytes, bucket, object_key}
      → node_agent ObjectStore.get_object(bucket, object_key) 直连 S3
      → sha256/size 校验 → staging → 备份 .prev → 原子替换 → exit(42)
      → systemd Restart=always 拉起新版本 → 心跳 v0.1.1 → controller 轮询确认 updated
```

关键点：读侧**与快照链路同构**（node_agent 直连 S3，S3 在节点侧可达，配置复用快照同款 env）；controller 只做清单与编排，不再承载文件。

## 4. 关键设计决策

### 4.1 对象 key 规范与桶

- 桶：**复用快照桶配置**（asset_service 无独立 S3 客户端历史，上传侧由 asset_service 持有 S3 client，桶名 env `ASSET_RELEASE_BUCKET`，默认与快照 `cluster` 一致）；agent 拉取时 bucket 由 controller 清单透传，不硬编码。
- key：`agent-release/{version}/{os}-{arch}/node-agent`（版本目录化，天然多版本并排，支持回滚到任意已发布版本——顺带解除原设计「controller 只记上一版本」的局限）。

### 4.2 发布上传（controller → asset_service 客户端流）

- asset_service.proto 新增（挂 AssetService 主 service）：
  ```proto
  message PutReleaseObjectRequest {
    string version = 1;  // 首条消息带元数据；后续为数据块（也可逐条带 chunk）
    string os = 2;
    string arch = 3;
    bytes chunk = 4;
  }
  message PutReleaseObjectResponse {
    string bucket = 1;
    string object_key = 2;
    string sha256 = 3;   // asset_service 边收边算
    uint64 size_bytes = 4;
  }
  rpc PutReleaseObject(stream PutReleaseObjectRequest) returns (PutReleaseObjectResponse);
  ```
- controller 上传 handler：gin multipart 收流 → 打开到 asset_service 的 client stream → 逐块（如 1 MiB）转发（首条带 version/os/arch）→ 关流等响应 → 以响应（sha256/size/object_key）落 `agent_releases`。**controller 不再落盘临时文件**。
- controller face client 新增 `PutReleaseObject`（grpc client-streaming）。
- 版本冲突（同 version/os/arch 已存在）由 controller 清单层 409 拦截（沿用现有 version 唯一约束）。

### 4.3 agent 更新拉取（ObjectStore 直连）

- node_agent.proto `UpdateNodeAgentRequest`：`download_url`（=4）**保留弃用**，新增：
  ```proto
  string object_key = 5;
  string bucket = 6;
  ```
- `update.rs`：`apply_update()` 下载源从「hyper HTTP GET download_url」改为「`ObjectStore.get_object(bucket, object_key)`」→ 校验 sha256/size（逻辑不变）→ staging → 备份 → 替换 → `run_update_and_restart()`（exit 42 不变）。
- 实现接线：grpc server / update handler 需要持有 `Arc<dyn ObjectStore>`（node_agent main.rs 已装配 `S3ObjectStore`，快照同款；dev 内存 store 兜底）。update 流程内对「内存对象」校验逻辑不变。
- **版本自述一致性**：下载的是 asset_service 已存对象，controller 清单 sha256 与 asset_service 计算一致 → agent 校验闭环。

### 4.4 controller 退役项

- `AgentReleaseDir` / `LocalReleaseStore`（本地盘）删除；
- `GET /api/node-agents/releases/:id/download` 路由删除；
- `AGENT_UPDATE_BASE_URL` config/env 删除（orchestrator 不再拼 download URL，下发 object_key/bucket）；
- 存量 `agent_releases` 记录（object_key 为空）：标注不可更新（UI 置灰 + 重新发布同版本覆盖即可；自家二进制重发成本低）。
- 迁移：`ALTER TABLE agent_releases ADD COLUMN object_key TEXT NOT NULL DEFAULT ''`（000033）。

### 4.5 配置

| 组件 | 新增 env | 说明 |
| --- | --- | --- |
| asset_service | `ASSET_RELEASE_BUCKET`（默认 cluster）、S3 端点/凭证（与 node_agent 快照同款：`S3_ENDPOINT/AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY/AWS_REGION` 等，按其 main.rs 现读取方式对齐） | S3 在节点侧，asset_service 同侧访问 |
| controller | 无新增（`ASSET_SERVICE_ADDR` 已是现有配置，指向 asset_service） | — |
| node_agent | 无新增（S3 配置与快照共用） | — |

## 5. 实施拆分

| P | 内容 | 涉及 | 验证 |
| --- | --- | --- | --- |
| P1 | asset_service：引入 aws-sdk-s3 + ObjectStore 实现（或封装 S3 put）；`PutReleaseObject` 流式 RPC（边收边 sha256）；注册到 AssetServiceServer | asset_service（Cargo+proto+main） | cargo check/test（sha256 边收边算单测） |
| P2 | controller：face client `PutReleaseObject`；上传 handler 改流式转发；`agent_releases` +object_key 迁移（000033）；use case Register/Get/OpenBinary 改造（OpenBinary 退役）；`AGENT_UPDATE_BASE_URL`/LocalReleaseStore/下载路由删除 | controller-go | go build + 单测（Register 校验/409 保留） |
| P3 | node_agent：proto `object_key/bucket`；`update.rs` 拉取改 ObjectStore；grpc server 注入 object_store | node_agent | cargo test（apply_update 校验逻辑保留） |
| P4 | 联动修正：platform-service/Web 无感（仅 controller 内部）；orchestrator 下发字段改 object_key/bucket；rollback 沿用 release_id | controller-go | go build |
| P5 | 部署/文档：controller→asset_service 上传链路端到端冒烟（本地内存 asset_service + 内存 store 兜底）；docs 状态 Implemented；upgrade doc §3.1/§8 同步修订 | docs + 全链路 | 见 §6 |

## 6. 验证清单

1. 上传 release：admin 上传 → controller 流式 → asset_service 写对象（本地冒烟用 InMemory store 断言 put 内容/sha256）→ 清单出现（object_key/sha256）；
2. 重复 version/os/arch → 409；
3. agent 更新：object_key 拉取 → sha256 一致 → 替换 → exit(42)（Linux 端 systemd Restart 拉起）→ 心跳 v0.1.1 → `updated`；**controller 无需对节点可达**（拓扑无关性验证：controller 本地 + agent 远端）；
4. sha256 篡改 → failed + 原因可见，当前版本不变；
5. 回滚 release_id → 指定版本对象拉取；
6. 存量 release（object_key 空）→ UI 标「不可更新」，重发后恢复。

## 7. 边界 / 后续

- 旧 release 记录不迁移对象（自家二进制重发成本低；object_key 空置灰）；
- rollback 到任意历史版本成为天然能力（对象按 version 并排），UI 仍以「最近发布」近似（可后续放开多版本选择）；
- asset_service 首增 S3 client：凭证/S3 配置与其部署环境对齐（节点侧 minio/S3），需在部署侧确认；
- 本稿不改变 node_agent 侧「下载→校验→替换→exit(42)→systemd 拉起」链路语义，只换下载源。
